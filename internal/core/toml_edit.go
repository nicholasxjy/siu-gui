package core

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

func parseTOMLMap(text string) (map[string]any, error) {
	var doc map[string]any
	if _, err := toml.Decode(text, &doc); err != nil {
		return nil, err
	}
	if doc == nil {
		doc = map[string]any{}
	}
	return doc, nil
}

func tomlValueToOrdered(v any) any {
	switch tv := v.(type) {
	case map[string]any:
		obj := NewOrderedObject()
		keys := make([]string, 0, len(tv))
		for k := range tv {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			obj.Set(k, tomlValueToOrdered(tv[k]))
		}
		return obj
	case []any:
		arr := make([]any, len(tv))
		for i, item := range tv {
			arr[i] = tomlValueToOrdered(item)
		}
		return arr
	case []map[string]any:
		arr := make([]any, len(tv))
		for i, item := range tv {
			arr[i] = tomlValueToOrdered(item)
		}
		return arr
	case time.Time:
		return tv.Format(time.RFC3339Nano)
	default:
		return tv
	}
}

type tomlSection struct {
	header []string // parsed table header path, nil for preamble
	isArr  bool     // [[...]]
	lines  []string // raw lines without trailing \n
}

// splitTOMLSections splits TOML text into top-level sections while tracking
// multiline strings so bracket lines inside strings are never misread as headers.
func splitTOMLSections(text string) []tomlSection {
	if text == "" {
		return nil
	}
	rawLines := strings.Split(text, "\n")
	// Preserve whether original text ended with a newline by dropping the empty
	// sentinel element produced by strings.Split when text ends with '\n'.
	if len(rawLines) > 0 && rawLines[len(rawLines)-1] == "" {
		rawLines = rawLines[:len(rawLines)-1]
	}

	sections := []tomlSection{{}}
	inMultiline := "" // `"""` or `'''`

	for _, line := range rawLines {
		if inMultiline == "" {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "[") {
				if path, isArr, ok := parseTOMLHeader(trimmed); ok {
					sections = append(sections, tomlSection{
						header: path,
						isArr:  isArr,
						lines:  []string{line},
					})
					continue
				}
			}
		}
		sections[len(sections)-1].lines = append(sections[len(sections)-1].lines, line)
		inMultiline = advanceMultilineState(line, inMultiline)
	}
	return sections
}

func advanceMultilineState(line, delim string) string {
	i := 0
	for i < len(line) {
		if delim != "" {
			if delim == `"""` && line[i] == '\\' {
				i += 2
				continue
			}
			if strings.HasPrefix(line[i:], delim) {
				delim = ""
				i += 3
				continue
			}
			i++
			continue
		}
		c := line[i]
		if c == '#' {
			break
		}
		if strings.HasPrefix(line[i:], `"""`) {
			delim = `"""`
			i += 3
			continue
		}
		if strings.HasPrefix(line[i:], `'''`) {
			delim = `'''`
			i += 3
			continue
		}
		if c == '"' {
			i++
			for i < len(line) {
				if line[i] == '\\' {
					i += 2
					continue
				}
				if line[i] == '"' {
					i++
					break
				}
				i++
			}
			continue
		}
		if c == '\'' {
			i++
			for i < len(line) {
				if line[i] == '\'' {
					i++
					break
				}
				i++
			}
			continue
		}
		i++
	}
	return delim
}

func parseTOMLHeader(trimmed string) ([]string, bool, bool) {
	isArr := false
	inner := ""
	if strings.HasPrefix(trimmed, "[[") {
		isArr = true
		rest := trimmed[2:]
		end := findHeaderEnd(rest, "]]")
		if end < 0 {
			return nil, false, false
		}
		after := strings.TrimSpace(rest[end+2:])
		if after != "" && !strings.HasPrefix(after, "#") {
			return nil, false, false
		}
		inner = rest[:end]
	} else if strings.HasPrefix(trimmed, "[") {
		rest := trimmed[1:]
		end := findHeaderEnd(rest, "]")
		if end < 0 {
			return nil, false, false
		}
		after := strings.TrimSpace(rest[end+1:])
		if after != "" && !strings.HasPrefix(after, "#") {
			return nil, false, false
		}
		inner = rest[:end]
	} else {
		return nil, false, false
	}
	parts, ok := splitTOMLKeyPath(inner)
	if !ok || len(parts) == 0 {
		return nil, false, false
	}
	return parts, isArr, true
}

func findHeaderEnd(s, closing string) int {
	inBasic := false
	inLit := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inBasic {
			if c == '\\' {
				i++
				continue
			}
			if c == '"' {
				inBasic = false
			}
			continue
		}
		if inLit {
			if c == '\'' {
				inLit = false
			}
			continue
		}
		if c == '"' {
			inBasic = true
		} else if c == '\'' {
			inLit = true
		} else if strings.HasPrefix(s[i:], closing) {
			return i
		}
	}
	return -1
}

func splitTOMLKeyPath(s string) ([]string, bool) {
	var parts []string
	i := 0
	for {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		if i >= len(s) {
			return nil, false
		}
		if s[i] == '"' {
			j := i + 1
			for j < len(s) {
				if s[j] == '\\' {
					j += 2
					continue
				}
				if s[j] == '"' {
					break
				}
				j++
			}
			if j >= len(s) {
				return nil, false
			}
			unq, err := strconv.Unquote(s[i : j+1])
			if err != nil {
				return nil, false
			}
			parts = append(parts, unq)
			i = j + 1
		} else if s[i] == '\'' {
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				return nil, false
			}
			parts = append(parts, s[i+1:i+1+j])
			i = i + 1 + j + 1
		} else {
			start := i
			for i < len(s) && s[i] != '.' && s[i] != ' ' && s[i] != '\t' {
				i++
			}
			if i == start {
				return nil, false
			}
			parts = append(parts, s[start:i])
		}
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		if i == len(s) {
			return parts, true
		}
		if s[i] != '.' {
			return nil, false
		}
		i++
	}
}

func parseTOMLAssignmentKey(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "[") {
		return "", false
	}
	// Find '=' outside quotes
	inBasic := false
	inLit := false
	eq := -1
	for i := 0; i < len(trimmed); i++ {
		c := trimmed[i]
		if inBasic {
			if c == '\\' {
				i++
				continue
			}
			if c == '"' {
				inBasic = false
			}
			continue
		}
		if inLit {
			if c == '\'' {
				inLit = false
			}
			continue
		}
		if c == '"' {
			inBasic = true
		} else if c == '\'' {
			inLit = true
		} else if c == '=' {
			eq = i
			break
		}
	}
	if eq <= 0 {
		return "", false
	}
	parts, ok := splitTOMLKeyPath(trimmed[:eq])
	if !ok || len(parts) == 0 {
		return "", false
	}
	return parts[0], true
}

func formatTOMLKey(k string) string {
	if isTOMLBareKey(k) {
		return k
	}
	return strconv.Quote(k)
}

func isTOMLBareKey(k string) bool {
	if k == "" {
		return false
	}
	for i := 0; i < len(k); i++ {
		c := k[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func formatTOMLValue(v any) string {
	switch tv := v.(type) {
	case string:
		return strconv.Quote(tv)
	case bool:
		return strconv.FormatBool(tv)
	case int64:
		return strconv.FormatInt(tv, 10)
	case float64:
		return strconv.FormatFloat(tv, 'g', -1, 64)
	case []string:
		parts := make([]string, len(tv))
		for i, s := range tv {
			parts[i] = strconv.Quote(s)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case []any:
		parts := make([]string, len(tv))
		for i, item := range tv {
			parts[i] = formatTOMLValue(item)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case *OrderedObject:
		parts := make([]string, 0, len(tv.Entries))
		for _, kv := range tv.Entries {
			parts = append(parts, fmt.Sprintf("%s = %s", formatTOMLKey(kv.Key), formatTOMLValue(kv.Value)))
		}
		return "{ " + strings.Join(parts, ", ") + " }"
	default:
		return strconv.Quote(fmt.Sprint(tv))
	}
}

func isManagedKey(k string) bool {
	for _, m := range managedMCPKeys {
		if m == k {
			return true
		}
	}
	return false
}

// writeTOML updates [mcp_servers.<name>] tables while preserving comments,
// unrelated tables, and unmanaged keys inside updated server tables.
func writeTOML(format McpFormat, text string, put []*Server, remove []string) (string, bool, error) {
	doc, err := parseTOMLMap(text)
	if err != nil {
		return "", false, err
	}
	var existingServers map[string]any
	if raw, ok := doc["mcp_servers"]; ok {
		m, ok := raw.(map[string]any)
		if !ok {
			return "", false, fmt.Errorf("mcp_servers isn't a table")
		}
		existingServers = m
	} else if len(put) == 0 {
		return "", false, nil
	}

	removeSet := map[string]bool{}
	for _, n := range remove {
		removeSet[n] = true
	}

	putChanges := map[string]*OrderedObject{}
	putOrder := []string{}
	for _, s := range put {
		var oldVal any
		if existingServers != nil {
			if raw, ok := existingServers[s.Name]; ok {
				oldVal = tomlValueToOrdered(raw)
			}
		}
		want := mergedMCP(format, oldVal, s)
		if oldVal != nil && orderedEqual(oldVal, want) {
			continue
		}
		putChanges[s.Name] = want
		putOrder = append(putOrder, s.Name)
	}

	if len(putChanges) == 0 {
		hasRemoval := false
		for n := range removeSet {
			if existingServers != nil {
				if _, ok := existingServers[n]; ok {
					hasRemoval = true
					break
				}
			}
		}
		if !hasRemoval {
			return "", false, nil
		}
	}

	sections := splitTOMLSections(text)
	handledPut := map[string]bool{}
	var outSections []tomlSection

	for _, sec := range sections {
		if !sec.isArr && len(sec.header) >= 2 && sec.header[0] == "mcp_servers" {
			name := sec.header[1]
			if removeSet[name] {
				continue
			}
			if want, changing := putChanges[name]; changing {
				if len(sec.header) > 2 {
					// Drop managed sub-tables like [mcp_servers.<name>.env] or
					// [mcp_servers.<name>.headers] / [mcp_servers.<name>.http_headers]
					// because we write them as inline tables under [mcp_servers.<name>].
					if isManagedKey(sec.header[2]) {
						continue
					}
					outSections = append(outSections, sec)
					continue
				}
				handledPut[name] = true
				outSections = append(outSections, rewriteTOMLServerSection(sec, want))
				continue
			}
		}
		// If there is a bare [mcp_servers] section with inline keys matching
		// removed/updated server names, strip those lines from it.
		if !sec.isArr && len(sec.header) == 1 && sec.header[0] == "mcp_servers" {
			filtered := filterTOMLSectionKeys(sec, func(k string) bool {
				if removeSet[k] {
					return true
				}
				_, changing := putChanges[k]
				return changing
			})
			// Keep [mcp_servers] only if it still has content beyond header.
			if len(filtered.lines) > 1 {
				outSections = append(outSections, filtered)
			}
			continue
		}
		outSections = append(outSections, sec)
	}

	// Append any newly added (or previously inline) servers as [mcp_servers.<name>] sections.
	for _, name := range putOrder {
		if handledPut[name] {
			continue
		}
		want := putChanges[name]
		lines := []string{fmt.Sprintf("[mcp_servers.%s]", formatTOMLKey(name))}
		for _, kv := range want.Entries {
			lines = append(lines, fmt.Sprintf("%s = %s", formatTOMLKey(kv.Key), formatTOMLValue(kv.Value)))
		}
		outSections = append(outSections, tomlSection{
			header: []string{"mcp_servers", name},
			lines:  lines,
		})
	}

	var b strings.Builder
	for i, sec := range outSections {
		if len(sec.lines) == 0 {
			continue
		}
		// Ensure blank line separator before table headers when previous section didn't end blank.
		if i > 0 && len(sec.header) > 0 && b.Len() > 0 {
			s := b.String()
			if !strings.HasSuffix(s, "\n\n") {
				if !strings.HasSuffix(s, "\n") {
					b.WriteByte('\n')
				}
				b.WriteByte('\n')
			}
		}
		for _, line := range sec.lines {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	out := b.String()
	if out == text {
		return "", false, nil
	}
	// Sanity-check the produced TOML parses cleanly.
	if _, err := parseTOMLMap(out); err != nil {
		return "", false, fmt.Errorf("internal error producing TOML: %w", err)
	}
	return out, true, nil
}

func rewriteTOMLServerSection(sec tomlSection, want *OrderedObject) tomlSection {
	// Collect unmanaged lines (preserving comments & unmanaged keys).
	unmanagedKeys := map[string]bool{}
	var kept []string
	var trailingBlanks []string
	headerLine := sec.lines[0]

	inMultiline := ""
	skipping := false
	for _, line := range sec.lines[1:] {
		if skipping {
			inMultiline = advanceMultilineState(line, inMultiline)
			if inMultiline == "" {
				skipping = false
			}
			continue
		}
		if inMultiline == "" {
			if key, ok := parseTOMLAssignmentKey(line); ok && isManagedKey(key) {
				inMultiline = advanceMultilineState(line, "")
				if inMultiline != "" {
					skipping = true
				}
				continue
			} else if ok {
				unmanagedKeys[key] = true
			}
		}
		inMultiline = advanceMultilineState(line, inMultiline)
		kept = append(kept, line)
	}

	// Separate trailing blank lines so new managed keys go before them.
	for len(kept) > 0 && strings.TrimSpace(kept[len(kept)-1]) == "" {
		trailingBlanks = append([]string{kept[len(kept)-1]}, trailingBlanks...)
		kept = kept[:len(kept)-1]
	}

	var managedLines []string
	for _, kv := range want.Entries {
		if unmanagedKeys[kv.Key] {
			continue
		}
		managedLines = append(managedLines, fmt.Sprintf("%s = %s", formatTOMLKey(kv.Key), formatTOMLValue(kv.Value)))
	}

	lines := make([]string, 0, 1+len(managedLines)+len(kept)+len(trailingBlanks))
	lines = append(lines, headerLine)
	lines = append(lines, managedLines...)
	lines = append(lines, kept...)
	lines = append(lines, trailingBlanks...)
	return tomlSection{
		header: sec.header,
		isArr:  sec.isArr,
		lines:  lines,
	}
}

func filterTOMLSectionKeys(sec tomlSection, drop func(key string) bool) tomlSection {
	if len(sec.lines) == 0 {
		return sec
	}
	lines := []string{sec.lines[0]}
	inMultiline := ""
	skipping := false
	for _, line := range sec.lines[1:] {
		if skipping {
			inMultiline = advanceMultilineState(line, inMultiline)
			if inMultiline == "" {
				skipping = false
			}
			continue
		}
		if inMultiline == "" {
			if key, ok := parseTOMLAssignmentKey(line); ok && drop(key) {
				inMultiline = advanceMultilineState(line, "")
				if inMultiline != "" {
					skipping = true
				}
				continue
			}
		}
		inMultiline = advanceMultilineState(line, inMultiline)
		lines = append(lines, line)
	}
	return tomlSection{header: sec.header, isArr: sec.isArr, lines: lines}
}
