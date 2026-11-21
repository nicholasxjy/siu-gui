package core

import (
	"fmt"
	"path/filepath"
	"sort"
)

// MCPStates computes the actual per-agent state ("on", "off", "paused", "error",
// or "unsupported:<reason>") for an MCP entry by checking both applied records
// and each agent's parsed MCP configuration on disk.
func MCPStates(store *Store, e *McpEntry) map[string]string {
	out := make(map[string]string, len(store.Agents))
	for _, agent := range store.Agents {
		ds := store.ServerState(e, agent)
		var state string
		switch ds.Kind {
		case StateOff:
			if mcpRemains(store, e, agent) {
				state = "error"
			} else {
				state = "off"
			}
		case StatePaused:
			if mcpRemains(store, e, agent) {
				state = "error"
			} else {
				state = "paused"
			}
		case StateUnsupported:
			state = "unsupported:" + ds.Reason
		case StateOn:
			if !agent.Detected() {
				state = "unsupported:not detected"
			} else {
				applied := store.Lib.Applied[agent.ID].MCP.Has(e.Server.Name)
				var actual *Server
				if agent.MCP != nil {
					if m, err := ReadMCP(agent.MCP); err == nil {
						if s, ok := m[e.Server.Name]; ok {
							actual = &s
						}
					}
				}
				if applied && actual != nil && agent.MCP != nil && sameEncodedServer(actual, &e.Server, agent.MCP.Format) {
					state = "on"
				} else {
					state = "error"
				}
			}
		}
		out[agent.ID] = state
	}
	return out
}

func sameEncodedServer(a, b *Server, format McpFormat) bool {
	return orderedEqual(EncodeMCP(format, a), EncodeMCP(format, b))
}

func mcpRemains(store *Store, entry *McpEntry, agent Agent) bool {
	if !store.Lib.Applied[agent.ID].MCP.Has(entry.Server.Name) {
		return false
	}
	if agent.MCP == nil {
		return true
	}
	entries, err := ReadMCP(agent.MCP)
	if err != nil {
		return true
	}
	_, exists := entries[entry.Server.Name]
	return exists
}

func skillRemains(store *Store, entry *SkillEntry, agent Agent) bool {
	if !store.Lib.Applied[agent.ID].Skills.Has(entry.Name) {
		return false
	}
	if agent.Skills == "" {
		return true
	}
	return LinksTo(filepath.Join(agent.Skills, entry.Name), store.SkillDir(entry.Name))
}

// SkillStates computes the actual per-agent state for a Skill entry by checking
// both applied records and real symlinks/junctions on disk.
func SkillStates(store *Store, e *SkillEntry) map[string]string {
	out := make(map[string]string, len(store.Agents))
	for _, agent := range store.Agents {
		ds := store.SkillState(e, agent)
		var state string
		switch ds.Kind {
		case StateOff:
			if skillRemains(store, e, agent) {
				state = "error"
			} else {
				state = "off"
			}
		case StatePaused:
			if skillRemains(store, e, agent) {
				state = "error"
			} else {
				state = "paused"
			}
		case StateUnsupported:
			state = "unsupported:" + ds.Reason
		case StateOn:
			if !agent.Detected() {
				state = "unsupported:not detected"
			} else {
				applied := store.Lib.Applied[agent.ID].Skills.Has(e.Name)
				linked := agent.Skills != "" && LinksTo(filepath.Join(agent.Skills, e.Name), store.SkillDir(e.Name))
				if applied && linked {
					state = "on"
				} else {
					state = "error"
				}
			}
		}
		out[agent.ID] = state
	}
	return out
}

// CollectStateIssues appends readable issue messages for every agent whose
// computed state is "error".
func CollectStateIssues(kind, name string, states map[string]string, agents []Agent, out *[]string) {
	ids := make([]string, 0, len(states))
	for id := range states {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if states[id] != "error" {
			continue
		}
		agentName := id
		for _, a := range agents {
			if a.ID == id {
				agentName = a.Name
				break
			}
		}
		*out = append(*out, fmt.Sprintf("%s %s: not applied in %s", kind, name, agentName))
	}
}

// CollectOrphanIssues reports applied entries whose library entry was removed
// but whose agent config/link still remains on disk.
func CollectOrphanIssues(store *Store, out *[]string) {
	agentIDs := make([]string, 0, len(store.Lib.Applied))
	for id := range store.Lib.Applied {
		agentIDs = append(agentIDs, id)
	}
	sort.Strings(agentIDs)

	for _, id := range agentIDs {
		applied := store.Lib.Applied[id]
		agent := store.Agent(id)
		label := id
		if agent != nil {
			label = agent.Name
		}
		for _, name := range applied.MCP.Sorted() {
			if store.Server(name) != nil {
				continue
			}
			remains := true
			if agent != nil && agent.MCP != nil {
				if entries, err := ReadMCP(agent.MCP); err == nil {
					_, remains = entries[name]
				}
			}
			if remains {
				*out = append(*out, fmt.Sprintf("MCP server %s: pending removal from %s", name, label))
			}
		}
		for _, name := range applied.Skills.Sorted() {
			if store.Skill(name) != nil {
				continue
			}
			remains := true
			if agent != nil && agent.Skills != "" {
				remains = LinksTo(filepath.Join(agent.Skills, name), store.SkillDir(name))
			}
			if remains {
				*out = append(*out, fmt.Sprintf("Skill %s: pending removal from %s", name, label))
			}
		}
	}
}
