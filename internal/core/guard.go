package core

import "fmt"

// ValidateTargets checks that every requested agent ID exists, supports kind
// (and server if kind == "mcp"), and is detected on disk (except shared "agents"
// for skills, which may be selected before ~/.agents exists).
func ValidateTargets(kind string, ids []string, store *Store, server *Server) (StringSet, error) {
	out := StringSet{}
	for _, id := range ids {
		agent := store.Agent(id)
		if agent == nil {
			return nil, fmt.Errorf("unknown agent %q", id)
		}
		switch kind {
		case "mcp":
			if id == "agents" {
				return nil, fmt.Errorf("~/.agents (shared) is not an MCP target")
			}
			if agent.MCP == nil {
				return nil, fmt.Errorf("%s has no MCP config siu can write", agent.Name)
			}
			if server != nil {
				if why := UnsupportedMCP(agent.MCP.Format, server); why != "" {
					return nil, fmt.Errorf("%s: %s", agent.Name, why)
				}
			}
			if !agent.Detected() {
				return nil, fmt.Errorf("%s isn't detected", agent.Name)
			}
		case "skills":
			if agent.Skills == "" {
				return nil, fmt.Errorf("%s has no skills folder", agent.Name)
			}
			if id != "agents" && !agent.Detected() {
				return nil, fmt.Errorf("%s isn't detected", agent.Name)
			}
		default:
			return nil, fmt.Errorf("unknown kind %q", kind)
		}
		out.Add(id)
	}
	return out, nil
}

// CheckMCPCollision refuses an add/rename/retarget that would overwrite an
// unmanaged MCP entry of the same name in any target agent's config file.
func CheckMCPCollision(store *Store, name string, agents StringSet) error {
	for _, id := range agents.Sorted() {
		agent := store.Agent(id)
		if agent == nil || agent.MCP == nil {
			continue
		}
		if store.Lib.Applied[id].MCP.Has(name) {
			continue
		}
		existing, err := ReadMCP(agent.MCP)
		if err != nil {
			continue
		}
		if _, exists := existing[name]; exists {
			return fmt.Errorf("%s already has an MCP server called %s that siu doesn't manage; import it explicitly first", agent.Name, name)
		}
	}
	return nil
}
