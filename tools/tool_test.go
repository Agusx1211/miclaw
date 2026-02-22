package tools

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func mainDeps() MainToolDeps {
	return MainToolDeps{
		SendMessage: func(context.Context, string, string) error { return nil },
	}
}

func mainDepsWithTyping() MainToolDeps {
	deps := mainDeps()
	deps.StartTyping = func(context.Context, string, time.Duration) error { return nil }
	deps.StopTyping = func(context.Context, string) error { return nil }
	return deps
}

func TestMainAgentToolsReturns13UniqueTools(t *testing.T) {
	got := MainAgentTools(mainDeps())
	if len(got) != 13 {
		t.Fatalf("want 13 tools, got %d", len(got))
	}
	seen := make(map[string]struct{}, len(got))
	for _, g := range got {
		if g.Name() == "" {
			t.Fatalf("tool name is empty")
		}
		name := g.Name()
		seen[name] = struct{}{}
	}
	if len(seen) != 13 {
		t.Fatalf("tool names are not unique: got %d", len(seen))
	}
}

func TestMainAgentToolsIncludesTypingWhenConfigured(t *testing.T) {
	got := MainAgentTools(mainDepsWithTyping())
	if len(got) != 14 {
		t.Fatalf("want 14 tools, got %d", len(got))
	}
	seen := make(map[string]struct{}, len(got))
	for _, g := range got {
		if g.Name() == "" {
			t.Fatalf("tool name is empty")
		}
		name := g.Name()
		seen[name] = struct{}{}
	}
	if len(seen) != 14 {
		t.Fatalf("tool names are not unique: got %d", len(seen))
	}
	if _, ok := seen["typing"]; !ok {
		t.Fatal("typing tool missing")
	}
}

func TestToProviderDefsProducesValidJSON(t *testing.T) {
	defs := ToProviderDefs(MainAgentTools(mainDeps()))
	if len(defs) != 13 {
		t.Fatalf("want 13 defs, got %d", len(defs))
	}
	for _, def := range defs {
		if !json.Valid(def.Parameters) {
			t.Fatalf("tool %q parameters are not valid JSON", def.Name)
		}
		var body map[string]any
		if err := json.Unmarshal(def.Parameters, &body); err != nil {
			t.Fatalf("tool %q parameters are not valid JSON object: %v", def.Name, err)
		}
	}
}

func TestToProviderDefsIncludesTyping(t *testing.T) {
	defs := ToProviderDefs(MainAgentTools(mainDepsWithTyping()))
	if len(defs) != 14 {
		t.Fatalf("want 14 defs, got %d", len(defs))
	}
	names := make(map[string]struct{}, len(defs))
	for _, d := range defs {
		names[d.Name] = struct{}{}
	}
	if _, ok := names["typing"]; !ok {
		t.Fatal("typing tool def missing")
	}
}

func TestJSONSchemaMarshal(t *testing.T) {
	raw := JSONSchema{
		Type:     "object",
		Required: []string{"path"},
		Properties: map[string]JSONSchema{
			"path": {
				Type: "string",
				Desc: "path to file",
			},
		},
	}
	b, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var got JSONSchema
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(raw, got) {
		t.Fatalf("round trip mismatch: %#v != %#v", raw, got)
	}
}

func TestJSONSchemaObjectIncludesEmptyProperties(t *testing.T) {
	raw := JSONSchema{Type: "object"}
	b, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if _, ok := got["properties"]; !ok {
		t.Fatalf("missing properties in object schema: %s", string(b))
	}
}
