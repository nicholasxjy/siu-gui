//go:build windows

package core

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

func readSystemProxy() (*systemProxySnapshot, error) {
	k, err := registry.OpenKey(
		registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Internet Settings`,
		registry.QUERY_VALUE,
	)
	if err != nil {
		return &systemProxySnapshot{}, nil
	}
	defer k.Close()

	enabled, _, err := k.GetIntegerValue("ProxyEnable")
	if err != nil || enabled == 0 {
		return &systemProxySnapshot{}, nil
	}
	server, _, err := k.GetStringValue("ProxyServer")
	if err != nil || strings.TrimSpace(server) == "" {
		return &systemProxySnapshot{}, nil
	}
	override, _, _ := k.GetStringValue("ProxyOverride")
	return parseWindowsProxy(server, override), nil
}

func parseWindowsProxy(server, override string) *systemProxySnapshot {
	snap := &systemProxySnapshot{}
	if override != "" {
		normalized := strings.ReplaceAll(override, ";", ",")
		snap.bypass, _ = parseBypassList(normalized, true)
	}
	server = strings.TrimSpace(server)
	if !strings.Contains(server, "=") {
		u := parseLenientProxyURL(server)
		snap.httpProxy = u
		snap.httpsProxy = u
		return snap
	}
	for _, entry := range strings.Split(server, ";") {
		entry = strings.TrimSpace(entry)
		k, v, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.TrimSpace(v)
		switch k {
		case "http":
			snap.httpProxy = parseLenientProxyURL(v)
		case "https":
			snap.httpsProxy = parseLenientProxyURL(v)
		case "socks":
			if !strings.Contains(v, "://") {
				v = "socks5h://" + v
			}
			snap.socksProxy = parseLenientProxyURL(v)
		}
	}
	return snap
}
