// Package schema defines seed entity and relationship types and provides
// loose validation helpers. Validation is intentionally permissive in v0.1;
// strict enforcement arrives in v0.7.
package schema

import (
	"fmt"
	"time"

	"github.com/lunguini/coggo/internal/types"
)

// SeedEntityTypes returns the entity-type definitions every fresh peer ships
// with. They are starter types — users keep, modify, extend, or supplement.
func SeedEntityTypes(peerDID string) []*types.EntityTypeDefinition {
	return []*types.EntityTypeDefinition{
		{
			Name:        "Project",
			PeerDID:     peerDID,
			Description: "Work or efforts with goals and trajectories",
			Fields: []types.FieldDef{
				{Name: "title", Type: types.FieldString, Required: true},
				{Name: "status", Type: types.FieldString, Default: "active",
					Description: "active | paused | completed | abandoned"},
				{Name: "description", Type: types.FieldString},
				{Name: "completion_estimate", Type: types.FieldNumber,
					Description: "0-100 estimate of completion"},
				{Name: "tags", Type: types.FieldListOf, ElementType: types.FieldString},
				{Name: "external_url", Type: types.FieldString,
					Description: "GitHub URL, project page, etc."},
			},
		},
		{
			Name:        "Domain",
			PeerDID:     peerDID,
			Description: "Life areas (health, finance, social, music, etc.)",
			Fields: []types.FieldDef{
				{Name: "title", Type: types.FieldString, Required: true},
				{Name: "description", Type: types.FieldString},
				{Name: "tags", Type: types.FieldListOf, ElementType: types.FieldString},
			},
		},
		{
			Name:        "Decision",
			PeerDID:     peerDID,
			Description: "Discrete reasoned choices with rationale, alternatives, supersedes",
			Fields: []types.FieldDef{
				{Name: "title", Type: types.FieldString, Required: true,
					Description: "One-line summary of the decision"},
				{Name: "rationale", Type: types.FieldString, Required: true,
					Description: "Why this was decided"},
				{Name: "alternatives", Type: types.FieldListOf, ElementType: types.FieldString,
					Description: "Alternatives considered and rejected"},
				{Name: "context", Type: types.FieldString,
					Description: "Background that led to this decision"},
				{Name: "confidence", Type: types.FieldString,
					Description: "low | medium | high"},
			},
		},
		{
			Name:        "Goal",
			PeerDID:     peerDID,
			Description: "Desired states with time horizons",
			Fields: []types.FieldDef{
				{Name: "title", Type: types.FieldString, Required: true},
				{Name: "description", Type: types.FieldString},
				{Name: "target_date", Type: types.FieldTimestamp},
				{Name: "status", Type: types.FieldString, Default: "open",
					Description: "open | achieved | abandoned | paused"},
				{Name: "success_criteria", Type: types.FieldString,
					Description: "What it looks like when achieved"},
			},
		},
		{
			Name:        "Observation",
			PeerDID:     peerDID,
			Description: "Context, learnings, signals that don't fit elsewhere",
			Fields: []types.FieldDef{
				{Name: "text", Type: types.FieldString, Required: true},
				{Name: "tags", Type: types.FieldListOf, ElementType: types.FieldString},
				{Name: "source", Type: types.FieldString,
					Description: "Where this observation came from"},
			},
		},
		{
			Name:        "Setting",
			PeerDID:     peerDID,
			Description: "Coggo behavioral settings stored as data",
			Fields: []types.FieldDef{
				{Name: "key", Type: types.FieldString, Required: true},
				{Name: "value", Type: types.FieldString, Required: true},
				{Name: "scope", Type: types.FieldString,
					Description: "global | peer-specific"},
			},
		},
	}
}

// SeedRelationshipTypes returns the relationship-type definitions every fresh
// peer ships with.
func SeedRelationshipTypes(peerDID string) []*types.RelationshipTypeDefinition {
	return []*types.RelationshipTypeDefinition{
		{
			Name:        "depends_on",
			PeerDID:     peerDID,
			Description: "Directional dependency",
			Directional: true,
			Fields: []types.FieldDef{
				{Name: "kind", Type: types.FieldString,
					Description: "blocking | soft | informational"},
			},
		},
		{
			Name:        "supersedes",
			PeerDID:     peerDID,
			Description: "Replacement (decisions, goals)",
			Directional: true,
			Fields: []types.FieldDef{
				{Name: "reason", Type: types.FieldString},
			},
		},
		{
			Name:        "affects",
			PeerDID:     peerDID,
			Description: "Entity A affects entity B's state or trajectory",
			Directional: true,
			Fields: []types.FieldDef{
				{Name: "nature", Type: types.FieldString,
					Description: "positive | negative | mixed | neutral"},
			},
		},
	}
}

// ValidationIssue describes one problem with a write. Severity "error" should
// block the write; "warning" should be logged and accepted in v0.1.
type ValidationIssue struct {
	Field    string
	Severity string // "error" | "warning"
	Message  string
}

// ValidateEntity applies loose v0.1 validation: required fields must be
// present (error); type mismatches and unknown fields produce warnings only.
// Defaults are applied in-place when missing.
func ValidateEntity(def *types.EntityTypeDefinition, fields map[string]any) []ValidationIssue {
	if def == nil {
		return []ValidationIssue{{Severity: "error", Message: "no type definition supplied"}}
	}
	if fields == nil {
		fields = map[string]any{}
	}

	var issues []ValidationIssue
	known := map[string]types.FieldDef{}
	for _, f := range def.Fields {
		known[f.Name] = f
		if _, ok := fields[f.Name]; !ok {
			if f.Default != nil {
				fields[f.Name] = f.Default
				continue
			}
			if f.Required {
				issues = append(issues, ValidationIssue{
					Field:    f.Name,
					Severity: "error",
					Message:  "required field missing",
				})
			}
			continue
		}
		if !checkType(fields[f.Name], f) {
			issues = append(issues, ValidationIssue{
				Field:    f.Name,
				Severity: "warning",
				Message:  fmt.Sprintf("expected %s", f.Type),
			})
		}
	}
	for name := range fields {
		if _, ok := known[name]; !ok {
			issues = append(issues, ValidationIssue{
				Field:    name,
				Severity: "warning",
				Message:  "unknown field; accepted but not in type definition",
			})
		}
	}
	return issues
}

func checkType(v any, def types.FieldDef) bool {
	if v == nil {
		return true
	}
	switch def.Type {
	case types.FieldString, types.FieldReference:
		_, ok := v.(string)
		return ok
	case types.FieldNumber:
		switch v.(type) {
		case float64, float32, int, int32, int64:
			return true
		}
		return false
	case types.FieldBoolean:
		_, ok := v.(bool)
		return ok
	case types.FieldTimestamp:
		switch x := v.(type) {
		case time.Time:
			return true
		case string:
			_, err := time.Parse(time.RFC3339, x)
			return err == nil
		}
		return false
	case types.FieldListOf:
		_, ok := v.([]any)
		return ok
	}
	return true
}

// HasErrors reports whether any issue has Severity == "error".
func HasErrors(issues []ValidationIssue) bool {
	for _, i := range issues {
		if i.Severity == "error" {
			return true
		}
	}
	return false
}
