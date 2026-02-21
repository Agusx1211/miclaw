package provider

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/agusx1211/miclaw/model"
)

func TestChatgptAccountID(t *testing.T) {
	payload := `{"https://api.openai.com/auth":{"chatgpt_account_id":"acc_123"}}`
	token := "x." + base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".y"
	got := chatgptAccountID(token)
	if got != "acc_123" {
		t.Fatalf("account id = %q", got)
	}
}

func TestMarshalCodexResponsesRequest(t *testing.T) {
	msgs := []model.Message{
		{
			ID:   "system-1",
			Role: model.RoleUser,
			Parts: []model.MessagePart{
				model.TextPart{Text: "system prompt"},
			},
		},
		{
			Role: model.RoleUser,
			Parts: []model.MessagePart{
				model.TextPart{Text: "hello"},
			},
		},
		{
			Role: model.RoleAssistant,
			Parts: []model.MessagePart{
				model.ToolCallPart{ID: "call_1", Name: "read", Parameters: json.RawMessage(`{"path":"/tmp/a"}`)},
			},
		},
		{
			Role: model.RoleTool,
			Parts: []model.MessagePart{
				model.ToolResultPart{ToolCallID: "call_1", Content: "file content"},
			},
		},
	}
	tools := []ToolDef{
		{Name: "read", Description: "Read a file", Parameters: json.RawMessage(`{"type":"object"}`)},
	}
	b, err := marshalCodexResponsesRequest("gpt-5.2-codex", "medium", msgs, tools)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	var v map[string]any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if v["model"] != "gpt-5.2-codex" || v["stream"] != true {
		t.Fatalf("unexpected request envelope: %#v", v)
	}
	if v["instructions"] != "system prompt" {
		t.Fatalf("unexpected instructions: %#v", v["instructions"])
	}
	if v["store"] != false {
		t.Fatalf("store must be false for codex responses: %#v", v["store"])
	}
	if _, ok := v["max_output_tokens"]; ok {
		t.Fatalf("max_output_tokens must be omitted: %#v", v)
	}
	if v["tool_choice"] != "auto" || v["parallel_tool_calls"] != true {
		t.Fatalf("unexpected tool settings: %#v", v)
	}
	input := v["input"].([]any)
	if len(input) != 3 {
		t.Fatalf("unexpected input len: %d", len(input))
	}
	first := input[0].(map[string]any)
	if first["type"] != "message" || first["role"] != "user" {
		t.Fatalf("unexpected first item: %#v", first)
	}
	if _, ok := first["output"]; ok {
		t.Fatalf("message item must not include output: %#v", first)
	}
	second := input[1].(map[string]any)
	if second["type"] != "function_call" || second["call_id"] != "call_1" || second["name"] != "read" {
		t.Fatalf("unexpected function_call item: %#v", second)
	}
	if !strings.Contains(second["arguments"].(string), `"path":"/tmp/a"`) {
		t.Fatalf("unexpected function arguments: %#v", second)
	}
	third := input[2].(map[string]any)
	if third["type"] != "function_call_output" || third["call_id"] != "call_1" {
		t.Fatalf("unexpected function_call_output item: %#v", third)
	}
}

func TestMarshalCodexResponsesRequestDefaultInstructions(t *testing.T) {
	msgs := []model.Message{
		{Role: model.RoleUser, Parts: []model.MessagePart{model.TextPart{Text: "hello"}}},
	}
	b, err := marshalCodexResponsesRequest("gpt-5.2-codex", "none", msgs, nil)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	var v map[string]any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if strings.TrimSpace(v["instructions"].(string)) == "" {
		t.Fatalf("instructions must not be empty")
	}
	if _, ok := v["reasoning"]; ok {
		t.Fatalf("reasoning must be omitted for effort none: %#v", v["reasoning"])
	}
}

func TestMarshalCodexResponsesRequestKeepsEmptyToolOutput(t *testing.T) {
	msgs := []model.Message{
		{Role: model.RoleUser, Parts: []model.MessagePart{model.TextPart{Text: "hello"}}},
		{Role: model.RoleTool, Parts: []model.MessagePart{model.ToolResultPart{ToolCallID: "call_1", Content: ""}}},
	}
	b, err := marshalCodexResponsesRequest("gpt-5.3-codex", "medium", msgs, nil)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	var v map[string]any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	input := v["input"].([]any)
	if len(input) != 2 {
		t.Fatalf("unexpected input len: %d", len(input))
	}
	toolOut := input[1].(map[string]any)
	if toolOut["type"] != "function_call_output" || toolOut["call_id"] != "call_1" {
		t.Fatalf("unexpected function_call_output item: %#v", toolOut)
	}
	if _, ok := toolOut["output"]; !ok {
		t.Fatalf("output field must be present even when empty: %#v", toolOut)
	}
}
