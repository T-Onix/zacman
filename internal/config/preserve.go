package config

import (
	"fmt"
	"sort"
	"strings"
)

type assignmentSpan struct {
	path       string
	start, end int // source line indexes; end is exclusive
	value      string
	lines      []string
}

// preserveUserFormatting updates only TOML assignments whose semantic value
// changed. It keeps all untouched source text byte-for-byte, including comments,
// table ordering, spacing, and user-authored notes.
func preserveUserFormatting(original, encoded []byte, oldCfg, newCfg Config) ([]byte, error) {
	oldCanonical, err := encodeTOML(oldCfg)
	if err != nil {
		return nil, err
	}
	oldCanonicalSpans := scanAssignments(oldCanonical)
	newCanonicalSpans := scanAssignments(encoded)
	originalSpans := scanAssignments(original)

	changed := make([]string, 0)
	for path, oldSpan := range oldCanonicalSpans {
		newSpan, ok := newCanonicalSpans[path]
		if ok && oldSpan.value != newSpan.value {
			changed = append(changed, path)
		}
	}
	sort.Strings(changed)
	if len(changed) == 0 {
		return append([]byte(nil), original...), nil
	}

	lines := splitSourceLines(original)
	// Replace existing assignments from the bottom up so the original line
	// indexes remain valid while applying edits.
	type edit struct {
		start, end int
		lines      []string
	}
	edits := make([]edit, 0, len(changed))
	missing := make([]string, 0)
	for _, path := range changed {
		want, ok := newCanonicalSpans[path]
		if !ok {
			continue
		}
		current, exists := originalSpans[path]
		if !exists {
			missing = append(missing, path)
			continue
		}
		replacement := preserveAssignmentComments(current.lines, want.lines)
		edits = append(edits, edit{start: current.start, end: current.end, lines: replacement})
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	for _, item := range edits {
		updated := make([]string, 0, len(lines)-(item.end-item.start)+len(item.lines))
		updated = append(updated, lines[:item.start]...)
		updated = append(updated, item.lines...)
		updated = append(updated, lines[item.end:]...)
		lines = updated
	}

	if len(missing) > 0 {
		// Insert missing keys in existing tables; if an entire table is absent,
		// append that canonical table once. Most old configs only encounter this
		// path when a newly introduced option is explicitly changed.
		sort.Strings(missing)
		missingByTable := map[string][]string{}
		for _, full := range missing {
			idx := strings.LastIndexByte(full, '.')
			table, key := "", full
			if idx >= 0 {
				table, key = full[:idx], full[idx+1:]
			}
			missingByTable[table] = append(missingByTable[table], key)
		}
		tables := make([]string, 0, len(missingByTable))
		for table := range missingByTable {
			tables = append(tables, table)
		}
		sort.Strings(tables)
		for _, table := range tables {
			keys := missingByTable[table]
			sort.Strings(keys)
			start, end, found := findTable(lines, table)
			if !found {
				section, ok := extractTable(encoded, table)
				if !ok {
					return nil, fmt.Errorf("cannot preserve TOML formatting: changed table [%s] is absent from encoded output", table)
				}
				lines = appendBlankSeparated(lines, section)
				continue
			}
			insert := end
			for insert > start+1 && strings.TrimSpace(lines[insert-1]) == "" {
				insert--
			}
			toInsert := make([]string, 0)
			for _, key := range keys {
				full := key
				if table != "" {
					full = table + "." + key
				}
				span, ok := newCanonicalSpans[full]
				if ok {
					toInsert = append(toInsert, span.lines...)
				}
			}
			updated := make([]string, 0, len(lines)+len(toInsert))
			updated = append(updated, lines[:insert]...)
			updated = append(updated, toInsert...)
			updated = append(updated, lines[insert:]...)
			lines = updated
		}
	}
	out := []byte(strings.Join(lines, "\n"))
	if len(original) > 0 && original[len(original)-1] == '\n' && (len(out) == 0 || out[len(out)-1] != '\n') {
		out = append(out, '\n')
	}
	return out, nil
}

func splitSourceLines(data []byte) []string {
	text := strings.TrimSuffix(string(data), "\n")
	if text == "" {
		return []string{}
	}
	return strings.Split(text, "\n")
}

func scanAssignments(data []byte) map[string]assignmentSpan {
	lines := splitSourceLines(data)
	result := make(map[string]assignmentSpan)
	table := ""
	start := -1
	fullPath := ""
	var statement strings.Builder
	for i, raw := range lines {
		clean := stripTOMLComment(raw)
		trimmed := strings.TrimSpace(clean)
		if start < 0 {
			if trimmed == "" {
				continue
			}
			if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") && !strings.HasPrefix(trimmed, "[[") {
				table = strings.TrimSpace(trimmed[1 : len(trimmed)-1])
				continue
			}
			eq := strings.IndexByte(trimmed, '=')
			if eq < 1 {
				continue
			}
			key := strings.TrimSpace(trimmed[:eq])
			fullPath = key
			if table != "" {
				fullPath = table + "." + key
			}
			start = i
			statement.Reset()
			statement.WriteString(clean)
		} else {
			statement.WriteByte('\n')
			statement.WriteString(clean)
		}
		open, err := hasOpenArray(statement.String())
		if err != nil {
			start = -1
			statement.Reset()
			continue
		}
		if open {
			continue
		}
		rawBlock := append([]string(nil), lines[start:i+1]...)
		value := ""
		if eq := strings.IndexByte(statement.String(), '='); eq >= 0 {
			value = strings.TrimSpace(statement.String()[eq+1:])
		}
		result[fullPath] = assignmentSpan{path: fullPath, start: start, end: i + 1, value: value, lines: rawBlock}
		start = -1
		fullPath = ""
		statement.Reset()
	}
	return result
}

func preserveAssignmentComments(oldLines, newLines []string) []string {
	out := append([]string(nil), newLines...)
	if len(oldLines) == 0 || len(newLines) == 0 {
		return out
	}
	floating := []string{}
	trailing := ""
	for i, line := range oldLines {
		clean := stripTOMLComment(line)
		trimmedClean := strings.TrimSpace(clean)
		trimmedRaw := strings.TrimSpace(line)
		if trimmedClean == "" && strings.HasPrefix(trimmedRaw, "#") {
			floating = append(floating, line)
			continue
		}
		if len(clean) < len(line) {
			comment := strings.TrimSpace(line[len(clean):])
			if !strings.HasPrefix(comment, "#") {
				continue
			}
			if i == len(oldLines)-1 {
				trailing = comment
			} else {
				floating = append(floating, "  "+comment)
			}
		}
	}
	isArray := strings.Contains(out[0], "[") && strings.HasSuffix(strings.TrimSpace(out[len(out)-1]), "]")
	if isArray && len(floating) > 0 {
		merged := make([]string, 0, len(out)+len(floating))
		merged = append(merged, out[0])
		merged = append(merged, floating...)
		merged = append(merged, out[1:]...)
		out = merged
	}
	if trailing != "" {
		last := len(out) - 1
		out[last] += " " + trailing
	}
	return out
}

func findTable(lines []string, table string) (start, end int, found bool) {
	wanted := "[" + table + "]"
	for i, line := range lines {
		trimmed := strings.TrimSpace(stripTOMLComment(line))
		if trimmed != wanted {
			continue
		}
		for j := i + 1; j < len(lines); j++ {
			candidate := strings.TrimSpace(stripTOMLComment(lines[j]))
			if strings.HasPrefix(candidate, "[") && strings.HasSuffix(candidate, "]") && !strings.HasPrefix(candidate, "[[") {
				return i, j, true
			}
		}
		return i, len(lines), true
	}
	return 0, 0, false
}

func extractTable(data []byte, table string) ([]string, bool) {
	lines := splitSourceLines(data)
	start, end, ok := findTable(lines, table)
	if !ok {
		return nil, false
	}
	return append([]string(nil), lines[start:end]...), true
}

func appendBlankSeparated(lines, appended []string) []string {
	out := append([]string(nil), lines...)
	if len(out) > 0 && out[len(out)-1] != "" {
		out = append(out, "")
	}
	out = append(out, appended...)
	if len(out) == 0 || out[len(out)-1] != "" {
		out = append(out, "")
	}
	return out
}
