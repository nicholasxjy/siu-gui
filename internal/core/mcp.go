package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// Transport is the MCP transport type.
type Transport string

const (
	TransportStdio Transport = "stdio"
	TransportHttp  Transport = "http"
	TransportSse   Transport = "sse"
)

func (t Transport) Label() string {
	switch t {
	case TransportHttp:
		return "http"
	case TransportSse:
		return "sse"
	default:
		return "stdio"
	}
}

func (t *Transport) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	switch s {
	case "", "stdio":
		*t = TransportStdio
	case "http":
		*t = TransportHttp
	case "sse":
		*t = TransportSse
	default:
		return fmt.Errorf("unknown transport %q", s)
	}
	return nil
}

// Server defines an MCP server in the library.
type Server struct {
	Name      string            `json:"name"`
	Transport Transport         `json:"transport"`
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	URL       string            `json:"url,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
}

func (s Server) Remote() bool {
	return s.Transport != "" && s.Transport != TransportStdio
}

func (s Server) Summary() string {
	if s.Remote() {
		return s.URL
	}
	parts := make([]string, 0, 1+len(s.Args))
	parts = append(parts, s.Command)
	parts = append(parts, s.Args...)
	return strings.Join(parts, " ")
}

func (s Server) Validate() error {
	if err := ValidName(s.Name); err != nil {
		return err
	}
	if s.Remote() {
		if !strings.HasPrefix(s.URL, "http://") && !strings.HasPrefix(s.URL, "https://") {
			return errors.New("the URL has to start with http:// or https://")
		}
	} else if strings.TrimSpace(s.Command) == "" {
		return errors.New("a command is needed")
	}
	return nil
}

func (s Server) Clone() Server {
	out := s
	if s.Args != nil {
		out.Args = append([]string(nil), s.Args...)
	}
	if s.Env != nil {
		out.Env = make(map[string]string, len(s.Env))
		for k, v := range s.Env {
			out.Env[k] = v
		}
	}
	if s.Headers != nil {
		out.Headers = make(map[string]string, len(s.Headers))
		for k, v := range s.Headers {
			out.Headers[k] = v
		}
	}
	return out
}

// ValidName ensures a name is a safe TOML bare key and plain folder name.
func ValidName(name string) error {
	ok := len(name) > 0 && len(name) <= 64 && isASCIIAlnum(name[0])
	if ok {
		for i := 0; i < len(name); i++ {
			c := name[i]
			if !isASCIIAlnum(c) && c != '-' && c != '_' {
				ok = false
				break
			}
		}
	}
	if !ok {
		return fmt.Errorf("name %q: use letters, digits, - and _ only", name)
	}
	return nil
}

func isASCIIAlnum(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// UnsupportedMCP explains why format cannot represent s, or "" if supported.
func UnsupportedMCP(format McpFormat, s *Server) string {
	t := s.Transport
	if t == "" {
		t = TransportStdio
	}
	switch {
	case format == FormatDesktop && (t == TransportHttp || t == TransportSse):
		return "remote servers go in Connectors"
	case (format == FormatCodex || format == FormatPi) && t == TransportSse:
		return "no SSE support"
	default:
		return ""
	}
}

func containerKey(format McpFormat) string {
	switch format {
	case FormatOpenCode:
		return "mcp"
	case FormatCodex, FormatGrok:
		return "mcp_servers"
	default:
		return "mcpServers"
	}
}

func isTOMLFormat(format McpFormat) bool {
	return format == FormatCodex || format == FormatGrok
}

func sortedStringMapObject(m map[string]string) *OrderedObject {
	obj := NewOrderedObject()
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		obj.Set(k, m[k])
	}
	return obj
}

func stringSliceAny(xs []string) []any {
	out := make([]any, len(xs))
	for i, x := range xs {
		out[i] = x
	}
	return out
}

// EncodeMCP builds the agent-specific JSON/TOML entry for s.
func EncodeMCP(format McpFormat, s *Server) *OrderedObject {
	o := NewOrderedObject()
	optMap := func(k string, m map[string]string) {
		if len(m) > 0 {
			o.Set(k, sortedStringMapObject(m))
		}
	}
	t := s.Transport
	if t == "" {
		t = TransportStdio
	}
	switch format {
	case FormatClaude:
		o.Set("type", t.Label())
		if s.Remote() {
			o.Set("url", s.URL)
			optMap("headers", s.Headers)
		} else {
			o.Set("command", s.Command)
			o.Set("args", stringSliceAny(s.Args))
			o.Set("env", sortedStringMapObject(s.Env))
		}
	case FormatGemini:
		switch t {
		case TransportHttp:
			o.Set("httpUrl", s.URL)
			optMap("headers", s.Headers)
		case TransportSse:
			o.Set("url", s.URL)
			optMap("headers", s.Headers)
		default:
			o.Set("command", s.Command)
			o.Set("args", stringSliceAny(s.Args))
			optMap("env", s.Env)
		}
	case FormatOpenCode:
		if s.Remote() {
			o.Set("type", "remote")
			o.Set("url", s.URL)
			optMap("headers", s.Headers)
		} else {
			o.Set("type", "local")
			cmd := make([]any, 0, 1+len(s.Args))
			cmd = append(cmd, s.Command)
			for _, a := range s.Args {
				cmd = append(cmd, a)
			}
			o.Set("command", cmd)
			optMap("environment", s.Env)
		}
		o.Set("enabled", true)
	case FormatCursor, FormatDesktop, FormatPi:
		if s.Remote() {
			o.Set("url", s.URL)
			optMap("headers", s.Headers)
		} else {
			o.Set("command", s.Command)
			o.Set("args", stringSliceAny(s.Args))
			optMap("env", s.Env)
		}
	case FormatCopilot:
		if s.Remote() {
			o.Set("type", t.Label())
			o.Set("url", s.URL)
			optMap("headers", s.Headers)
		} else {
			o.Set("type", "local")
			o.Set("command", s.Command)
			o.Set("args", stringSliceAny(s.Args))
			optMap("env", s.Env)
		}
		o.Set("tools", []any{"*"})
	case FormatGrok, FormatAntigravity:
		if s.Remote() {
			urlKey := "serverUrl"
			if format == FormatGrok {
				urlKey = "url"
			}
			o.Set(urlKey, s.URL)
			if t == TransportSse {
				o.Set("type", "sse")
			}
			optMap("headers", s.Headers)
		} else {
			o.Set("command", s.Command)
			o.Set("args", stringSliceAny(s.Args))
			optMap("env", s.Env)
		}
	case FormatCodex:
		if s.Remote() {
			o.Set("url", s.URL)
			optMap("http_headers", s.Headers)
		} else {
			o.Set("command", s.Command)
			o.Set("args", stringSliceAny(s.Args))
			optMap("env", s.Env)
		}
	}
	return o
}

func getObjStr(o *OrderedObject, k string) string {
	v, ok := o.Get(k)
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

func getObjList(o *OrderedObject, k string) []string {
	v, ok := o.Get(k)
	if !ok {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func getObjMap(o *OrderedObject, k string) map[string]string {
	v, ok := o.Get(k)
	if !ok {
		return nil
	}
	sub, ok := v.(*OrderedObject)
	if !ok || len(sub.Entries) == 0 {
		return nil
	}
	out := make(map[string]string, len(sub.Entries))
	for _, kv := range sub.Entries {
		if s, ok := kv.Value.(string); ok {
			out[kv.Key] = s
		} else if b, err := marshalNoEscape(kv.Value); err == nil {
			out[kv.Key] = string(b)
		}
	}
	return out
}

// DecodeMCP parses a single server entry from an agent's config file.
func DecodeMCP(format McpFormat, name string, v any) (Server, bool) {
	m, ok := v.(*OrderedObject)
	if !ok || m == nil {
		return Server{}, false
	}
	s := Server{Name: name, Transport: TransportStdio}
	setRemote := func(t Transport, u, hk string) {
		s.Transport = t
		s.URL = u
		s.Headers = getObjMap(m, hk)
	}
	setLocal := func(ek string) {
		s.Transport = TransportStdio
		s.Command = getObjStr(m, "command")
		s.Args = getObjList(m, "args")
		s.Env = getObjMap(m, ek)
	}

	switch format {
	case FormatOpenCode:
		if getObjStr(m, "type") == "remote" {
			setRemote(TransportHttp, getObjStr(m, "url"), "headers")
		} else {
			cmd := getObjList(m, "command")
			if len(cmd) == 0 {
				return Server{}, false
			}
			s.Command = cmd[0]
			if len(cmd) > 1 {
				s.Args = cmd[1:]
			}
			s.Env = getObjMap(m, "environment")
		}
	case FormatGemini:
		httpURL := getObjStr(m, "httpUrl")
		u := getObjStr(m, "url")
		if httpURL != "" {
			setRemote(TransportHttp, httpURL, "headers")
		} else if u != "" {
			t := TransportSse
			if getObjStr(m, "type") == "http" {
				t = TransportHttp
			}
			setRemote(t, u, "headers")
		} else {
			setLocal("env")
		}
	case FormatCodex:
		u := getObjStr(m, "url")
		if u == "" {
			setLocal("env")
		} else {
			setRemote(TransportHttp, u, "http_headers")
		}
	default:
		u := getObjStr(m, "url")
		if format == FormatAntigravity && m.Has("serverUrl") {
			u = getObjStr(m, "serverUrl")
		}
		if u == "" {
			setLocal("env")
		} else {
			t := TransportHttp
			if getObjStr(m, "type") == "sse" {
				t = TransportSse
			}
			setRemote(t, u, "headers")
		}
	}

	if (s.Remote() && s.URL == "") || (!s.Remote() && s.Command == "") {
		return Server{}, false
	}
	return s, true
}

type rawMCPEntry struct {
	name  string
	value any
}

func readTextOptional(path string) (string, bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("%s: %w", path, err)
	}
	t := string(b)
	if strings.TrimSpace(t) == "" {
		return "", false, nil
	}
	return t, true, nil
}

// ReadMCP reads every MCP server defined in file, returning an empty map if the
// file does not exist or is empty.
func ReadMCP(file *McpFile) (map[string]Server, error) {
	entries, err := readRawMCP(file)
	if err != nil {
		return nil, err
	}
	out := make(map[string]Server, len(entries))
	for _, e := range entries {
		if s, ok := DecodeMCP(file.Format, e.name, e.value); ok {
			out[e.name] = s
		}
	}
	return out, nil
}

func readRawMCP(file *McpFile) ([]rawMCPEntry, error) {
	text, ok, err := readTextOptional(file.Path)
	if err != nil || !ok {
		return nil, err
	}
	if isTOMLFormat(file.Format) {
		doc, err := parseTOMLMap(text)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", file.Path, err)
		}
		raw, ok := doc[containerKey(file.Format)]
		if !ok {
			return nil, nil
		}
		tbl, ok := raw.(map[string]any)
		if !ok {
			return nil, nil
		}
		keys := make([]string, 0, len(tbl))
		for k := range tbl {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make([]rawMCPEntry, 0, len(keys))
		for _, k := range keys {
			out = append(out, rawMCPEntry{name: k, value: tomlValueToOrdered(tbl[k])})
		}
		return out, nil
	}
	parsed, err := ParseOrderedJSON([]byte(StripJSONC(text)))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file.Path, err)
	}
	root, ok := parsed.(*OrderedObject)
	if !ok {
		return nil, nil
	}
	container, ok := root.Get(containerKey(file.Format))
	if !ok {
		return nil, nil
	}
	obj, ok := container.(*OrderedObject)
	if !ok {
		return nil, nil
	}
	out := make([]rawMCPEntry, 0, len(obj.Entries))
	for _, kv := range obj.Entries {
		out = append(out, rawMCPEntry{name: kv.Key, value: kv.Value})
	}
	return out, nil
}

var managedMCPKeys = []string{
	"type",
	"url",
	"httpUrl",
	"serverUrl",
	"headers",
	"http_headers",
	"command",
	"args",
	"env",
	"environment",
}

func mergedMCP(format McpFormat, old any, s *Server) *OrderedObject {
	next := EncodeMCP(format, s)
	if oldObj, ok := old.(*OrderedObject); ok && oldObj != nil {
		for _, kv := range oldObj.Entries {
			if !isManagedKey(kv.Key) && !next.Has(kv.Key) {
				next.Set(kv.Key, kv.Value)
			}
		}
	}
	return next
}

// WriteMCP puts servers into file and removes named servers atomically.
// Returns true if the file content changed.
func WriteMCP(file *McpFile, put []*Server, remove []string) (bool, error) {
	text, hasText, err := readTextOptional(file.Path)
	if err != nil {
		return false, err
	}
	var out string
	var changed bool
	if isTOMLFormat(file.Format) {
		out, changed, err = writeTOML(file.Format, text, put, remove)
	} else {
		out, changed, err = writeJSONMCP(file.Format, text, hasText, put, remove)
	}
	if err != nil {
		return false, err
	}
	if !changed || (hasText && out == text) {
		return false, nil
	}
	if err := WriteAtomic(file.Path, []byte(out)); err != nil {
		return false, err
	}
	return true, nil
}

func writeJSONMCP(format McpFormat, text string, hasText bool, put []*Server, remove []string) (string, bool, error) {
	var root *OrderedObject
	if hasText {
		if StripJSONC(text) != text {
			return "", false, errors.New("has comments, which siu won't overwrite; edit it by hand")
		}
		parsed, err := ParseOrderedJSON([]byte(text))
		if err != nil {
			return "", false, err
		}
		var ok bool
		root, ok = parsed.(*OrderedObject)
		if !ok {
			return "", false, errors.New("isn't a JSON object")
		}
	} else {
		root = NewOrderedObject()
	}

	key := containerKey(format)
	had := root.Has(key)
	var servers *OrderedObject
	if v, ok := root.Get(key); ok {
		var isObj bool
		servers, isObj = v.(*OrderedObject)
		if !isObj {
			return "", false, fmt.Errorf("%s isn't an object", key)
		}
	} else {
		servers = NewOrderedObject()
		root.Set(key, servers)
	}

	changed := false
	for _, s := range put {
		old, _ := servers.Get(s.Name)
		next := mergedMCP(format, old, s)
		if old == nil || !orderedEqual(old, next) {
			servers.Set(s.Name, next)
			changed = true
		}
	}
	for _, name := range remove {
		if servers.Delete(name) {
			changed = true
		}
	}
	if !changed {
		return "", false, nil
	}
	if !had && servers.Len() == 0 {
		root.Delete(key)
	}
	out, err := marshalPrettyJSON(root)
	if err != nil {
		return "", false, err
	}
	return string(out), true, nil
}

// WriteAtomic writes data through a symlink to its target via a sibling temp file.
// Existing file permissions are preserved; new files are created 0600 on Unix.
func WriteAtomic(path string, data []byte) error {
	target := path
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		target = resolved
	}
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var existingMode os.FileMode
	hasExisting := false
	if info, err := os.Stat(target); err == nil {
		existingMode = info.Mode().Perm()
		hasExisting = true
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(target)+".siu-tmp-*")
	if err != nil {
		return fmt.Errorf("%s: %w", target, err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("%s: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("%s: %w", tmpPath, err)
	}
	if runtime.GOOS != "windows" {
		if hasExisting {
			_ = os.Chmod(tmpPath, existingMode)
		} else {
			_ = os.Chmod(tmpPath, 0o600)
		}
	}
	if err := os.Rename(tmpPath, target); err != nil {
		return fmt.Errorf("%s: %w", target, err)
	}
	cleanup = false
	return nil
}
