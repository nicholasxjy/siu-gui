package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// ProxyMode controls how outbound downloads route through proxies.
type ProxyMode string

const (
	ProxyModeSystem ProxyMode = "system"
	ProxyModeCustom ProxyMode = "custom"
	ProxyModeDirect ProxyMode = "direct"
)

// ProxySettings persists the user's download proxy policy in proxy.json.
type ProxySettings struct {
	Mode    ProxyMode `json:"mode"`
	URL     string    `json:"url"`
	NoProxy string    `json:"noProxy"`
}

func DefaultProxySettings() ProxySettings {
	return ProxySettings{Mode: ProxyModeSystem}
}

func (p ProxySettings) Validate() (ProxySettings, error) {
	if p.Mode == "" {
		p.Mode = ProxyModeSystem
	}
	switch p.Mode {
	case ProxyModeSystem, ProxyModeCustom, ProxyModeDirect:
	default:
		return ProxySettings{}, errors.New("无效的代理设置")
	}
	p.URL = strings.TrimSpace(p.URL)
	p.NoProxy = strings.TrimSpace(p.NoProxy)
	if len(p.URL) > 4096 || len(p.NoProxy) > 8192 {
		return ProxySettings{}, errors.New("代理设置过长，请缩短代理地址或绕过列表。")
	}
	if p.Mode == ProxyModeCustom {
		u, err := parseProxyURL(p.URL)
		if err != nil {
			return ProxySettings{}, err
		}
		if u == nil {
			return ProxySettings{}, errors.New("请输入有效的代理服务器地址和端口。")
		}
		if _, err := parseBypassList(p.NoProxy, false); err != nil {
			return ProxySettings{}, errors.New("绕过列表包含无效项目，请使用逗号分隔的域名、IP 或 CIDR。")
		}
	}
	return p, nil
}

func parseProxyURL(raw string) (*url.URL, error) {
	if raw == "" {
		return nil, errors.New("请输入有效的代理地址。")
	}
	// Reject explicit empty query/fragment (`http://host:80?` or `#`) too.
	if strings.ContainsAny(raw, "?#") {
		return nil, errors.New("代理地址须为 HTTP、HTTPS 或 SOCKS5 地址，且不能包含路径、查询或片段。")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, errors.New("请输入有效的代理地址。")
	}
	switch u.Scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return nil, errors.New("代理地址须为 HTTP、HTTPS 或 SOCKS5 地址，且不能包含路径、查询或片段。")
	}
	if u.Hostname() == "" {
		return nil, errors.New("代理地址须为 HTTP、HTTPS 或 SOCKS5 地址，且不能包含路径、查询或片段。")
	}
	if portStr := u.Port(); portStr != "" {
		port, err := strconv.Atoi(portStr)
		if err != nil || port <= 0 || port > 65535 {
			return nil, errors.New("代理地址须为 HTTP、HTTPS 或 SOCKS5 地址，且不能包含路径、查询或片段。")
		}
	}
	if u.Path != "" && u.Path != "/" {
		return nil, errors.New("代理地址须为 HTTP、HTTPS 或 SOCKS5 地址，且不能包含路径、查询或片段。")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("代理地址须为 HTTP、HTTPS 或 SOCKS5 地址，且不能包含路径、查询或片段。")
	}
	return u, nil
}

// ParseProxySettingsJSON strictly decodes ProxySettings (rejecting unknown fields).
func ParseProxySettingsJSON(data []byte) (ProxySettings, error) {
	var s ProxySettings
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return ProxySettings{}, errors.New("无效的代理设置")
	}
	if s.Mode == "" {
		s.Mode = ProxyModeSystem
	}
	return s, nil
}

// LoadProxy loads and validates proxy.json under root, defaulting to system mode.
func LoadProxy(root string) (ProxySettings, error) {
	path := filepath.Join(root, "proxy.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return DefaultProxySettings(), nil
		}
		return ProxySettings{}, fmt.Errorf("无法读取代理设置: %w", err)
	}
	s, err := ParseProxySettingsJSON(raw)
	if err != nil {
		return ProxySettings{}, errors.New("无法读取代理设置，请在设置页重新保存。")
	}
	return s.Validate()
}

// CurrentProxy loads proxy settings from the active resolved library root.
func CurrentProxy() (ProxySettings, error) {
	return LoadProxy(Resolve().Root)
}

// SaveProxy validates and atomically persists proxy.json with user-only permissions.
func SaveProxy(root string, settings ProxySettings) (ProxySettings, error) {
	validated, err := settings.Validate()
	if err != nil {
		return ProxySettings{}, err
	}
	raw, err := marshalPrettyJSON(validated)
	if err != nil {
		return ProxySettings{}, fmt.Errorf("无法保存代理设置: %w", err)
	}
	if err := WriteAtomic(filepath.Join(root, "proxy.json"), raw); err != nil {
		return ProxySettings{}, fmt.Errorf("无法保存代理设置: %w", err)
	}
	return validated, nil
}

// ---- Bypass list matching ----

type bypassMatcher struct {
	all        bool
	localOnly  bool // Windows "<local>" rule
	cidrs      []netip.Prefix
	exactIPs   []ipPortRule
	domains    []domainPortRule
}

type ipPortRule struct {
	addr netip.Addr
	port string
}

type domainPortRule struct {
	suffix string // lowercase without leading dot
	exact  bool
	port   string
}

func parseBypassList(raw string, lenient bool) (*bypassMatcher, error) {
	m := &bypassMatcher{}
	if strings.TrimSpace(raw) == "" {
		return m, nil
	}
	for _, part := range strings.Split(raw, ",") {
		tok := strings.TrimSpace(part)
		if tok == "" {
			continue
		}
		if tok == "*" {
			m.all = true
			continue
		}
		if strings.EqualFold(tok, "<local>") {
			m.localOnly = true
			continue
		}
		if strings.ContainsAny(tok, " \t\r\n") || strings.Contains(tok, "://") {
			if lenient {
				continue
			}
			return nil, fmt.Errorf("invalid bypass token %q", tok)
		}
		// CIDR?
		if strings.Contains(tok, "/") {
			pfx, err := netip.ParsePrefix(tok)
			if err != nil {
				if lenient {
					continue
				}
				return nil, fmt.Errorf("invalid CIDR %q", tok)
			}
			m.cidrs = append(m.cidrs, pfx)
			continue
		}
		host, port, err := splitBypassHostPort(tok)
		if err != nil {
			if lenient {
				continue
			}
			return nil, err
		}
		if addr, err := netip.ParseAddr(host); err == nil {
			m.exactIPs = append(m.exactIPs, ipPortRule{addr: addr.Unmap(), port: port})
			continue
		}
		h := strings.ToLower(host)
		exact := true
		if rest, ok := strings.CutPrefix(h, "*."); ok {
			h = rest
			exact = false
		} else if rest, ok := strings.CutPrefix(h, "."); ok {
			h = rest
			exact = false
		}
		if !isValidBypassDomain(h) {
			if lenient {
				continue
			}
			return nil, fmt.Errorf("invalid bypass host %q", tok)
		}
		m.domains = append(m.domains, domainPortRule{suffix: h, exact: exact, port: port})
	}
	return m, nil
}

func splitBypassHostPort(tok string) (string, string, error) {
	// Bracketed IPv6, with or without port.
	if strings.HasPrefix(tok, "[") {
		if h, p, err := net.SplitHostPort(tok); err == nil {
			if err := validatePortString(p); err != nil {
				return "", "", err
			}
			return h, p, nil
		}
		if strings.HasSuffix(tok, "]") {
			return tok[1 : len(tok)-1], "", nil
		}
		return "", "", fmt.Errorf("invalid bracketed host %q", tok)
	}
	// Bare IPv6 address (contains multiple colons).
	if strings.Count(tok, ":") > 1 {
		return tok, "", nil
	}
	// Single colon -> host:port.
	if idx := strings.LastIndexByte(tok, ':'); idx >= 0 {
		h := tok[:idx]
		p := tok[idx+1:]
		if err := validatePortString(p); err != nil {
			return "", "", err
		}
		return h, p, nil
	}
	return tok, "", nil
}

func validatePortString(p string) error {
	port, err := strconv.Atoi(p)
	if err != nil || port <= 0 || port > 65535 {
		return fmt.Errorf("invalid port %q", p)
	}
	return nil
}

func isValidBypassDomain(h string) bool {
	if h == "" || len(h) > 253 || strings.HasPrefix(h, ".") || strings.HasSuffix(h, ".") {
		return false
	}
	for _, label := range strings.Split(h, ".") {
		if label == "" || len(label) > 63 {
			return false
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
				return false
			}
		}
	}
	return true
}

func (m *bypassMatcher) Matches(u *url.URL) bool {
	if m == nil {
		return false
	}
	if m.all {
		return true
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return false
	}
	port := effectivePort(u)
	if m.localOnly && !strings.Contains(host, ".") {
		return true
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		addr = addr.Unmap()
		for _, pfx := range m.cidrs {
			if pfx.Contains(addr) {
				return true
			}
		}
		for _, rule := range m.exactIPs {
			if rule.addr == addr && (rule.port == "" || rule.port == port) {
				return true
			}
		}
		return false
	}
	for _, rule := range m.domains {
		if rule.port != "" && rule.port != port {
			continue
		}
		if host == rule.suffix || (!rule.exact && strings.HasSuffix(host, "."+rule.suffix)) {
			return true
		}
		// Standard NO_PROXY semantics: bare domain `example.com` also matches subdomains.
		if rule.exact && strings.HasSuffix(host, "."+rule.suffix) {
			return true
		}
	}
	return false
}

func effectivePort(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return "443"
	case "http":
		return "80"
	default:
		return ""
	}
}

// ---- Route resolution ----

// systemProxySnapshot holds proxy endpoints read from the host OS.
type systemProxySnapshot struct {
	httpProxy  *url.URL
	httpsProxy *url.URL
	socksProxy *url.URL
	bypass     *bypassMatcher
}

func (s *systemProxySnapshot) routeFor(u *url.URL) *url.URL {
	if s == nil {
		return nil
	}
	if s.bypass != nil && s.bypass.Matches(u) {
		return nil
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		if s.httpsProxy != nil {
			return s.httpsProxy
		}
	case "http":
		if s.httpProxy != nil {
			return s.httpProxy
		}
	}
	if s.socksProxy != nil {
		return s.socksProxy
	}
	return nil
}

type envProxyConfig struct {
	httpProxy  string
	httpsProxy string
	allProxy   string
	noProxy    string
}

func readEnvProxyConfig() envProxyConfig {
	get := func(upper, lower string) string {
		if v := os.Getenv(upper); v != "" {
			return v
		}
		return os.Getenv(lower)
	}
	return envProxyConfig{
		httpProxy:  get("HTTP_PROXY", "http_proxy"),
		httpsProxy: get("HTTPS_PROXY", "https_proxy"),
		allProxy:   get("ALL_PROXY", "all_proxy"),
		noProxy:    get("NO_PROXY", "no_proxy"),
	}
}

func (e envProxyConfig) proxyForScheme(scheme string) string {
	switch strings.ToLower(scheme) {
	case "https":
		if e.httpsProxy != "" {
			return e.httpsProxy
		}
	case "http":
		if e.httpProxy != "" {
			return e.httpProxy
		}
	}
	return e.allProxy
}

func parseLenientProxyURL(raw string) *url.URL {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return nil
	}
	return u
}

// ResolveProxyRoutes resolves the ordered proxy routes (nil entry = direct)
// for target under settings. System proxy configuration is re-read live.
func ResolveProxyRoutes(settings ProxySettings, target *url.URL) ([]*url.URL, error) {
	return resolveProxyRoutesWith(settings, target, readEnvProxyConfig(), readSystemProxy)
}

func resolveProxyRoutesWith(
	settings ProxySettings,
	target *url.URL,
	envCfg envProxyConfig,
	sysReader func() (*systemProxySnapshot, error),
) ([]*url.URL, error) {
	switch settings.Mode {
	case ProxyModeDirect:
		return []*url.URL{nil}, nil
	case ProxyModeCustom:
		bypass, err := parseBypassList(settings.NoProxy, false)
		if err != nil {
			return nil, errors.New("绕过列表包含无效项目，请使用逗号分隔的域名、IP 或 CIDR。")
		}
		if bypass.Matches(target) {
			return []*url.URL{nil}, nil
		}
		u, err := parseProxyURL(settings.URL)
		if err != nil {
			return nil, err
		}
		return []*url.URL{u}, nil
	default: // ProxyModeSystem
		envBypass, _ := parseBypassList(envCfg.noProxy, true)
		if envBypass.Matches(target) {
			return []*url.URL{nil}, nil
		}
		if raw := envCfg.proxyForScheme(target.Scheme); raw != "" {
			if u := parseLenientProxyURL(raw); u != nil {
				return []*url.URL{u}, nil
			}
		}
		sys, err := sysReader()
		if err != nil {
			return nil, fmt.Errorf("无法读取系统代理，请在设置中手动配置代理或选择直连: %w", err)
		}
		if sys != nil {
			if u := sys.routeFor(target); u != nil {
				return []*url.URL{u}, nil
			}
		}
		return []*url.URL{nil}, nil
	}
}

// ConfigureCommandProxy sets explicit proxy environment variables on a child
// command (such as `docker pull`) matching the active proxy policy.
func ConfigureCommandProxy(cmd *exec.Cmd) error {
	settings, err := CurrentProxy()
	if err != nil {
		return err
	}
	envMap := map[string]string{}
	base := cmd.Env
	if base == nil {
		base = os.Environ()
	}
	var filtered []string
	dropKeys := map[string]bool{
		"HTTP_PROXY": true, "http_proxy": true,
		"HTTPS_PROXY": true, "https_proxy": true,
		"ALL_PROXY": true, "all_proxy": true,
	}
	if settings.Mode == ProxyModeDirect || settings.Mode == ProxyModeCustom {
		dropKeys["NO_PROXY"] = true
		dropKeys["no_proxy"] = true
	}
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		if !dropKeys[k] {
			filtered = append(filtered, kv)
		}
	}
	for _, pair := range [][2]string{
		{"HTTP", "http://registry-1.docker.io/"},
		{"HTTPS", "https://registry-1.docker.io/"},
	} {
		u, _ := url.Parse(pair[1])
		routes, err := ResolveProxyRoutes(settings, u)
		if err != nil {
			return err
		}
		val := ""
		if len(routes) > 0 && routes[0] != nil {
			val = routes[0].String()
		}
		envMap[pair[0]+"_PROXY"] = val
		envMap[strings.ToLower(pair[0])+"_proxy"] = val
	}
	switch settings.Mode {
	case ProxyModeDirect:
		envMap["NO_PROXY"] = "*"
		envMap["no_proxy"] = "*"
	case ProxyModeCustom:
		envMap["NO_PROXY"] = settings.NoProxy
		envMap["no_proxy"] = settings.NoProxy
	}
	for k, v := range envMap {
		filtered = append(filtered, k+"="+v)
	}
	cmd.Env = filtered
	return nil
}
