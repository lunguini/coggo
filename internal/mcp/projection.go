package mcp

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// fieldsParamDoc is the per-tool documentation block describing the `fields`
// parameter. Concatenated into each read tool's description so AI clients
// see a uniform contract.
const fieldsParamDoc = "  fields (array of strings, optional): GraphQL-style allowlist for the response. " +
	"When provided, each result object is reduced to only the named keys, drawn from both top-level metadata " +
	"(id, type, peer_did, created_at, etc.) and `data.*` fields. " +
	"Append `:N` to truncate a string field to N characters (e.g. `rationale:200`). " +
	"Omit `fields` for the full payload.\n\n"

// fieldsParam returns the MCP tool option declaring the `fields` parameter.
// Shared so all read tools accept it identically.
func fieldsParam() mcp.ToolOption {
	return mcp.WithArray(
		"fields",
		mcp.Description("Allowlist of fields to return; suffix with :N to truncate strings. Omit for the full payload."),
		mcp.Items(map[string]any{"type": "string"}),
	)
}

// projectJSON walks a federation response payload and applies a GraphQL-style
// field allowlist. Each field name may carry a `:N` suffix to truncate string
// values to N characters (UTF-8 safe; appends a U+2026 ellipsis when
// truncated). When fields is empty, the input is returned unchanged.
//
// Shape rules:
//   - Arrays are walked element-by-element.
//   - Entity-shaped objects (have a `data` object) are projected:
//     selected top-level keys are kept; selected names matching keys inside
//     `data` are kept inside `data`. Unknown selections are dropped.
//   - TypeDef-shaped objects (have `name` and a `fields` array) are projected
//     on top-level keys only.
//   - Objects that don't match those shapes have their values walked
//     recursively (so wrappers like `{entity_types:[...], relationship_types:[...]}`
//     and `{entity:{...}, score:N}` apply projection to the nested entities).
//
// On parse error, the original input is returned untouched.
func projectJSON(raw []byte, fields []string) ([]byte, error) {
	if len(fields) == 0 || len(raw) == 0 {
		return raw, nil
	}
	sel := parseSelections(fields)
	if len(sel.keep) == 0 {
		return raw, nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return raw, nil
	}
	out := walk(v, sel)
	b, err := json.Marshal(out)
	if err != nil {
		return raw, err
	}
	return b, nil
}

type selection struct {
	keep     map[string]bool
	truncate map[string]int
}

func parseSelections(fields []string) selection {
	s := selection{keep: map[string]bool{}, truncate: map[string]int{}}
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		name := f
		if i := strings.IndexByte(f, ':'); i > 0 {
			name = f[:i]
			if n, err := strconv.Atoi(f[i+1:]); err == nil && n > 0 {
				s.truncate[name] = n
			}
		}
		s.keep[name] = true
	}
	return s
}

func walk(v any, sel selection) any {
	switch t := v.(type) {
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = walk(e, sel)
		}
		return out
	case map[string]any:
		if isEntityLike(t) {
			return projectEntity(t, sel)
		}
		if isTypeDefLike(t) {
			return projectFlat(t, sel)
		}
		out := make(map[string]any, len(t))
		for k, e := range t {
			out[k] = walk(e, sel)
		}
		return out
	default:
		return v
	}
}

func isEntityLike(m map[string]any) bool {
	_, hasData := m["data"]
	if !hasData {
		return false
	}
	_, dataIsObj := m["data"].(map[string]any)
	return dataIsObj
}

func isTypeDefLike(m map[string]any) bool {
	if _, ok := m["name"].(string); !ok {
		return false
	}
	_, ok := m["fields"].([]any)
	return ok
}

func projectEntity(m map[string]any, sel selection) map[string]any {
	out := make(map[string]any, len(sel.keep))
	data, _ := m["data"].(map[string]any)
	for name := range sel.keep {
		if v, ok := m[name]; ok && name != "data" {
			out[name] = applyTruncate(v, name, sel)
			continue
		}
		if dv, ok := data[name]; ok {
			d, _ := out["data"].(map[string]any)
			if d == nil {
				d = map[string]any{}
			}
			d[name] = applyTruncate(dv, name, sel)
			out["data"] = d
		}
	}
	return out
}

func projectFlat(m map[string]any, sel selection) map[string]any {
	out := make(map[string]any, len(sel.keep))
	for name := range sel.keep {
		if v, ok := m[name]; ok {
			out[name] = applyTruncate(v, name, sel)
		}
	}
	return out
}

func applyTruncate(v any, name string, sel selection) any {
	n, ok := sel.truncate[name]
	if !ok {
		return v
	}
	s, isStr := v.(string)
	if !isStr {
		return v
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
