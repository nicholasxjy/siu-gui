package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

const (
	mcpRegistryURL = "https://registry.modelcontextprotocol.io/v0/servers"
	skillsShURL    = "https://www.skills.sh"
)

// InputPlace specifies where a user-supplied input goes on a MarketServer.
type InputPlace string

const (
	PlaceEnv    InputPlace = "env"
	PlaceHeader InputPlace = "header"
	PlaceArg    InputPlace = "arg"
)

func (p *InputPlace) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	switch strings.ToLower(s) {
	case "env":
		*p = PlaceEnv
	case "header":
		*p = PlaceHeader
	case "arg":
		*p = PlaceArg
	default:
		return fmt.Errorf("unknown input place %q", s)
	}
	return nil
}

// MarketInput describes a required or optional value a server needs before starting.
type MarketInput struct {
	Key      string     `json:"key"`
	Place    InputPlace `json:"place"`
	Label    string     `json:"label"`
	Hint     string     `json:"hint"`
	Secret   bool       `json:"secret"`
	Required bool       `json:"required"`
	Format   string     `json:"format"`
}

// MarketServer is an MCP server offered by the featured list or official MCP Registry.
type MarketServer struct {
	ID          string        `json:"id"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	Publisher   string        `json:"publisher"`
	Homepage    string        `json:"homepage"`
	Server      Server        `json:"server"`
	Inputs      []MarketInput `json:"inputs"`
	Featured    bool          `json:"featured"`
}

// Fill returns the server with user-provided values applied in input order.
func (m MarketServer) Fill(values []string, home string) Server {
	s := m.Server.Clone()
	for i := 0; i < len(m.Inputs) && i < len(values); i++ {
		input := m.Inputs[i]
		v := strings.TrimSpace(values[i])
		if v == "" {
			continue
		}
		switch input.Place {
		case PlaceEnv:
			if s.Env == nil {
				s.Env = map[string]string{}
			}
			s.Env[input.Key] = v
		case PlaceHeader:
			if s.Headers == nil {
				s.Headers = map[string]string{}
			}
			s.Headers[input.Key] = strings.ReplaceAll(input.Format, "{}", v)
		case PlaceArg:
			if rest, ok := strings.CutPrefix(v, "~/"); ok {
				v = filepath.Join(home, rest)
			}
			s.Args = append(s.Args, v)
		}
	}
	return s
}

func secretEnvInput(key, hint string) MarketInput {
	return MarketInput{
		Key:      key,
		Place:    PlaceEnv,
		Label:    key,
		Hint:     hint,
		Secret:   true,
		Required: true,
		Format:   "{}",
	}
}

func featuredRemote(id, title, publisher, rawURL, desc string, inputs []MarketInput) MarketServer {
	if inputs == nil {
		inputs = []MarketInput{}
	}
	return MarketServer{
		ID:          id,
		Title:       title,
		Description: desc,
		Publisher:   publisher,
		Homepage:    rawURL,
		Server: Server{
			Name:      id,
			Transport: TransportHttp,
			URL:       rawURL,
		},
		Inputs:   inputs,
		Featured: true,
	}
}

func featuredLocal(id, title, publisher, home, desc, cmd string, args []string, inputs []MarketInput) MarketServer {
	if inputs == nil {
		inputs = []MarketInput{}
	}
	return MarketServer{
		ID:          id,
		Title:       title,
		Description: desc,
		Publisher:   publisher,
		Homepage:    home,
		Server: Server{
			Name:      id,
			Transport: TransportStdio,
			Command:   cmd,
			Args:      append([]string(nil), args...),
		},
		Inputs:   inputs,
		Featured: true,
	}
}

// Featured returns the curated list of well-known MCP servers.
func Featured() []MarketServer {
	const mcp = "https://github.com/modelcontextprotocol/servers/tree/main/src"
	return []MarketServer{
		featuredRemote(
			"context7",
			"Context7",
			"Upstash",
			"https://mcp.context7.com/mcp",
			"Up-to-date documentation and code examples for any library, straight into the prompt.",
			[]MarketInput{{
				Key:      "CONTEXT7_API_KEY",
				Place:    PlaceHeader,
				Label:    "API key",
				Hint:     "Optional — raises the rate limit. From context7.com/dashboard",
				Secret:   true,
				Required: false,
				Format:   "{}",
			}},
		),
		featuredLocal(
			"playwright",
			"Playwright",
			"Microsoft",
			"https://github.com/microsoft/playwright-mcp",
			"Drive a real browser: open pages, click, type and read them through the accessibility tree.",
			"npx",
			[]string{"-y", "@playwright/mcp@latest"},
			nil,
		),
		featuredLocal(
			"chrome-devtools",
			"Chrome DevTools",
			"Google",
			"https://github.com/ChromeDevTools/chrome-devtools-mcp",
			"Control and inspect a live Chrome: console, network, performance traces and screenshots.",
			"npx",
			[]string{"-y", "chrome-devtools-mcp@latest"},
			nil,
		),
		featuredRemote(
			"github",
			"GitHub",
			"GitHub",
			"https://api.githubcopilot.com/mcp/",
			"Repositories, issues, pull requests, Actions and code search on GitHub.",
			[]MarketInput{{
				Key:      "Authorization",
				Place:    PlaceHeader,
				Label:    "Personal access token",
				Hint:     "github.com/settings/personal-access-tokens",
				Secret:   true,
				Required: true,
				Format:   "Bearer {}",
			}},
		),
		featuredRemote(
			"deepwiki",
			"DeepWiki",
			"Cognition",
			"https://mcp.deepwiki.com/mcp",
			"Ask questions about any public GitHub repository and read its generated wiki.",
			nil,
		),
		featuredRemote(
			"exa",
			"Exa",
			"Exa",
			"https://mcp.exa.ai/mcp",
			"Web search and code search built for agents, with clean page contents.",
			nil,
		),
		featuredLocal(
			"filesystem",
			"Filesystem",
			"Model Context Protocol",
			mcp+"/filesystem",
			"Read, write and search files, limited to the folders you allow.",
			"npx",
			[]string{"-y", "@modelcontextprotocol/server-filesystem"},
			[]MarketInput{{
				Key:      "dir",
				Place:    PlaceArg,
				Label:    "Folder",
				Hint:     "The folder it may read and write, e.g. ~/code",
				Secret:   false,
				Required: true,
				Format:   "{}",
			}},
		),
		featuredLocal(
			"fetch",
			"Fetch",
			"Model Context Protocol",
			mcp+"/fetch",
			"Fetch a web page and hand it over as Markdown.",
			"uvx",
			[]string{"mcp-server-fetch"},
			nil,
		),
		featuredLocal(
			"memory",
			"Memory",
			"Model Context Protocol",
			mcp+"/memory",
			"A knowledge graph the agent keeps across conversations.",
			"npx",
			[]string{"-y", "@modelcontextprotocol/server-memory"},
			nil,
		),
		featuredLocal(
			"sequential-thinking",
			"Sequential Thinking",
			"Model Context Protocol",
			mcp+"/sequentialthinking",
			"Think a problem through step by step, revising and branching as it goes.",
			"npx",
			[]string{"-y", "@modelcontextprotocol/server-sequential-thinking"},
			nil,
		),
		featuredLocal(
			"git",
			"Git",
			"Model Context Protocol",
			mcp+"/git",
			"Read, search and change Git repositories.",
			"uvx",
			[]string{"mcp-server-git"},
			nil,
		),
		featuredLocal(
			"time",
			"Time",
			"Model Context Protocol",
			mcp+"/time",
			"The current time anywhere, and conversions between time zones.",
			"uvx",
			[]string{"mcp-server-time"},
			nil,
		),
		featuredRemote(
			"sentry",
			"Sentry",
			"Sentry",
			"https://mcp.sentry.dev/mcp",
			"Issues, errors and traces from Sentry. Signs in through the browser on first use.",
			nil,
		),
		featuredRemote(
			"linear",
			"Linear",
			"Linear",
			"https://mcp.linear.app/mcp",
			"Find, create and update Linear issues, projects and comments. Signs in on first use.",
			nil,
		),
		featuredRemote(
			"notion",
			"Notion",
			"Notion",
			"https://mcp.notion.com/mcp",
			"Search, read and write pages and databases in your Notion workspace. Signs in on first use.",
			nil,
		),
		featuredRemote(
			"atlassian",
			"Atlassian",
			"Atlassian",
			"https://mcp.atlassian.com/v1/mcp",
			"Jira issues and Confluence pages from Atlassian Cloud. Signs in on first use.",
			nil,
		),
		featuredRemote(
			"supabase",
			"Supabase",
			"Supabase",
			"https://mcp.supabase.com/mcp",
			"Manage Supabase projects: tables, SQL, migrations, logs and edge functions.",
			nil,
		),
		featuredRemote(
			"stripe",
			"Stripe",
			"Stripe",
			"https://mcp.stripe.com",
			"Customers, payments, subscriptions and Stripe's documentation.",
			nil,
		),
		featuredRemote(
			"vercel",
			"Vercel",
			"Vercel",
			"https://mcp.vercel.com",
			"Projects, deployments and logs on Vercel, and its documentation.",
			nil,
		),
		featuredRemote(
			"neon",
			"Neon",
			"Neon",
			"https://mcp.neon.tech/mcp",
			"Serverless Postgres on Neon: projects, branches and SQL.",
			nil,
		),
		featuredRemote(
			"cloudflare-docs",
			"Cloudflare Docs",
			"Cloudflare",
			"https://docs.mcp.cloudflare.com/mcp",
			"Search Cloudflare's documentation.",
			nil,
		),
		featuredRemote(
			"microsoft-learn",
			"Microsoft Learn",
			"Microsoft",
			"https://learn.microsoft.com/api/mcp",
			"Official Microsoft and Azure documentation and code samples.",
			nil,
		),
		featuredRemote(
			"huggingface",
			"Hugging Face",
			"Hugging Face",
			"https://huggingface.co/mcp",
			"Search models, datasets, Spaces and papers on the Hugging Face Hub.",
			nil,
		),
		featuredLocal(
			"brave-search",
			"Brave Search",
			"Brave",
			"https://github.com/brave/brave-search-mcp-server",
			"Web, news, image and local search from Brave's independent index.",
			"npx",
			[]string{"-y", "@brave/brave-search-mcp-server"},
			[]MarketInput{secretEnvInput("BRAVE_API_KEY", "brave.com/search/api")},
		),
		featuredLocal(
			"firecrawl",
			"Firecrawl",
			"Firecrawl",
			"https://github.com/firecrawl/firecrawl-mcp-server",
			"Scrape, crawl and search the web into clean Markdown.",
			"npx",
			[]string{"-y", "firecrawl-mcp"},
			[]MarketInput{secretEnvInput("FIRECRAWL_API_KEY", "firecrawl.dev/app/api-keys")},
		),
		featuredLocal(
			"tavily",
			"Tavily",
			"Tavily",
			"https://github.com/tavily-ai/tavily-mcp",
			"Search and extract from the web with Tavily.",
			"npx",
			[]string{"-y", "tavily-mcp"},
			[]MarketInput{secretEnvInput("TAVILY_API_KEY", "app.tavily.com")},
		),
	}
}

// FeaturedMatching returns featured servers matching query q.
func FeaturedMatching(q string) []MarketServer {
	q = strings.ToLower(strings.TrimSpace(q))
	var out []MarketServer
	for _, m := range Featured() {
		if q == "" {
			out = append(out, m)
			continue
		}
		hay := strings.ToLower(fmt.Sprintf("%s %s %s %s", m.ID, m.Title, m.Publisher, m.Description))
		if strings.Contains(hay, q) {
			out = append(out, m)
		}
	}
	return out
}

// ---- Official MCP Registry ----

// SearchRegistry queries the official MCP Registry for servers matching q.
func SearchRegistry(ctx context.Context, q string) ([]MarketServer, error) {
	rawURL := fmt.Sprintf("%s?search=%s&limit=60&version=latest", mcpRegistryURL, URLEncode(strings.TrimSpace(q)))
	body, err := HTTPGetJSON[map[string]any](ctx, rawURL)
	if err != nil {
		return nil, err
	}
	return ParseRegistry(body, q), nil
}

// ParseRegistry extracts active, deduplicated MarketServer descriptors from a registry response.
func ParseRegistry(body map[string]any, q string) []MarketServer {
	featuredSummaries := map[string]bool{}
	for _, m := range Featured() {
		featuredSummaries[m.Server.Summary()] = true
	}
	seen := map[string]bool{}
	var out []MarketServer
	serversRaw, _ := body["servers"].([]any)
	for _, item := range serversRaw {
		e, ok := item.(map[string]any)
		if !ok {
			continue
		}
		status := "active"
		if meta, ok := e["_meta"].(map[string]any); ok {
			if official, ok := meta["io.modelcontextprotocol.registry/official"].(map[string]any); ok {
				if s, ok := official["status"].(string); ok {
					status = s
				}
			}
		}
		if status != "active" {
			continue
		}
		srvObj, ok := e["server"].(map[string]any)
		if !ok {
			continue
		}
		m, ok := fromRegistry(srvObj)
		if !ok {
			continue
		}
		summary := m.Server.Summary()
		if featuredSummaries[summary] || seen[summary] {
			continue
		}
		seen[summary] = true
		out = append(out, m)
	}

	ql := strings.ToLower(strings.TrimSpace(q))
	rank := func(m MarketServer) int {
		n := strings.ToLower(m.Server.Name)
		if n == ql {
			return 0
		}
		if strings.Contains(n, ql) {
			return 1
		}
		return 2
	}
	sort.SliceStable(out, func(i, j int) bool {
		return rank(out[i]) < rank(out[j])
	})
	return out
}

func hasPlaceholder(s string) bool {
	return strings.Contains(s, "{") && strings.Contains(s, "}")
}

func jsonMapStr(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

func jsonMapBool(m map[string]any, key string) bool {
	v, _ := m[key].(bool)
	return v
}

func fromRegistry(r map[string]any) (MarketServer, bool) {
	id, ok := r["name"].(string)
	if !ok || id == "" {
		return MarketServer{}, false
	}
	var repo string
	if repoObj, ok := r["repository"].(map[string]any); ok {
		repo = jsonMapStr(repoObj, "url")
	}
	homepage := jsonMapStr(r, "websiteUrl")
	if homepage == "" {
		homepage = repo
	}
	var publisher string
	if rest, ok := strings.CutPrefix(repo, "https://github.com/"); ok {
		publisher, _, _ = strings.Cut(rest, "/")
	} else {
		ns, _, _ := strings.Cut(id, "/")
		parts := strings.Split(ns, ".")
		for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
			parts[i], parts[j] = parts[j], parts[i]
		}
		publisher = strings.Join(parts, ".")
	}
	name := ShortName(id)
	title := jsonMapStr(r, "title")
	if title == "" {
		title = name
	}
	m := MarketServer{
		ID:          id,
		Title:       title,
		Description: jsonMapStr(r, "description"),
		Publisher:   publisher,
		Homepage:    homepage,
		Server:      Server{Name: name, Transport: TransportStdio},
		Inputs:      []MarketInput{},
		Featured:    false,
	}

	remotes, _ := r["remotes"].([]any)
	for _, want := range []string{"streamable-http", "sse"} {
		for _, item := range remotes {
			rm, ok := item.(map[string]any)
			if !ok {
				continue
			}
			rawURL := jsonMapStr(rm, "url")
			if jsonMapStr(rm, "type") != want || hasPlaceholder(rawURL) || !strings.HasPrefix(rawURL, "https://") {
				continue
			}
			if want == "sse" {
				m.Server.Transport = TransportSse
			} else {
				m.Server.Transport = TransportHttp
			}
			m.Server.URL = rawURL
			headers, _ := rm["headers"].([]any)
			for _, hItem := range headers {
				h, ok := hItem.(map[string]any)
				if !ok {
					continue
				}
				key := jsonMapStr(h, "name")
				val := jsonMapStr(h, "value")
				required := jsonMapBool(h, "isRequired")
				secret := jsonMapBool(h, "isSecret")
				if val != "" && !hasPlaceholder(val) {
					if m.Server.Headers == nil {
						m.Server.Headers = map[string]string{}
					}
					m.Server.Headers[key] = val
				} else if required || secret {
					format := "{}"
					if val != "" {
						format = replacePlaceholders(val)
					}
					m.Inputs = append(m.Inputs, MarketInput{
						Key:      key,
						Place:    PlaceHeader,
						Label:    key,
						Hint:     jsonMapStr(h, "description"),
						Secret:   secret,
						Required: required,
						Format:   format,
					})
				}
			}
			return m, true
		}
	}

	packages, _ := r["packages"].([]any)
	for _, want := range []string{"npm", "pypi", "oci"} {
		for _, item := range packages {
			p, ok := item.(map[string]any)
			if !ok {
				continue
			}
			ident := jsonMapStr(p, "identifier")
			transport := ""
			if tObj, ok := p["transport"].(map[string]any); ok {
				transport = jsonMapStr(tObj, "type")
			}
			if jsonMapStr(p, "registryType") != want || ident == "" || !(transport == "" || transport == "stdio") {
				continue
			}
			switch want {
			case "npm":
				m.Server.Command = "npx"
				m.Server.Args = []string{"-y", ident}
			case "pypi":
				m.Server.Command = "uvx"
				m.Server.Args = []string{ident}
			default:
				m.Server.Command = "docker"
				m.Server.Args = []string{"run", "-i", "--rm"}
			}
			envVars, _ := p["environmentVariables"].([]any)
			for _, eItem := range envVars {
				ev, ok := eItem.(map[string]any)
				if !ok {
					continue
				}
				required := jsonMapBool(ev, "isRequired")
				secret := jsonMapBool(ev, "isSecret")
				if !required && !secret {
					continue
				}
				key := jsonMapStr(ev, "name")
				if want == "oci" {
					m.Server.Args = append(m.Server.Args, "-e", key)
				}
				m.Inputs = append(m.Inputs, MarketInput{
					Key:      key,
					Place:    PlaceEnv,
					Label:    key,
					Hint:     jsonMapStr(ev, "description"),
					Secret:   secret,
					Required: required,
					Format:   "{}",
				})
			}
			if want == "oci" {
				m.Server.Args = append(m.Server.Args, ident)
			}
			pkgArgs, _ := p["packageArguments"].([]any)
			for i, aItem := range pkgArgs {
				a, ok := aItem.(map[string]any)
				if !ok {
					continue
				}
				v := jsonMapStr(a, "value")
				if v == "" {
					v = jsonMapStr(a, "default")
				}
				if hasPlaceholder(v) {
					v = ""
				}
				named := jsonMapStr(a, "type") == "named"
				required := jsonMapBool(a, "isRequired")
				switch {
				case v != "" && named:
					m.Server.Args = append(m.Server.Args, jsonMapStr(a, "name"), v)
				case v != "":
					m.Server.Args = append(m.Server.Args, v)
				case required && !named:
					label := jsonMapStr(a, "valueHint")
					if label == "" {
						label = fmt.Sprintf("Argument %d", i+1)
					}
					m.Inputs = append(m.Inputs, MarketInput{
						Key:      fmt.Sprintf("arg%d", i),
						Place:    PlaceArg,
						Label:    label,
						Hint:     jsonMapStr(a, "description"),
						Secret:   false,
						Required: true,
						Format:   "{}",
					})
				case required:
					return MarketServer{}, false
				}
			}
			return m, true
		}
	}
	return MarketServer{}, false
}

func replacePlaceholders(s string) string {
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch {
		case r == '{':
			if depth == 0 {
				b.WriteString("{}")
			}
			depth++
		case r == '}' && depth > 0:
			depth--
		case depth == 0:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ShortName derives a library-friendly server name from a registry identifier.
func ShortName(n string) string {
	orig := strings.ToLower(n)
	if idx := strings.LastIndexByte(orig, '/'); idx >= 0 {
		orig = orig[idx+1:]
	}
	s := orig
	for _, p := range []string{"mcp-server-", "server-", "mcp-"} {
		if rest, ok := strings.CutPrefix(s, p); ok {
			s = rest
		}
	}
	for _, suf := range []string{"-mcp-server", "-mcp", "-server", "_mcp", ".mcp"} {
		if rest, ok := strings.CutSuffix(s, suf); ok {
			s = rest
		}
	}
	if s == "" {
		s = orig
	}
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	clean := strings.Trim(b.String(), "-_")
	if len(clean) > 64 {
		clean = clean[:64]
	}
	if clean == "" {
		return "server"
	}
	return clean
}

// ---- skills.sh ----

// MarketSkill describes a skill listed on skills.sh.
type MarketSkill struct {
	Source   string `json:"source"`
	SkillID  string `json:"skillId"`
	Name     string `json:"name"`
	Installs uint64 `json:"installs"`
	Official bool   `json:"isOfficial"`
}

func (s MarketSkill) Key() string {
	return s.Source + "/" + s.SkillID
}

func isValidRepoSpec(s string) bool {
	parts := strings.Split(s, "/")
	if len(parts) != 2 {
		return false
	}
	for _, p := range parts {
		if !isRepoSegment(p) {
			return false
		}
	}
	return true
}

// SearchSkills queries skills.sh's search API.
func SearchSkills(ctx context.Context, q string) ([]MarketSkill, error) {
	type body struct {
		Skills []MarketSkill `json:"skills"`
	}
	resp, err := HTTPGetJSON[body](ctx, fmt.Sprintf("%s/api/search?q=%s&limit=50", skillsShURL, URLEncode(strings.TrimSpace(q))))
	if err != nil {
		return nil, err
	}
	out := make([]MarketSkill, 0, len(resp.Skills))
	for _, s := range resp.Skills {
		if isValidRepoSpec(s.Source) && s.SkillID != "" {
			out = append(out, s)
		}
	}
	return out, nil
}

// PopularSkills scrapes the initialSkills list off skills.sh's front page.
func PopularSkills(ctx context.Context) ([]MarketSkill, error) {
	raw, err := HTTPGet(ctx, skillsShURL+"/", "text/html", 8<<20)
	if err != nil {
		return nil, err
	}
	return ParsePopularSkills(string(raw))
}

// ParsePopularSkills parses the escaped initialSkills JSON array from skills.sh HTML.
func ParsePopularSkills(page string) ([]MarketSkill, error) {
	const key = `initialSkills\":`
	idx := strings.Index(page, key)
	if idx < 0 {
		return nil, errors.New("skills.sh's page has no list of skills")
	}
	rest := page[idx+len(key):]
	rest = strings.ReplaceAll(rest, `\\`, `\`)
	rest = strings.ReplaceAll(rest, `\"`, `"`)
	dec := json.NewDecoder(strings.NewReader(rest))
	var list []MarketSkill
	if err := dec.Decode(&list); err != nil {
		return nil, err
	}
	out := make([]MarketSkill, 0, len(list))
	for _, s := range list {
		if isValidRepoSpec(s.Source) && s.SkillID != "" {
			out = append(out, s)
		}
	}
	return out, nil
}

// PopularView caps each repository to 4 entries and returns up to 60 skills.
func PopularView(index []MarketSkill) []MarketSkill {
	per := map[string]int{}
	out := make([]MarketSkill, 0, 60)
	for _, s := range index {
		per[s.Source]++
		if per[s.Source] <= 4 {
			out = append(out, s)
			if len(out) == 60 {
				break
			}
		}
	}
	return out
}

// SearchLocalSkills matches every query word against name/skillId/source,
// ordered by installs descending.
func SearchLocalSkills(list []MarketSkill, q string, limit int) []MarketSkill {
	words := strings.Fields(strings.ToLower(q))
	if len(words) == 0 {
		return []MarketSkill{}
	}
	var hits []MarketSkill
	for _, s := range list {
		hay := strings.ToLower(fmt.Sprintf("%s %s %s", s.Name, s.SkillID, s.Source))
		ok := true
		for _, w := range words {
			if !strings.Contains(hay, w) {
				ok = false
				break
			}
		}
		if ok {
			hits = append(hits, s)
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		return hits[i].Installs > hits[j].Installs
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

// MergeSkills puts remote results first (inheriting official flags from local)
// and appends unseen local matches.
func MergeSkills(remote []MarketSkill, local []MarketSkill) []MarketSkill {
	keys := make(map[string]bool, len(remote))
	official := map[string]bool{}
	for _, s := range local {
		if s.Official {
			official[s.Key()] = true
		}
	}
	out := make([]MarketSkill, 0, len(remote)+len(local))
	for _, s := range remote {
		k := s.Key()
		keys[k] = true
		if official[k] {
			s.Official = true
		}
		out = append(out, s)
	}
	for _, s := range local {
		if !keys[s.Key()] {
			out = append(out, s)
		}
	}
	return out
}

// MetaDescription extracts the HTML meta description from a skills.sh skill page.
func MetaDescription(page string) string {
	const marker = `<meta name="description" content="`
	i := strings.Index(page, marker)
	if i < 0 {
		return ""
	}
	rest := page[i+len(marker):]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		return ""
	}
	d := rest[:end]
	d = strings.ReplaceAll(d, "&amp;", "&")
	d = strings.ReplaceAll(d, "&quot;", "\"")
	d = strings.ReplaceAll(d, "&#x27;", "'")
	d = strings.ReplaceAll(d, "&#39;", "'")
	d = strings.ReplaceAll(d, "&lt;", "<")
	d = strings.ReplaceAll(d, "&gt;", ">")
	return strings.TrimSpace(d)
}
