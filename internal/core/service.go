package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Service is the in-process siu core engine that handles every protocol method
// in docs/protocol.md. Mutating requests are serialized under writeLock while
// reads and network discovery/scans run concurrently.
type Service struct {
	writeLock sync.Mutex
	scans     *ScanManager
	version   string
	terminal  bool

	marketMu    sync.Mutex
	marketCache []MarketServer
}

// NewService constructs a core Service reporting version in Snapshot.version.
func NewService(version string) *Service {
	if version == "" {
		version = "0.0.9"
	}
	return &Service{
		scans:   NewScanManager(),
		version: version,
	}
}

// NewTerminalService constructs a service that skips agent instructions, including in backups.
func NewTerminalService(version string) *Service {
	s := NewService(version)
	s.terminal = true
	return s
}

// Close releases any retained temporary scan directories.
func (s *Service) Close() error {
	if s == nil {
		return nil
	}
	s.scans.Close()
	return nil
}

type requestEnvelope struct {
	ID     json.RawMessage            `json:"id,omitempty"`
	Method string                     `json:"method"`
	Params map[string]json.RawMessage `json:"params"`
}

type responseEnvelope struct {
	ID    json.RawMessage `json:"id,omitempty"`
	OK    bool            `json:"ok"`
	Data  any             `json:"data,omitempty"`
	Error string          `json:"error,omitempty"`
}

// Request handles a raw JSON payload (`{"method":...,"params":...}`) and
// returns the raw JSON response (`{"ok":true,"data":...}` or `{"ok":false,"error":...}`).
// Panics inside handlers are recovered into a protocol error response.
func (s *Service) Request(ctx context.Context, payload string) (string, error) {
	if strings.TrimSpace(payload) == "" {
		return encodeResponse(responseEnvelope{OK: false, Error: "invalid JSON: empty payload"}), nil
	}
	var req requestEnvelope
	if err := json.Unmarshal([]byte(payload), &req); err != nil {
		return encodeResponse(responseEnvelope{OK: false, Error: fmt.Sprintf("invalid JSON: %v", err)}), nil
	}
	if req.Method == "" {
		return encodeResponse(responseEnvelope{ID: req.ID, OK: false, Error: "method is required"}), nil
	}
	if req.Params == nil {
		req.Params = map[string]json.RawMessage{}
	}

	var (
		data any
		err  error
	)
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = errors.New("internal error handling the request")
			}
		}()
		if isMutatingMethod(req.Method) {
			s.writeLock.Lock()
			defer s.writeLock.Unlock()
		}
		data, err = s.dispatch(ctx, req.Method, req.Params)
	}()

	if err != nil {
		return encodeResponse(responseEnvelope{ID: req.ID, OK: false, Error: err.Error()}), nil
	}
	return encodeResponse(responseEnvelope{ID: req.ID, OK: true, Data: data}), nil
}

func encodeResponse(resp responseEnvelope) string {
	b, err := marshalNoEscape(resp)
	if err != nil {
		return `{"ok":false,"error":"internal encoding error"}`
	}
	return string(b)
}

func isMutatingMethod(method string) bool {
	switch method {
	case "set_proxy_settings",
		"export_config", "preview_config", "import_config",
		"sync", "save_server", "set_agents", "set_enabled",
		"delete", "import", "install_skills", "install_server",
		"update", "clear", "save_instruction", "delete_instruction", "set_instruction_enabled":
		return true
	default:
		return false
	}
}

func (s *Service) dispatch(ctx context.Context, method string, params map[string]json.RawMessage) (any, error) {
	switch method {
	case "snapshot":
		return s.hSnapshot()
	case "export_config", "preview_config", "import_config":
		return s.hConfig(method, params)
	case "proxy_settings":
		return CurrentProxy()
	case "set_proxy_settings":
		return s.hSetProxySettings(params)
	case "sync":
		return s.hSync()
	case "save_server":
		return s.hSaveServer(params)
	case "set_agents":
		return s.hSetAgents(params)
	case "set_enabled":
		return s.hSetEnabled(params)
	case "delete":
		return s.hDelete(params)
	case "instruction_content", "save_instruction", "delete_instruction", "set_instruction_enabled":
		if !s.terminal {
			return s.hInstruction(method, params)
		}
	case "import":
		return s.hImport(params)
	case "scan_skills":
		return s.hScanSkills(ctx, params)
	case "install_skills":
		return s.hInstallSkills(params)
	case "release_scan":
		return s.hReleaseScan(params)
	case "skill_content":
		return s.hSkillContent(params)
	case "discover":
		return s.hDiscover(ctx, params)
	case "install_server":
		return s.hInstallServer(params)
	case "update":
		return s.hUpdate(ctx, params)
	case "clear":
		return s.hClear(params)
	}
	return nil, fmt.Errorf("unknown method %q", method)
}

// ---- Param decoding helpers ----

func paramStr(params map[string]json.RawMessage, key string) (string, error) {
	raw, ok := params[key]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return "", fmt.Errorf("%s is required", key)
	}
	var v string
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", fmt.Errorf("%s is required", key)
	}
	return v, nil
}

func optParamStr(params map[string]json.RawMessage, key string) *string {
	raw, ok := params[key]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var v string
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	return &v
}

func paramBool(params map[string]json.RawMessage, key string) (bool, error) {
	raw, ok := params[key]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return false, fmt.Errorf("%s is required", key)
	}
	var v bool
	if err := json.Unmarshal(raw, &v); err != nil {
		return false, fmt.Errorf("%s is required", key)
	}
	return v, nil
}

func optParamBool(params map[string]json.RawMessage, key string) bool {
	raw, ok := params[key]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return false
	}
	var v bool
	_ = json.Unmarshal(raw, &v)
	return v
}

func paramStringSlice(params map[string]json.RawMessage, key string) ([]string, error) {
	raw, ok := params[key]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return []string{}, nil
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, fmt.Errorf("%s must be an array of strings", key)
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		var str string
		if err := json.Unmarshal(item, &str); err != nil {
			return nil, fmt.Errorf("%s must be an array of strings", key)
		}
		out = append(out, str)
	}
	return out, nil
}

func openResolvedStore() (Resolved, *Store, error) {
	resolved := Resolve()
	store, err := OpenStore(resolved.Env, resolved.Root)
	if err != nil {
		return Resolved{}, nil, err
	}
	return resolved, store, nil
}

// ---- Handlers ----

func (s *Service) hSetProxySettings(params map[string]json.RawMessage) (any, error) {
	raw, ok := params["proxy"]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return nil, errors.New("proxy is required")
	}
	settings, err := ParseProxySettingsJSON(raw)
	if err != nil {
		return nil, err
	}
	return SaveProxy(Resolve().Root, settings)
}

func (s *Service) applyDefaultIfPending(resolved Resolved) (*AppliedConfig, []string, error) {
	cfgPath := filepath.Join(resolved.Root, "config.json")
	raw, err := readBounded(cfgPath, MaxBackupBytes)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("无法读取 config.json: %w", err)
	}
	currentDigest := backupDigest(raw)
	raw = nil

	applied, err := ReadAppliedConfig(resolved.Root)
	if err != nil {
		return nil, nil, err
	}
	if applied != nil && applied.Digest == currentDigest {
		return applied, nil, nil
	}

	s.writeLock.Lock()
	defer s.writeLock.Unlock()
	store, err := OpenStore(resolved.Env, resolved.Root)
	if err != nil {
		return nil, nil, err
	}
	restored, err := applyDefaultBackup(store, resolved.Env.Home, s.terminal)
	if err != nil {
		return nil, nil, err
	}
	if restored != nil {
		return &AppliedConfig{
			Digest: restored.Digest,
			Theme:  restored.Theme,
		}, restored.Report.Issues, nil
	}
	if isFile(cfgPath) {
		applied, err = ReadAppliedConfig(resolved.Root)
		if err != nil {
			return nil, nil, err
		}
		return applied, nil, nil
	}
	return nil, nil, nil
}

func (s *Service) hSnapshot() (any, error) {
	resolved := Resolve()
	applied, autoIssues, err := s.applyDefaultIfPending(resolved)
	if err != nil {
		autoIssues = append(autoIssues, fmt.Sprintf("config.json 自动恢复失败：%v", err))
		applied = nil
	}
	store, err := OpenStore(resolved.Env, resolved.Root)
	if err != nil {
		return nil, err
	}
	snap := s.buildSnapshot(store, resolved.Env.Home)
	snap.Issues = append(snap.Issues, autoIssues...)
	snap.AutoConfig = applied
	return snap, nil
}

func (s *Service) hConfig(method string, params map[string]json.RawMessage) (any, error) {
	p, err := paramStr(params, "path")
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(p) {
		return nil, errors.New("请选择绝对路径的 JSON 文件")
	}
	resolved, store, err := openResolvedStore()
	if err != nil {
		return nil, err
	}
	switch method {
	case "export_config":
		exported, err := ExportBackup(store, resolved.Env.Home, p, optParamStr(params, "theme"))
		if err != nil {
			return nil, fmt.Errorf("无法导出配置备份: %w", err)
		}
		return map[string]any{
			"path":       p,
			"mcpCount":   exported.MCP,
			"skillCount": exported.Skills,
		}, nil
	case "preview_config":
		prev, err := previewBackup(store, resolved.Env.Home, p, s.terminal)
		if err != nil {
			return nil, fmt.Errorf("无法预览配置备份: %w", err)
		}
		issues := prev.Issues
		if issues == nil {
			issues = []string{}
		}
		return map[string]any{
			"path":       p,
			"digest":     prev.Digest,
			"exportedAt": prev.ExportedAt,
			"mcpCount":   prev.MCP,
			"skillCount": prev.Skills,
			"issues":     issues,
			"theme":      prev.Theme,
		}, nil
	case "import_config":
		digest, err := paramStr(params, "digest")
		if err != nil {
			return nil, err
		}
		restored, err := importBackup(store, resolved.Env.Home, p, digest, s.terminal)
		if err != nil {
			return nil, fmt.Errorf("无法恢复配置备份: %w", err)
		}
		logMsg := fmt.Sprintf("恢复前的配置已备份到 %s", restored.Saved)
		res := s.buildMutationResult(store, resolved.Env.Home, restored.Report, "配置已恢复", &logMsg)
		res.Theme = restored.Theme
		return res, nil
	default:
		return nil, fmt.Errorf("unknown method %q", method)
	}
}

func (s *Service) hSync() (any, error) {
	resolved, store, err := openResolvedStore()
	if err != nil {
		return nil, err
	}
	report, err := store.Commit()
	if err != nil {
		return nil, err
	}
	pruneIfSafe(store, report)
	return s.buildMutationResult(store, resolved.Env.Home, report, "Synced", nil), nil
}

func (s *Service) hSaveServer(params map[string]json.RawMessage) (any, error) {
	rawSrv, ok := params["server"]
	if !ok || len(rawSrv) == 0 || string(rawSrv) == "null" {
		return nil, errors.New("server is required")
	}
	srv, err := DecodeServerRaw(rawSrv)
	if err != nil {
		return nil, err
	}
	if err := srv.Validate(); err != nil {
		return nil, err
	}
	agents, err := paramStringSlice(params, "agents")
	if err != nil {
		return nil, err
	}
	oldName := optParamStr(params, "oldName")
	origin := optParamStr(params, "origin")

	resolved, store, err := openResolvedStore()
	if err != nil {
		return nil, err
	}
	targets, err := ValidateTargets("mcp", agents, store, &srv)
	if err != nil {
		return nil, err
	}
	if err := CheckMCPCollision(store, srv.Name, targets); err != nil {
		return nil, err
	}

	if oldName != nil {
		if store.Server(*oldName) == nil {
			return nil, fmt.Errorf("no server called %s", *oldName)
		}
		if err := store.EditServer(*oldName, srv); err != nil {
			return nil, err
		}
		if err := store.SetServerAgents(srv.Name, targets); err != nil {
			return nil, err
		}
	} else {
		if store.Server(srv.Name) != nil {
			return nil, fmt.Errorf("the library already has a server called %s", srv.Name)
		}
		originStr := ""
		if origin != nil {
			originStr = *origin
		}
		if err := store.AddServer(srv, targets, originStr); err != nil {
			return nil, err
		}
	}
	report, err := store.Commit()
	if err != nil {
		return nil, err
	}
	return s.buildMutationResult(store, resolved.Env.Home, report, fmt.Sprintf("Saved %s", srv.Name), nil), nil
}

func (s *Service) hSetAgents(params map[string]json.RawMessage) (any, error) {
	kind, err := paramStr(params, "kind")
	if err != nil {
		return nil, err
	}
	name, err := paramStr(params, "name")
	if err != nil {
		return nil, err
	}
	agents, err := paramStringSlice(params, "agents")
	if err != nil {
		return nil, err
	}
	resolved, store, err := openResolvedStore()
	if err != nil {
		return nil, err
	}
	var srvForCheck *Server
	if kind == "mcp" {
		e := store.Server(name)
		if e == nil {
			return nil, fmt.Errorf("no server called %s", name)
		}
		cp := e.Server.Clone()
		srvForCheck = &cp
	}
	targets, err := ValidateTargets(kind, agents, store, srvForCheck)
	if err != nil {
		return nil, err
	}
	switch kind {
	case "mcp":
		if err := CheckMCPCollision(store, name, targets); err != nil {
			return nil, err
		}
		if err := store.SetServerAgents(name, targets); err != nil {
			return nil, err
		}
	case "skills":
		if err := store.SetSkillAgents(name, targets); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown kind %q", kind)
	}
	report, err := store.Commit()
	if err != nil {
		return nil, err
	}
	return s.buildMutationResult(store, resolved.Env.Home, report, "Updated agents", nil), nil
}

func (s *Service) hSetEnabled(params map[string]json.RawMessage) (any, error) {
	kind, err := paramStr(params, "kind")
	if err != nil {
		return nil, err
	}
	name, err := paramStr(params, "name")
	if err != nil {
		return nil, err
	}
	enabled, err := paramBool(params, "enabled")
	if err != nil {
		return nil, err
	}
	resolved, store, err := openResolvedStore()
	if err != nil {
		return nil, err
	}
	switch kind {
	case "mcp":
		e := store.Server(name)
		if e == nil {
			return nil, fmt.Errorf("no server called %s", name)
		}
		if e.Enabled != enabled {
			if _, err := store.ToggleServerEnabled(name); err != nil {
				return nil, err
			}
		}
	case "skills":
		e := store.Skill(name)
		if e == nil {
			return nil, fmt.Errorf("no skill called %s", name)
		}
		if e.Enabled != enabled {
			if _, err := store.ToggleSkillEnabled(name); err != nil {
				return nil, err
			}
		}
	default:
		return nil, fmt.Errorf("unknown kind %q", kind)
	}
	report, err := store.Commit()
	if err != nil {
		return nil, err
	}
	return s.buildMutationResult(store, resolved.Env.Home, report, "Updated", nil), nil
}

func (s *Service) hDelete(params map[string]json.RawMessage) (any, error) {
	kind, err := paramStr(params, "kind")
	if err != nil {
		return nil, err
	}
	name, err := paramStr(params, "name")
	if err != nil {
		return nil, err
	}
	resolved, store, err := openResolvedStore()
	if err != nil {
		return nil, err
	}
	switch kind {
	case "mcp":
		if err := store.RemoveServer(name); err != nil {
			return nil, err
		}
	case "skills":
		if err := store.RemoveSkill(name); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown kind %q", kind)
	}
	report, err := store.Commit()
	if err != nil {
		return nil, err
	}
	if kind == "skills" {
		pruneIfSafe(store, report)
	}
	return s.buildMutationResult(store, resolved.Env.Home, report, fmt.Sprintf("Deleted %s", name), nil), nil
}

func (s *Service) hImport(params map[string]json.RawMessage) (any, error) {
	kind, err := paramStr(params, "kind")
	if err != nil {
		return nil, err
	}
	name, err := paramStr(params, "name")
	if err != nil {
		return nil, err
	}
	resolved, store, err := openResolvedStore()
	if err != nil {
		return nil, err
	}
	switch kind {
	case "mcp":
		var picked *FoundServer
		for _, f := range store.FoundServers() {
			if f.Server.Name == name {
				cp := f
				picked = &cp
				break
			}
		}
		if picked == nil {
			return nil, fmt.Errorf("%s isn't an unmanaged MCP server right now", name)
		}
		if err := store.ImportServer(*picked); err != nil {
			return nil, err
		}
	case "skills":
		var picked *FoundSkill
		for _, f := range store.FoundSkills() {
			if f.Name == name {
				cp := f
				picked = &cp
				break
			}
		}
		if picked == nil {
			return nil, fmt.Errorf("%s isn't an unmanaged skill right now", name)
		}
		if err := store.ImportSkill(*picked); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown kind %q", kind)
	}
	report, err := store.Commit()
	if err != nil {
		return nil, err
	}
	return s.buildMutationResult(store, resolved.Env.Home, report, fmt.Sprintf("Imported %s", name), nil), nil
}

func (s *Service) hScanSkills(ctx context.Context, params map[string]json.RawMessage) (any, error) {
	source, err := paramStr(params, "source")
	if err != nil {
		return nil, err
	}
	resolved, store, err := openResolvedStore()
	if err != nil {
		return nil, err
	}
	spec, ok := ParseSpec(source, resolved.Env.Home)
	if !ok {
		return nil, errors.New("use owner/repo, a github.com URL, or a local path")
	}
	switch spec.Kind {
	case SpecLocal:
		if !isDir(spec.LocalDir) {
			return nil, fmt.Errorf("%s isn't a folder", spec.LocalDir)
		}
		canon, err := filepath.EvalSymlinks(spec.LocalDir)
		if err != nil {
			canon = spec.LocalDir
		}
		found := FindSkills(canon)
		if len(found) == 0 {
			return nil, fmt.Errorf("no SKILL.md under %s", canon)
		}
		tok := s.scans.Insert(source, canon, ScanOrigin{Kind: "local"}, found, "")
		return EncodeScanResultDTO(tok, source, found, store), nil
	case SpecGithub:
		tmp, root, err := DownloadGitHub(ctx, spec.Repo, spec.Reference)
		if err != nil {
			return nil, err
		}
		all := FindSkills(root)
		var found []FoundSkillFile
		for _, f := range all {
			if spec.Subpath == "" || f.Path == spec.Subpath || strings.HasPrefix(f.Path, spec.Subpath+"/") {
				found = append(found, f)
			}
		}
		if len(found) == 0 {
			_ = os.RemoveAll(tmp)
			suffix := ""
			if spec.Subpath != "" {
				suffix = "/" + spec.Subpath
			}
			return nil, fmt.Errorf("no SKILL.md found in %s%s", spec.Repo, suffix)
		}
		tok := s.scans.Insert(source, root, ScanOrigin{
			Kind:      "github",
			Repo:      spec.Repo,
			Reference: spec.Reference,
		}, found, tmp)
		return EncodeScanResultDTO(tok, source, found, store), nil
	default:
		return nil, errors.New("use owner/repo, a github.com URL, or a local path")
	}
}

func (s *Service) hInstallSkills(params map[string]json.RawMessage) (any, error) {
	token, err := paramStr(params, "token")
	if err != nil {
		return nil, err
	}
	paths, err := paramStringSlice(params, "paths")
	if err != nil {
		return nil, err
	}
	agents, err := paramStringSlice(params, "agents")
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, errors.New("pick at least one skill")
	}
	resolved, store, err := openResolvedStore()
	if err != nil {
		return nil, err
	}
	targets, err := ValidateTargets("skills", agents, store, nil)
	if err != nil {
		return nil, err
	}

	var (
		picked   []FoundSkillFile
		origin   ScanOrigin
		pickErr  error
	)
	foundToken := s.scans.With(token, func(entry *ScanEntry) {
		origin = entry.Origin
		for _, p := range paths {
			var match *FoundSkillFile
			for i := range entry.Found {
				if entry.Found[i].Path == p {
					match = &entry.Found[i]
					break
				}
			}
			if match == nil {
				pickErr = fmt.Errorf("%s isn't part of this scan", p)
				return
			}
			picked = append(picked, *match)
		}
	})
	if !foundToken {
		return nil, errors.New("scan token expired or unknown")
	}
	if pickErr != nil {
		return nil, pickErr
	}

	names, err := store.InstallSkills(picked, func(f FoundSkillFile) *Source {
		if origin.Kind == "github" {
			return &Source{
				Kind: "github",
				Repo: origin.Repo,
				Ref:  origin.Reference,
				Path: f.Path,
			}
		}
		return &Source{Kind: "local", Dir: f.Dir}
	}, targets)
	if err != nil {
		return nil, err
	}
	report, err := store.Commit()
	if err != nil {
		return nil, err
	}
	return s.buildMutationResult(store, resolved.Env.Home, report, fmt.Sprintf("Installed %s", strings.Join(names, ", ")), nil), nil
}

func (s *Service) hReleaseScan(params map[string]json.RawMessage) (any, error) {
	token, err := paramStr(params, "token")
	if err != nil {
		return nil, err
	}
	s.scans.Release(token)
	return map[string]any{}, nil
}

func (s *Service) hSkillContent(params map[string]json.RawMessage) (any, error) {
	name, err := paramStr(params, "name")
	if err != nil {
		return nil, err
	}
	_, store, err := openResolvedStore()
	if err != nil {
		return nil, err
	}
	if store.Skill(name) == nil {
		return nil, fmt.Errorf("no skill called %s", name)
	}
	path := filepath.Join(store.SkillDir(name), "SKILL.md")
	const limit = 1 << 20
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > limit {
		return nil, errors.New("SKILL.md is larger than 1 MiB")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"content": string(content),
		"path":    path,
	}, nil
}

func (s *Service) hDiscover(ctx context.Context, params map[string]json.RawMessage) (any, error) {
	kind, err := paramStr(params, "kind")
	if err != nil {
		return nil, err
	}
	query := ""
	if q := optParamStr(params, "query"); q != nil {
		query = *q
	}
	refresh := optParamBool(params, "refresh")
	resolved := Resolve()
	warnings := []string{}

	switch kind {
	case "mcp":
		servers := FeaturedMatching(query)
		fromCache := false
		if strings.TrimSpace(query) != "" {
			list, problems, cached := SearchRegistryCached(
				filepath.Join(resolved.Root, "cache"),
				query,
				refresh,
				func() ([]MarketServer, error) {
					return SearchRegistry(ctx, query)
				},
			)
			servers = append(servers, list...)
			warnings = append(warnings, problems...)
			fromCache = cached
		}
		seen := map[string]bool{}
		deduped := make([]MarketServer, 0, len(servers))
		for _, srv := range servers {
			if !seen[srv.ID] {
				seen[srv.ID] = true
				deduped = append(deduped, srv)
			}
		}
		s.rememberMarketServers(deduped)
		dtoServers := make([]MarketServerDTO, 0, len(deduped))
		for _, m := range deduped {
			dtoServers = append(dtoServers, EncodeMarketServerDTO(m))
		}
		return DiscoveryDTO{
			Servers:  dtoServers,
			Skills:   []MarketSkill{},
			Warnings: warnings,
			Cached:   fromCache,
		}, nil
	case "skills":
		cacheDir := filepath.Join(resolved.Root, "cache")
		cachedFile := ReadCache[[]MarketSkill](cacheDir, "skills-popular", time.Hour)
		needFetch := refresh || cachedFile == nil || !cachedFile.Fresh
		var index []MarketSkill
		fromCache := false
		if needFetch {
			list, err := PopularSkills(ctx)
			if err == nil && len(list) > 0 {
				WriteCache(cacheDir, "skills-popular", list)
				index = list
			} else {
				if err != nil {
					warnings = append(warnings, fmt.Sprintf("skills.sh: %v", err))
				} else {
					warnings = append(warnings, "skills.sh: its page has no list of skills")
				}
				if cachedFile != nil {
					index = cachedFile.Data
					fromCache = true
				}
			}
		} else if cachedFile != nil {
			index = cachedFile.Data
			fromCache = true
		}

		var skills []MarketSkill
		if strings.TrimSpace(query) == "" {
			skills = PopularView(index)
		} else {
			remote, err := SearchSkills(ctx, query)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("skills.sh: %v", err))
				fromCache = true
				skills = SearchLocalSkills(index, query, 50)
			} else {
				skills = MergeSkills(remote, SearchLocalSkills(index, query, 50))
			}
		}
		if skills == nil {
			skills = []MarketSkill{}
		}
		return DiscoveryDTO{
			Servers:  []MarketServerDTO{},
			Skills:   skills,
			Warnings: warnings,
			Cached:   fromCache,
		}, nil
	default:
		return nil, fmt.Errorf("unknown kind %q", kind)
	}
}

func (s *Service) rememberMarketServers(list []MarketServer) {
	s.marketMu.Lock()
	defer s.marketMu.Unlock()
	for _, m := range list {
		filtered := s.marketCache[:0]
		for _, cur := range s.marketCache {
			if cur.ID != m.ID {
				filtered = append(filtered, cur)
			}
		}
		s.marketCache = append(filtered, m)
	}
	const capLimit = 300
	if len(s.marketCache) > capLimit {
		s.marketCache = append([]MarketServer(nil), s.marketCache[len(s.marketCache)-capLimit:]...)
	}
}

func (s *Service) lookupMarketServer(id string) (MarketServer, bool) {
	s.marketMu.Lock()
	for _, m := range s.marketCache {
		if m.ID == id {
			s.marketMu.Unlock()
			return m, true
		}
	}
	s.marketMu.Unlock()
	for _, m := range Featured() {
		if m.ID == id {
			return m, true
		}
	}
	return MarketServer{}, false
}

func (s *Service) hInstallServer(params map[string]json.RawMessage) (any, error) {
	id, err := paramStr(params, "id")
	if err != nil {
		return nil, err
	}
	valuesMap := map[string]string{}
	if rawVals, ok := params["values"]; ok && len(rawVals) > 0 && string(rawVals) != "null" {
		var m map[string]any
		if err := json.Unmarshal(rawVals, &m); err == nil {
			for k, v := range m {
				if str, ok := v.(string); ok {
					valuesMap[k] = str
				}
			}
		}
	}
	agents, err := paramStringSlice(params, "agents")
	if err != nil {
		return nil, err
	}
	rename := optParamStr(params, "name")

	marketSrv, ok := s.lookupMarketServer(id)
	if !ok {
		return nil, fmt.Errorf("%s isn't a known market server; discover it first", id)
	}
	for _, input := range marketSrv.Inputs {
		if input.Required && strings.TrimSpace(valuesMap[input.Key]) == "" {
			return nil, fmt.Errorf("%s is required", input.Label)
		}
	}

	resolved, store, err := openResolvedStore()
	if err != nil {
		return nil, err
	}
	ordered := make([]string, len(marketSrv.Inputs))
	for i, input := range marketSrv.Inputs {
		ordered[i] = valuesMap[input.Key]
	}
	server := marketSrv.Fill(ordered, resolved.Env.Home)
	if rename != nil {
		server.Name = *rename
	}
	if err := server.Validate(); err != nil {
		return nil, err
	}
	if store.Server(server.Name) != nil {
		return nil, fmt.Errorf("the library already has a server called %s", server.Name)
	}
	targets, err := ValidateTargets("mcp", agents, store, &server)
	if err != nil {
		return nil, err
	}
	if err := CheckMCPCollision(store, server.Name, targets); err != nil {
		return nil, err
	}
	if err := store.AddServer(server, targets, id); err != nil {
		return nil, err
	}
	report, err := store.Commit()
	if err != nil {
		return nil, err
	}
	return s.buildMutationResult(store, resolved.Env.Home, report, fmt.Sprintf("Installed %s", server.Name), nil), nil
}

func (s *Service) hUpdate(ctx context.Context, params map[string]json.RawMessage) (any, error) {
	kind, err := paramStr(params, "kind")
	if err != nil {
		return nil, err
	}
	name := optParamStr(params, "name")
	resolved, store, err := openResolvedStore()
	if err != nil {
		return nil, err
	}
	var issues []string
	var logText string
	remote := NetRemote{}

	switch {
	case kind == "mcp" && name != nil:
		if err := RequireKnown(store, "mcp", *name); err != nil {
			return nil, err
		}
		line, err := UpdateOneMCP(ctx, store, remote, *name)
		if err != nil {
			return nil, err
		}
		if line.Failed {
			issues = append(issues, line.Line)
		}
		logText = fmt.Sprintf("%s  %s", *name, line.Line)
	case kind == "skills" && name != nil:
		if err := RequireKnown(store, "skills", *name); err != nil {
			return nil, err
		}
		line, err := UpdateOneSkill(ctx, store, remote, *name)
		if err != nil {
			return nil, err
		}
		if line.Failed {
			issues = append(issues, line.Line)
		}
		logText = fmt.Sprintf("%s  %s", *name, line.Line)
	case kind == "mcp" && name == nil:
		var buf bytes.Buffer
		failed, err := UpdateAllMCP(ctx, store, remote, &buf)
		if err != nil {
			return nil, err
		}
		if failed > 0 {
			issues = append(issues, fmt.Sprintf("%d MCP server(s) failed to update", failed))
		}
		logText = buf.String()
	case kind == "skills" && name == nil:
		var buf bytes.Buffer
		failed, err := UpdateAllSkills(ctx, store, remote, &buf)
		if err != nil {
			return nil, err
		}
		if failed > 0 {
			issues = append(issues, fmt.Sprintf("%d skill(s) failed to update", failed))
		}
		logText = buf.String()
	default:
		return nil, fmt.Errorf("unknown kind %q", kind)
	}

	report := Report{Changed: 0, Issues: issues}
	return s.buildMutationResult(store, resolved.Env.Home, report, "Updated", &logText), nil
}

func (s *Service) hClear(params map[string]json.RawMessage) (any, error) {
	kind, err := paramStr(params, "kind")
	if err != nil {
		return nil, err
	}
	resolved, store, err := openResolvedStore()
	if err != nil {
		return nil, err
	}
	var names []string
	switch kind {
	case "mcp":
		for _, e := range store.Lib.MCP {
			names = append(names, e.Server.Name)
		}
		for _, n := range names {
			if err := store.RemoveServer(n); err != nil {
				return nil, err
			}
		}
	case "skills":
		for _, e := range store.Lib.Skills {
			names = append(names, e.Name)
		}
		for _, n := range names {
			if err := store.RemoveSkill(n); err != nil {
				return nil, err
			}
		}
	default:
		return nil, fmt.Errorf("unknown kind %q", kind)
	}
	report, err := store.Commit()
	if err != nil {
		return nil, err
	}
	if kind == "skills" {
		pruneIfSafe(store, report)
	}
	var msg string
	switch len(names) {
	case 0:
		msg = fmt.Sprintf("No %s entries to clear", kind)
	case 1:
		msg = fmt.Sprintf("Cleared 1 %s entry", kind)
	default:
		msg = fmt.Sprintf("Cleared %d %s entries", len(names), kind)
	}
	return s.buildMutationResult(store, resolved.Env.Home, report, msg, nil), nil
}
