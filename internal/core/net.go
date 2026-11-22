package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	xproxy "golang.org/x/net/proxy"
)

const (
	// skills.sh serves a placeholder page to unknown User-Agents.
	httpUserAgent   = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0 Safari/537.36 siu"
	httpTimeout     = 60 * time.Second
	maxHTTPWorkers  = 8
	maxHTTPRedirect = 10
)

var httpWorkerSem = make(chan struct{}, maxHTTPWorkers)

type cachedTransport struct {
	key       string
	transport *http.Transport
}

var (
	transportMu    sync.Mutex
	transportCache []cachedTransport
)

func transportForProxy(proxyURL *url.URL) (*http.Transport, error) {
	key := ""
	if proxyURL != nil {
		key = proxyURL.String()
	}
	transportMu.Lock()
	defer transportMu.Unlock()

	for _, entry := range transportCache {
		if entry.key == key {
			return entry.transport, nil
		}
	}

	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	tr := &http.Transport{
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          16,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   20 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		// Explicit nil Proxy ensures direct connections never read stale env vars.
		Proxy: nil,
	}

	if proxyURL != nil {
		switch proxyURL.Scheme {
		case "http", "https":
			uCopy := *proxyURL
			tr.Proxy = http.ProxyURL(&uCopy)
		case "socks5", "socks5h":
			var auth *xproxy.Auth
			if proxyURL.User != nil {
				pwd, _ := proxyURL.User.Password()
				auth = &xproxy.Auth{
					User:     proxyURL.User.Username(),
					Password: pwd,
				}
			}
			s5, err := xproxy.SOCKS5("tcp", proxyURL.Host, auth, dialer)
			if err != nil {
				return nil, fmt.Errorf("无法创建下载代理: %w", err)
			}
			if cd, ok := s5.(xproxy.ContextDialer); ok {
				tr.DialContext = cd.DialContext
			} else {
				tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
					return s5.Dial(network, addr)
				}
			}
		default:
			return nil, errors.New("不支持此系统代理协议，请手动配置 HTTP 或 SOCKS5 代理。")
		}
	}

	if len(transportCache) >= 8 {
		transportCache[0].transport.CloseIdleConnections()
		transportCache = transportCache[1:]
	}
	transportCache = append(transportCache, cachedTransport{key: key, transport: tr})
	return tr, nil
}

// HTTPGet fetches rawURL with live proxy routing, redirect re-resolution,
// bounded concurrency, response size cap, and a hard overall timeout.
func HTTPGet(ctx context.Context, rawURL, accept string, limit int64) ([]byte, error) {
	return httpGetWithTimeout(ctx, nil, rawURL, accept, limit, httpTimeout)
}

func httpGetWithTimeout(
	ctx context.Context,
	overrideSettings *ProxySettings,
	rawURL, accept string,
	limit int64,
	timeout time.Duration,
) ([]byte, error) {
	select {
	case httpWorkerSem <- struct{}{}:
		defer func() { <-httpWorkerSem }()
	default:
		return nil, errors.New("HTTP requests are still stalled; check your connection or proxy before retrying")
	}

	if ctx == nil {
		ctx = context.Background()
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var settings ProxySettings
	if overrideSettings != nil {
		settings = *overrideSettings
	} else {
		var err error
		settings, err = CurrentProxy()
		if err != nil {
			return nil, err
		}
	}

	return httpGetConfigured(reqCtx, settings, rawURL, accept, limit, timeout)
}

func httpGetConfigured(
	ctx context.Context,
	settings ProxySettings,
	rawURL, accept string,
	limit int64,
	timeout time.Duration,
) ([]byte, error) {
	current, err := url.Parse(rawURL)
	if err != nil || current.Scheme == "" || current.Host == "" {
		return nil, fmt.Errorf("invalid download URL: %w", err)
	}

	for redirects := 0; redirects <= maxHTTPRedirect; redirects++ {
		routes, err := ResolveProxyRoutes(settings, current)
		if err != nil {
			return nil, err
		}

		var resp *http.Response
		var lastErr error
		for _, proxyURL := range routes {
			tr, err := transportForProxy(proxyURL)
			if err != nil {
				lastErr = err
				continue
			}
			client := &http.Client{
				Transport: tr,
				CheckRedirect: func(*http.Request, []*http.Request) error {
					return http.ErrUseLastResponse
				},
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, current.String(), nil)
			if err != nil {
				lastErr = err
				continue
			}
			req.Header.Set("User-Agent", httpUserAgent)
			if accept != "" {
				req.Header.Set("Accept", accept)
			}
			r, err := client.Do(req)
			if err != nil {
				lastErr = err
				continue
			}
			resp = r
			break
		}

		if resp == nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return nil, fmt.Errorf("GET %s timed out after %v; check your connection or proxy and retry", urlHost(current.String()), timeout)
			}
			if lastErr == nil {
				lastErr = errors.New("no proxy route available")
			}
			return nil, fmt.Errorf("GET %s: %w", urlHost(current.String()), lastErr)
		}

		if isRedirectStatus(resp.StatusCode) {
			loc := resp.Header.Get("Location")
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
			if loc != "" {
				nextRef, err := url.Parse(loc)
				if err != nil {
					return nil, fmt.Errorf("invalid download redirect: %w", err)
				}
				next := current.ResolveReference(nextRef)
				if next.Scheme != "http" && next.Scheme != "https" {
					return nil, errors.New("unsupported download redirect")
				}
				current = next
				continue
			}
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
			return nil, fmt.Errorf("GET %s: http status: %d", urlHost(current.String()), resp.StatusCode)
		}

		body, readErr := io.ReadAll(io.LimitReader(resp.Body, limit+1))
		_ = resp.Body.Close()
		if readErr != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return nil, fmt.Errorf("GET %s timed out after %v; check your connection or proxy and retry", urlHost(current.String()), timeout)
			}
			return nil, fmt.Errorf("reading %s: %w", urlHost(current.String()), readErr)
		}
		if int64(len(body)) > limit {
			return nil, fmt.Errorf("%s sent more than %d MB", urlHost(current.String()), limit>>20)
		}
		return body, nil
	}

	return nil, errors.New("too many download redirects")
}

func isRedirectStatus(code int) bool {
	switch code {
	case http.StatusMovedPermanently,
		http.StatusFound,
		http.StatusSeeOther,
		http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect:
		return true
	default:
		return false
	}
}

// HTTPGetJSON fetches rawURL expecting JSON and unmarshals it into T.
func HTTPGetJSON[T any](ctx context.Context, rawURL string) (T, error) {
	var zero T
	raw, err := HTTPGet(ctx, rawURL, "application/json", 16<<20)
	if err != nil {
		return zero, err
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		return zero, fmt.Errorf("%s answered something unexpected: %w", urlHost(rawURL), err)
	}
	return out, nil
}

func urlHost(raw string) string {
	if _, rest, ok := strings.Cut(raw, "://"); ok {
		if h, _, ok := strings.Cut(rest, "/"); ok {
			return h
		}
		return rest
	}
	return raw
}

// URLEncode percent-encodes a query/path segment (RFC 3986 unreserved chars stay literal).
func URLEncode(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}
