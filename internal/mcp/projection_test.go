package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProjectEntityArray(t *testing.T) {
	in := `[{"id":"e1","type":"Decision","peer_did":"did:key:p1","data":{"title":"T","rationale":"a very long rationale that should be truncated"}}]`
	out, _ := projectJSON([]byte(in), []string{"id", "title", "rationale:10"})
	var v []map[string]any
	if err := json.Unmarshal(out, &v); err != nil {
		t.Fatalf("parse: %v\n%s", err, out)
	}
	if len(v) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(v))
	}
	e := v[0]
	if e["id"] != "e1" {
		t.Fatalf("missing id: %+v", e)
	}
	if _, ok := e["type"]; ok {
		t.Fatalf("type should have been dropped")
	}
	if _, ok := e["peer_did"]; ok {
		t.Fatalf("peer_did should have been dropped")
	}
	d, _ := e["data"].(map[string]any)
	if d["title"] != "T" {
		t.Fatalf("title not preserved: %+v", d)
	}
	r, _ := d["rationale"].(string)
	if !strings.HasSuffix(r, "…") || len([]rune(r)) != 11 {
		t.Fatalf("rationale not truncated to 10 + ellipsis: %q (runes=%d)", r, len([]rune(r)))
	}
}

func TestProjectNoFieldsPassthrough(t *testing.T) {
	in := `[{"id":"e1","data":{"title":"T"}}]`
	out, _ := projectJSON([]byte(in), nil)
	if string(out) != in {
		t.Fatalf("expected passthrough, got %s", out)
	}
}

func TestProjectTypeListWrapper(t *testing.T) {
	in := `{"entity_types":[{"name":"Decision","peer_did":"did:key:p1","fields":[],"description":"discrete reasoned choices"}],"relationship_types":[]}`
	out, _ := projectJSON([]byte(in), []string{"name", "description:10"})
	var v map[string]any
	if err := json.Unmarshal(out, &v); err != nil {
		t.Fatalf("parse: %v\n%s", err, out)
	}
	ets, _ := v["entity_types"].([]any)
	if len(ets) != 1 {
		t.Fatalf("expected 1 entity_type, got %d", len(ets))
	}
	td, _ := ets[0].(map[string]any)
	if td["name"] != "Decision" {
		t.Fatalf("name missing: %+v", td)
	}
	if _, ok := td["peer_did"]; ok {
		t.Fatalf("peer_did should be dropped")
	}
	desc, _ := td["description"].(string)
	if !strings.HasSuffix(desc, "…") {
		t.Fatalf("description not truncated: %q", desc)
	}
}

func TestProjectSemanticSearchWrapper(t *testing.T) {
	in := `[{"entity":{"id":"e1","type":"Decision","data":{"title":"T","rationale":"R"}},"score":0.9}]`
	out, _ := projectJSON([]byte(in), []string{"id", "title"})
	var v []map[string]any
	if err := json.Unmarshal(out, &v); err != nil {
		t.Fatalf("parse: %v\n%s", err, out)
	}
	r := v[0]
	if _, ok := r["score"]; !ok {
		t.Fatalf("score wrapper should remain: %+v", r)
	}
	e, _ := r["entity"].(map[string]any)
	if e["id"] != "e1" {
		t.Fatalf("entity id missing: %+v", e)
	}
	d, _ := e["data"].(map[string]any)
	if d["title"] != "T" {
		t.Fatalf("entity title missing: %+v", e)
	}
}

func TestProjectInvalidJSONReturnsOriginal(t *testing.T) {
	in := `not json`
	out, _ := projectJSON([]byte(in), []string{"id"})
	if string(out) != in {
		t.Fatalf("expected passthrough on parse error, got %s", out)
	}
}

func TestProjectTruncateNonStringIsNoOp(t *testing.T) {
	in := `[{"id":"e1","data":{"completion_estimate":42.5}}]`
	out, _ := projectJSON([]byte(in), []string{"id", "completion_estimate:5"})
	var v []map[string]any
	if err := json.Unmarshal(out, &v); err != nil {
		t.Fatalf("parse: %v", err)
	}
	d, _ := v[0]["data"].(map[string]any)
	if d["completion_estimate"].(float64) != 42.5 {
		t.Fatalf("non-string field unexpectedly modified: %+v", d)
	}
}
