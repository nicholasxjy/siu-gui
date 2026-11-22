package core

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestResolvePrefersNewTestHomeAndSupportsLegacy(t *testing.T) {
	legacyHome := t.TempDir()
	t.Setenv("SIU_GUI_TEST_HOME", legacyHome)
	t.Setenv("SIU_PROJECT_TEST_HOME", "")
	t.Setenv("SIU_HOME", t.TempDir())
	if got := Resolve(); got.Env.Home != legacyHome || got.Root != filepath.Join(legacyHome, ".siu") {
		t.Fatalf("legacy test home resolved to home %q and library %q", got.Env.Home, got.Root)
	}

	testHome := t.TempDir()
	t.Setenv("SIU_PROJECT_TEST_HOME", testHome)
	if got := Resolve(); got.Env.Home != testHome || got.Root != filepath.Join(testHome, ".siu") {
		t.Fatalf("new test home resolved to home %q and library %q", got.Env.Home, got.Root)
	}
}

func TestMCPFormatsRoundTripAndTOMLPreservation(t *testing.T) {
	dir := t.TempDir()

	// 1. Claude JSON preserves unrelated keys & unmanaged servers
	claudePath := filepath.Join(dir, ".claude.json")
	if err := os.WriteFile(claudePath, []byte(`{"numStartups":3,"mcpServers":{"mine":{"type":"stdio","command":"me","args":[],"env":{}}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	claudeFile := &McpFile{Path: claudePath, Format: FormatClaude}
	ctx7 := &Server{Name: "ctx7", Transport: TransportHttp, URL: "https://mcp.context7.com/mcp", Headers: map[string]string{"X-Key": "k"}}
	changed, err := WriteMCP(claudeFile, []*Server{ctx7}, nil)
	if err != nil || !changed {
		t.Fatalf("WriteMCP claude: changed=%v err=%v", changed, err)
	}
	got, err := ReadMCP(claudeFile)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["mine"]; !ok {
		t.Fatalf("unmanaged server lost: %+v", got)
	}
	if got["ctx7"].URL != ctx7.URL || got["ctx7"].Headers["X-Key"] != "k" {
		t.Fatalf("ctx7 mismatch: %+v", got["ctx7"])
	}
	rawClaude, _ := os.ReadFile(claudePath)
	if !strings.Contains(string(rawClaude), `"numStartups": 3`) {
		t.Fatalf("unrelated key lost: %s", rawClaude)
	}

	// 2. JSONC comments are readable, but writing refuses to destroy comments
	ocPath := filepath.Join(dir, "opencode.jsonc")
	if err := os.WriteFile(ocPath, []byte("// comment\n{\"mcp\":{\"a\":{\"type\":\"local\",\"command\":[\"npx\",\"-y\",\"pkg\"],},},}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ocFile := &McpFile{Path: ocPath, Format: FormatOpenCode}
	ocServers, err := ReadMCP(ocFile)
	if err != nil {
		t.Fatalf("ReadMCP JSONC: %v", err)
	}
	if ocServers["a"].Command != "npx" || len(ocServers["a"].Args) != 2 {
		t.Fatalf("decoded OpenCode server = %+v", ocServers["a"])
	}
	if _, err := WriteMCP(ocFile, []*Server{ctx7}, nil); err == nil {
		t.Fatal("expected WriteMCP to refuse overwriting JSONC with comments")
	}

	// 3. Codex TOML preserves comments, unrelated tables, and unmanaged keys
	codexPath := filepath.Join(dir, "config.toml")
	origTOML := "# User comment\nmodel = \"o3\"\n\n[mcp_servers.mine]\ncommand = \"my-tool\"\nstartup_timeout_sec = 30\n"
	if err := os.WriteFile(codexPath, []byte(origTOML), 0o644); err != nil {
		t.Fatal(err)
	}
	codexFile := &McpFile{Path: codexPath, Format: FormatCodex}
	stdioSrv := &Server{Name: "fs", Transport: TransportStdio, Command: "npx", Args: []string{"-y", "fs"}, Env: map[string]string{"ROOT": "/tmp"}}
	changed, err = WriteMCP(codexFile, []*Server{stdioSrv, ctx7}, nil)
	if err != nil || !changed {
		t.Fatalf("WriteMCP codex: changed=%v err=%v", changed, err)
	}
	rawTOML, _ := os.ReadFile(codexPath)
	if !strings.Contains(string(rawTOML), "# User comment") || !strings.Contains(string(rawTOML), `model = "o3"`) || !strings.Contains(string(rawTOML), "startup_timeout_sec = 30") {
		t.Fatalf("TOML comments or unrelated fields lost:\n%s", rawTOML)
	}
	codexGot, err := ReadMCP(codexFile)
	if err != nil {
		t.Fatalf("ReadMCP codex: %v", err)
	}
	if codexGot["fs"].Command != "npx" || codexGot["fs"].Env["ROOT"] != "/tmp" {
		t.Fatalf("codex fs = %+v", codexGot["fs"])
	}
	if codexGot["ctx7"].Transport != TransportHttp || codexGot["ctx7"].Headers["X-Key"] != "k" {
		t.Fatalf("codex ctx7 = %+v", codexGot["ctx7"])
	}
	// Remove fs; mine and ctx7 stay
	changed, err = WriteMCP(codexFile, []*Server{ctx7}, []string{"fs"})
	if err != nil || !changed {
		t.Fatalf("WriteMCP codex remove: changed=%v err=%v", changed, err)
	}
	codexGot, err = ReadMCP(codexFile)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := codexGot["fs"]; ok {
		t.Fatal("fs was not removed from codex config.toml")
	}
	if _, ok := codexGot["mine"]; !ok {
		t.Fatal("unmanaged mine was removed from codex config.toml")
	}

	// 4. All other formats encode/decode round trip cleanly
	formats := []struct {
		name   string
		format McpFormat
		srv    Server
	}{
		{"cursor-http", FormatCursor, Server{Name: "r", Transport: TransportHttp, URL: "https://a.dev/mcp", Headers: map[string]string{"A": "1"}}},
		{"desktop-stdio", FormatDesktop, Server{Name: "d", Transport: TransportStdio, Command: "node", Args: []string{"x.js"}, Env: map[string]string{"K": "V"}}},
		{"gemini-http", FormatGemini, Server{Name: "g", Transport: TransportHttp, URL: "https://g.dev/mcp"}},
		{"gemini-sse", FormatGemini, Server{Name: "gs", Transport: TransportSse, URL: "https://g.dev/sse"}},
		{"opencode-http", FormatOpenCode, Server{Name: "o", Transport: TransportHttp, URL: "https://o.dev/mcp", Headers: map[string]string{"Auth": "tok"}}},
		{"copilot-stdio", FormatCopilot, Server{Name: "c", Transport: TransportStdio, Command: "uvx", Args: []string{"tool"}}},
		{"pi-http", FormatPi, Server{Name: "p", Transport: TransportHttp, URL: "https://p.dev/mcp"}},
		{"grok-sse", FormatGrok, Server{Name: "gr", Transport: TransportSse, URL: "https://gr.dev/sse", Headers: map[string]string{"X": "Y"}}},
		{"agy-http", FormatAntigravity, Server{Name: "ag", Transport: TransportHttp, URL: "https://ag.dev/mcp"}},
		{"agy-sse", FormatAntigravity, Server{Name: "ags", Transport: TransportSse, URL: "https://ag.dev/sse"}},
	}
	for _, tc := range formats {
		encoded := EncodeMCP(tc.format, &tc.srv)
		decoded, ok := DecodeMCP(tc.format, tc.srv.Name, encoded)
		if !ok {
			t.Fatalf("%s: DecodeMCP failed for %v", tc.name, encoded)
		}
		if !sameEncodedServer(&decoded, &tc.srv, tc.format) {
			t.Fatalf("%s: round-trip mismatch: got %+v, want %+v", tc.name, decoded, tc.srv)
		}
	}
}

func TestSkillsFrontMatterFindHashAndLink(t *testing.T) {
	m := ParseSkillMeta("---\nname: pdf\ndescription: \"Read: and write PDFs\"\nlicense: MIT\n---\n# body\nname: no")
	if m.Name != "pdf" || m.Description != "Read: and write PDFs" {
		t.Fatalf("ParseSkillMeta = %+v", m)
	}
	m2 := ParseSkillMeta("---\nname: x\ndescription: >\n  folded over\n  two lines\nother: 1\n---\n")
	if m2.Description != "folded over two lines" {
		t.Fatalf("folded description = %q", m2.Description)
	}

	root := t.TempDir()
	mk := func(rel, name string) {
		d := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "SKILL.md"), []byte(fmt.Sprintf("---\nname: %s\ndescription: d\n---\n", name)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("skills/pdf", "pdf")
	mk("skills/pdf/inner", "inner")
	mk("skills/Web Design", "")
	mk(".hidden/x", "x")

	found := FindSkills(root)
	if len(found) != 2 || found[0].Path != "skills/Web Design" || found[0].Name() != "web-design" || found[1].Path != "skills/pdf" || found[1].Name() != "pdf" {
		t.Fatalf("FindSkills = %+v", found)
	}

	target := filepath.Join(root, "lib/100%PATH% & 你好")
	linkPath := filepath.Join(root, "agent/70%TARGET% & 专业 skills/x")
	_ = os.MkdirAll(target, 0o755)
	_ = os.WriteFile(filepath.Join(target, "SKILL.md"), []byte("v1"), 0o644)
	if err := Link(target, linkPath); err != nil {
		t.Fatalf("Link: %v", err)
	}
	if !LinksTo(linkPath, target) {
		t.Fatalf("expected LinksTo=true")
	}
	if err := Unlink(linkPath); err != nil {
		t.Fatalf("Unlink: %v", err)
	}
	if !isFile(filepath.Join(target, "SKILL.md")) {
		t.Fatalf("Unlink touched target")
	}
}

func TestParseSpecWindowsAndGitHub(t *testing.T) {
	home := "/home/u"
	cases := []struct {
		in   string
		want Spec
		ok   bool
	}{
		{"anthropics/skills", Spec{Kind: SpecGithub, Repo: "anthropics/skills", Reference: "HEAD", Subpath: ""}, true},
		{"anthropics/skills/skills/pdf", Spec{Kind: SpecGithub, Repo: "anthropics/skills", Reference: "HEAD", Subpath: "skills/pdf"}, true},
		{"https://github.com/a/b.git", Spec{Kind: SpecGithub, Repo: "a/b", Reference: "HEAD", Subpath: ""}, true},
		{"https://github.com/a/b/tree/main/skills/pdf/", Spec{Kind: SpecGithub, Repo: "a/b", Reference: "main", Subpath: "skills/pdf"}, true},
		{"https://github.com/a/b/blob/v1/x/SKILL.md", Spec{Kind: SpecGithub, Repo: "a/b", Reference: "v1", Subpath: "x"}, true},
		{"/tmp/skills/pdf/SKILL.md", Spec{Kind: SpecLocal, LocalDir: "/tmp/skills/pdf"}, true},
		{`C:\skills\pdf\SKILL.md`, Spec{Kind: SpecLocal, LocalDir: `C:\skills\pdf`}, true},
		{`\\server\share\skills\pdf`, Spec{Kind: SpecLocal, LocalDir: `\\server\share\skills\pdf`}, true},
		{"nope", Spec{}, false},
		{"a b/c", Spec{}, false},
	}
	for _, tc := range cases {
		got, ok := ParseSpec(tc.in, home)
		if ok != tc.ok || got != tc.want {
			t.Errorf("ParseSpec(%q) = (%+v, %v), want (%+v, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestMarketRegistryAndSkillsShParsing(t *testing.T) {
	for _, m := range Featured() {
		if err := m.Server.Validate(); err != nil {
			t.Fatalf("featured server %s invalid: %v", m.ID, err)
		}
	}
	if len(FeaturedMatching("")) < 20 {
		t.Fatalf("expected > 20 featured servers")
	}
	if ShortName("io.github.upstash/context7-mcp") != "context7" {
		t.Fatalf("ShortName mismatch")
	}

	var body map[string]any
	_ = json.Unmarshal([]byte(`{"servers":[
		{"server":{"name":"io.acme/remote-mcp","title":"Remote","remotes":[{"type":"streamable-http","url":"https://r.dev/mcp","headers":[{"name":"Authorization","value":"Bearer {token}","isRequired":true,"isSecret":true}]}]}},
		{"server":{"name":"com.example/gone","remotes":[{"type":"sse","url":"https://gone.dev"}]},"_meta":{"io.modelcontextprotocol.registry/official":{"status":"deleted"}}},
		{"server":{"name":"com.example/templated","remotes":[{"type":"sse","url":"https://{tenant}.dev"}]}}
	]}`), &body)
	parsed := ParseRegistry(body, "remote")
	if len(parsed) != 1 || parsed[0].Server.Name != "remote" || parsed[0].Inputs[0].Format != "Bearer {}" {
		t.Fatalf("ParseRegistry = %+v", parsed)
	}
	filled := parsed[0].Fill([]string{"tok"}, "/h")
	if filled.Headers["Authorization"] != "Bearer tok" {
		t.Fatalf("Fill headers = %+v", filled.Headers)
	}

	page := `x self.__next_f.push([1,"{\"initialSkills\":[{\"source\":\"a/b\",\"skillId\":\"s1\",\"name\":\"s1\",\"installs\":10},{\"source\":\"bad\",\"skillId\":\"x\",\"name\":\"x\"}],\"more\":1}"])`
	pop, err := ParsePopularSkills(page)
	if err != nil || len(pop) != 1 || pop[0].Key() != "a/b/s1" {
		t.Fatalf("ParsePopularSkills = (%+v, %v)", pop, err)
	}
	if got := MetaDescription(`<head><meta name="description" content="Make &amp; read PDFs"/></head>`); got != "Make & read PDFs" {
		t.Fatalf("MetaDescription = %q", got)
	}
}

func TestRegistryCacheOfflineFallbackAndTrim(t *testing.T) {
	dir := t.TempDir()
	offered := Featured()[:1]
	first, warnings, cached := SearchRegistryCached(dir, "Context7", false, func() ([]MarketServer, error) {
		return offered, nil
	})
	if len(first) != 1 || len(warnings) != 0 || cached {
		t.Fatalf("first search = (%d, %v, %v)", len(first), warnings, cached)
	}
	fresh, warnings, cached := SearchRegistryCached(dir, " context7 ", false, func() ([]MarketServer, error) {
		t.Fatal("fresh cache should avoid network")
		return nil, nil
	})
	if len(fresh) != 1 || len(warnings) != 0 || !cached {
		t.Fatalf("fresh cache = (%d, %v, %v)", len(fresh), warnings, cached)
	}
	offline, warnings, cached := SearchRegistryCached(dir, "context7", true, func() ([]MarketServer, error) {
		return nil, errors.New("offline")
	})
	if len(offline) != 1 || len(warnings) == 0 || !strings.Contains(warnings[0], "offline") || !cached {
		t.Fatalf("offline fallback = (%d, %v, %v)", len(offline), warnings, cached)
	}
	for n := 0; n < 105; n++ {
		WriteCache(dir, fmt.Sprintf("mcp-registry-%d", n), []int{n})
	}
	_ = os.WriteFile(filepath.Join(dir, "skills-popular.json"), []byte("retained"), 0o644)
	trimRegistryCache(dir)
	entries, _ := os.ReadDir(dir)
	if len(entries) != 101 {
		t.Fatalf("expected 101 files after trim, got %d", len(entries))
	}
}

type fakeRemote struct {
	versions  map[string]string
	trees     map[string]string
	downloads int
}

func (f *fakeRemote) Latest(_ context.Context, _ Eco, name string) (string, error) {
	if v, ok := f.versions[name]; ok {
		return v, nil
	}
	return "", fmt.Errorf("%s not found", name)
}

func (f *fakeRemote) Pull(_ context.Context, _ string) error { return nil }

func (f *fakeRemote) Download(_ context.Context, repo, _ string) (string, string, error) {
	f.downloads++
	src, ok := f.trees[repo]
	if !ok {
		return "", "", fmt.Errorf("missing %s", repo)
	}
	tmp, err := os.MkdirTemp("", "siu-fake-*")
	if err != nil {
		return "", "", err
	}
	if err := CopyDir(src, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return "", "", err
	}
	return tmp, tmp, nil
}

func TestUpdateAllMCPAndSkills(t *testing.T) {
	home := t.TempDir()
	_ = os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	_ = os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	store, err := OpenStore(EnvRooted(home), filepath.Join(home, ".siu"))
	if err != nil {
		t.Fatal(err)
	}
	servers := []Server{
		{Name: "pinned", Command: "npx", Args: []string{"-y", "@a/pinned@1.0.0"}},
		{Name: "loose", Command: "npx", Args: []string{"-y", "loose"}},
		{Name: "tagged", Command: "npx", Args: []string{"-y", "tagged@latest"}},
		{Name: "current", Command: "uvx", Args: []string{"current==2.0"}},
		{Name: "gone", Command: "npx", Args: []string{"gone"}},
		{Name: "img", Command: "docker", Args: []string{"run", "-i", "--rm", "ghcr.io/a/img"}},
		{Name: "own", Command: "node", Args: []string{"s.js"}},
		{Name: "web", Transport: TransportHttp, URL: "https://x.dev/mcp"},
	}
	for _, s := range servers {
		if err := store.AddServer(s, store.DefaultServerAgents(&s), ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Commit(); err != nil {
		t.Fatal(err)
	}

	fake := &fakeRemote{
		versions: map[string]string{
			"@a/pinned": "1.4.0",
			"loose":     "3.1.0",
			"current":   "2.0",
		},
	}
	var out bytes.Buffer
	failed, err := UpdateAllMCP(context.Background(), store, fake, &out)
	if err != nil {
		t.Fatal(err)
	}
	if failed != 1 {
		t.Fatalf("failed = %d, want 1\n%s", failed, out.String())
	}
	text := out.String()
	for _, want := range []string{
		"@a/pinned 1.0.0 → 1.4.0",
		"loose unpinned → 3.1.0",
		"tagged@latest follows its releases already",
		"current 2.0 is the latest",
		"couldn't look up gone",
		"pulled ghcr.io/a/img",
		"runs node, not a package siu can update",
		"remote, nothing to update",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	if store.Server("loose").Server.Args[1] != "loose@3.1.0" {
		t.Fatalf("loose args = %v", store.Server("loose").Server.Args)
	}
}

func TestProxyValidationBypassHTTPAndSOCKS5(t *testing.T) {
	root := t.TempDir()
	def, err := LoadProxy(root)
	if err != nil || def.Mode != ProxyModeSystem {
		t.Fatalf("LoadProxy default = (%+v, %v)", def, err)
	}
	saved, err := SaveProxy(root, ProxySettings{
		Mode:    ProxyModeCustom,
		URL:     " socks5h://Alice:Pa%40sS@127.0.0.1:1080 ",
		NoProxy: "localhost,*.local,10.0.0.0/8",
	})
	if err != nil {
		t.Fatalf("SaveProxy valid: %v", err)
	}
	for _, bad := range []string{"", "not a URL", "ftp://localhost:21", "http://localhost:0", "http://localhost:80/path", "http://localhost:80?password=secret"} {
		if _, err := SaveProxy(root, ProxySettings{Mode: ProxyModeCustom, URL: bad}); err == nil {
			t.Errorf("expected error for bad proxy URL %q", bad)
		}
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(filepath.Join(root, "proxy.json"))
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("proxy.json perm = %o, want 0600", info.Mode().Perm())
		}
	}

	// Bypass checks
	for _, raw := range []string{"http://localhost/x", "https://host.local", "http://10.2.3.4/x"} {
		u, _ := url.Parse(raw)
		routes, err := ResolveProxyRoutes(saved, u)
		if err != nil || len(routes) != 1 || routes[0] != nil {
			t.Fatalf("expected bypass for %s, got (%v, %v)", raw, routes, err)
		}
	}

	// Env vs OS precedence
	target, _ := url.Parse("https://github.com/x")
	osProxy := func() (*systemProxySnapshot, error) {
		return &systemProxySnapshot{httpsProxy: parseLenientProxyURL("http://127.0.0.1:7890")}, nil
	}
	routes, err := resolveProxyRoutesWith(DefaultProxySettings(), target, envProxyConfig{noProxy: "localhost"}, osProxy)
	if err != nil || routes[0] == nil || routes[0].Port() != "7890" {
		t.Fatalf("NO_PROXY alone should keep system proxy, got %v err %v", routes, err)
	}
	routes, err = resolveProxyRoutesWith(DefaultProxySettings(), target, envProxyConfig{allProxy: "socks5h://127.0.0.1:1080"}, func() (*systemProxySnapshot, error) {
		t.Fatal("explicit env proxy must win over OS")
		return nil, nil
	})
	if err != nil || routes[0] == nil || routes[0].Port() != "1080" {
		t.Fatalf("explicit env proxy = %v err %v", routes, err)
	}

	// Real HTTP proxy + redirect to bypassed target
	directSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("direct"))
	}))
	defer directSrv.Close()

	var sawProxyAuth string
	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawProxyAuth = r.Header.Get("Proxy-Authorization")
		http.Redirect(w, r, directSrv.URL+"/ok", http.StatusFound)
	}))
	defer proxySrv.Close()

	pu, _ := url.Parse(proxySrv.URL)
	settings := ProxySettings{
		Mode:    ProxyModeCustom,
		URL:     fmt.Sprintf("http://alice:pa%%40ss@%s", pu.Host),
		NoProxy: "127.0.0.1",
	}
	body, err := httpGetWithTimeout(context.Background(), &settings, "http://downloads.invalid/x", "*/*", 1024, 5*time.Second)
	if err != nil || string(body) != "direct" {
		t.Fatalf("HTTP proxy redirect bypass: body=%q err=%v", body, err)
	}
	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("alice:pa@ss"))
	if sawProxyAuth != wantAuth {
		t.Fatalf("Proxy-Authorization = %q, want %q", sawProxyAuth, wantAuth)
	}

	// Real minimal SOCKS5 server test
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 512)
		n, err := conn.Read(buf)
		if err != nil || n < 2 || buf[0] != 5 {
			return
		}
		_, _ = conn.Write([]byte{5, 0}) // no auth
		n, err = conn.Read(buf)
		if err != nil || n < 4 || buf[0] != 5 || buf[1] != 1 {
			return
		}
		_, _ = conn.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
		n, err = conn.Read(buf)
		if err != nil || !strings.HasPrefix(string(buf[:n]), "GET /x ") {
			return
		}
		_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok")
	}()
	s5Settings := ProxySettings{Mode: ProxyModeCustom, URL: "socks5h://" + ln.Addr().String()}
	body, err = httpGetWithTimeout(context.Background(), &s5Settings, "http://downloads.invalid/x", "*/*", 1024, 5*time.Second)
	if err != nil || string(body) != "ok" {
		t.Fatalf("SOCKS5 request: body=%q err=%v", body, err)
	}
}

func TestBackupInteropFixtureAndAutoRestore(t *testing.T) {
	home := t.TempDir()
	_ = os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	store, err := OpenStore(EnvRooted(home), filepath.Join(home, ".siu"))
	if err != nil {
		t.Fatal(err)
	}

	fixturePath, err := filepath.Abs(filepath.Join("testdata", "siu-backup-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	prev, err := PreviewBackup(store, home, fixturePath)
	if err != nil {
		t.Fatalf("PreviewBackup fixture: %v", err)
	}
	if prev.MCP != 2 || prev.Skills != 1 || prev.Theme != nil {
		t.Fatalf("unexpected fixture preview: %+v", prev)
	}
	restored, err := ImportBackup(store, home, fixturePath, prev.Digest)
	if err != nil {
		t.Fatalf("ImportBackup fixture: %v", err)
	}
	if restored.Theme != nil {
		t.Fatalf("CLI fixture should have nil theme, got %v", *restored.Theme)
	}
	// Verify rebased args, env, binary file, and executable bit
	portable := store.Server("portable")
	if portable == nil {
		t.Fatal("portable server missing")
	}
	wantArg := "--data=" + filepath.Join(store.Root, "skills", "portable")
	if portable.Server.Args[1] != wantArg {
		t.Fatalf("rebased arg = %q, want %q", portable.Server.Args[1], wantArg)
	}
	if portable.Server.Env["TOKEN"] != "Bearer fixture-token" {
		t.Fatalf("credential altered: %q", portable.Server.Env["TOKEN"])
	}
	binData, err := os.ReadFile(filepath.Join(store.SkillDir("portable"), "assets", "opaque.bin"))
	if err != nil || len(binData) != 6 {
		t.Fatalf("binary asset mismatch: len=%d err=%v", len(binData), err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(store.SkillDir("portable"), "scripts", "run.sh"))
		if err != nil || info.Mode().Perm()&0o100 == 0 {
			t.Fatalf("run.sh should be executable, mode=%v err=%v", info.Mode(), err)
		}
	}
	// Paused server stays paused and is not written to Claude
	if store.Server("paused").Enabled {
		t.Fatal("paused server should stay paused")
	}
	claudeServers, err := ReadMCP(store.Agent("claude").MCP)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := claudeServers["paused"]; ok {
		t.Fatal("paused server should not be written to claude")
	}
	if _, ok := claudeServers["portable"]; !ok {
		t.Fatal("portable server should be written to claude")
	}

	// Re-export with theme and verify path traversal rejection
	theme := "dark"
	outPath := filepath.Join(home, "reexport.json")
	exp, err := ExportBackup(store, home, outPath, &theme)
	if err != nil || exp.MCP != 2 || exp.Skills != 1 {
		t.Fatalf("ExportBackup = (%+v, %v)", exp, err)
	}
	for _, bad := range []string{"../escape", "/absolute", "C:/escape", `a\b`, "NUL.txt", "a/COM1", "file:stream", "a/."} {
		if _, err := validateRelativeBackupPath(bad); err == nil {
			t.Errorf("validateRelativeBackupPath accepted %q", bad)
		}
	}
}
