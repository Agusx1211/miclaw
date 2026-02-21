package tools

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

func mainDeps() MainToolDeps {
	return MainToolDeps{
		Model:       "test-model",
		SendMessage: func(context.Context, string, string) error { return nil },
		IsActive:    func() bool { return false },
	}
}

func TestMainAgentToolsReturns20UniqueTools(t *testing.T) {
	got := MainAgentTools(mainDeps())
	if len(got) != 20 {
		t.Fatalf("want 20 tools, got %d", len(got))
	}
	seen := make(map[string]struct{}, len(got))
	for _, g := range got {
		if g.Name() == "" {
			t.Fatalf("tool name is empty")
		}
		name := g.Name()
		seen[name] = struct{}{}
	}
	if len(seen) != 20 {
		t.Fatalf("tool names are not unique: got %d", len(seen))
	}
}

func TestSubAgentToolsReturns6Tools(t *testing.T) {
	got := SubAgentTools(nil, nil)
	if len(got) != 6 {
		t.Fatalf("want 6 tools, got %d", len(got))
	}
	seen := make(map[string]struct{}, len(got))
	for _, g := range got {
		if g.Name() == "" {
			t.Fatalf("tool name is empty")
		}
		name := g.Name()
		seen[name] = struct{}{}
	}
	if len(seen) != 6 {
		t.Fatalf("tool names are not unique: got %d", len(seen))
	}
}

func TestMainAgentToolsOmitsMessageWhenSendUnavailable(t *testing.T) {
	deps := mainDeps()
	deps.SendMessage = nil
	got := MainAgentTools(deps)
	for _, g := range got {
		if g.Name() == "message" {
			t.Fatal("unexpected message tool")
		}
	}
	if len(got) != 19 {
		t.Fatalf("want 19 tools, got %d", len(got))
	}
}

func TestSubAgentToolsAreSubsetOfMainTools(t *testing.T) {
	mainTools := MainAgentTools(mainDeps())
	subTools := SubAgentTools(nil, nil)
	mainSet := make(map[string]struct{}, len(mainTools))
	for _, tool := range mainTools {
		mainSet[tool.Name()] = struct{}{}
	}
	for _, tool := range subTools {
		if _, ok := mainSet[tool.Name()]; !ok {
			t.Fatalf("sub-agent tool %q not in main tools", tool.Name())
		}
	}
}

func TestToProviderDefsProducesValidJSON(t *testing.T) {
	defs := ToProviderDefs(SubAgentTools(nil, nil))
	if len(defs) != 6 {
		t.Fatalf("want 6 defs, got %d", len(defs))
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
