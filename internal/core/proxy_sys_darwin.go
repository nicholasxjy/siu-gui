//go:build darwin

package core

import (
	"fmt"
	"net/url"
	"os/exec"
	"strings"
)

func readSystemProxy() (*systemProxySnapshot, error) {
	out, err := exec.Command("scutil", "--proxy").Output()
	if err != nil {
		return &systemProxySnapshot{}, nil
	}
	return parseScutilProxy(string(out)), nil
}

func parseScutilProxy(text string) *systemProxySnapshot {
	kv := map[string]string{}
	var exceptions []string
	inExceptions := false
	for _, rawLine := range strings.Split(text, "\n") {
		line := strings.TrimSpace(rawLine)
		if strings.HasPrefix(line, "ExceptionsList") && strings.Contains(line, "{") {
			inExceptions = true
			continue
		}
		if inExceptions {
			if line == "}" {
				inExceptions = false
				continue
			}
			if _, val, ok := strings.Cut(line, ":"); ok {
				if v := strings.TrimSpace(val); v != "" {
					exceptions = append(exceptions, v)
				}
			}
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		kv[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}

	snap := &systemProxySnapshot{}
	if len(exceptions) > 0 {
		snap.bypass, _ = parseBypassList(strings.Join(exceptions, ","), true)
	}
	buildURL := func(scheme, hostKey, portKey string) *url.URL {
		host := kv[hostKey]
		if host == "" {
			return nil
		}
		if port := kv[portKey]; port != "" && port != "0" {
			return parseLenientProxyURL(fmt.Sprintf("%s://%s:%s", scheme, host, port))
		}
		return parseLenientProxyURL(fmt.Sprintf("%s://%s", scheme, host))
	}
	if kv["HTTPEnable"] == "1" {
		snap.httpProxy = buildURL("http", "HTTPProxy", "HTTPPort")
	}
	if kv["HTTPSEnable"] == "1" {
		snap.httpsProxy = buildURL("http", "HTTPSProxy", "HTTPSPort")
	}
	if kv["SOCKSEnable"] == "1" {
		snap.socksProxy = buildURL("socks5h", "SOCKSProxy", "SOCKSPort")
	}
	return snap
}
