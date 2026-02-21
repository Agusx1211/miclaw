package agent

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func assertPartEqual(t *testing.T, want, got MessagePart) {
	t.Helper()
	switch w := want.(type) {
	case TextPart:
		g, ok := got.(TextPart)
		if !ok {
			t.Fatalf("part type mismatch: expected TextPart, got %T", got)
		}
		if !reflect.DeepEqual(w, g) {
			t.Fatalf("text part mismatch: %v != %v", w, g)
		}
	case ReasoningPart:
		g, ok := got.(ReasoningPart)
		if !ok {
			t.Fatalf("part type mismatch: expected ReasoningPart, got %T", got)
		}
		if !reflect.DeepEqual(w, g) {
			t.Fatalf("reasoning part mismatch: %v != %v", w, g)
		}
	case ToolCallPart:
		g, ok := got.(ToolCallPart)
		if !ok {
			t.Fatalf("part type mismatch: expected ToolCallPart, got %T", got)
		}
		if w.ID != g.ID || w.Name != g.Name || !reflect.DeepEqual(w.Parameters, g.Parameters) {
			t.Fatalf("tool call part mismatch: %v != %v", w, g)
		}
	case ToolResultPart:
		g, ok := got.(ToolResultPart)
		if !ok {
			t.Fatalf("part type mismatch: expected ToolResultPart, got %T", got)
		}
		if !reflect.DeepEqual(w, g) {
			t.Fatalf("tool result part mismatch: %v != %v", w, g)
		}
	case FinishPart:
		g, ok := got.(FinishPart)
		if !ok {
			t.Fatalf("part type mismatch: expected FinishPart, got %T", got)
		}
		if !reflect.DeepEqual(w, g) {
			t.Fatalf("finish part mismatch: %v != %v", w, g)
		}
	case BinaryPart:
		g, ok := got.(BinaryPart)
		if !ok {
			t.Fatalf("part type mismatch: expected BinaryPart, got %T", got)
		}
		if w.MimeType != g.MimeType || !reflect.DeepEqual(w.Data, g.Data) {
			t.Fatalf("binary part mismatch: %v != %v", w, g)
		}
	default:
		t.Fatalf("unknown part type %T", want)
	}
}

func TestMessagePartJSONRoundTripText(t *testing.T) {
	msg := Message{
		ID:        "m1",
		SessionID: "s1",
		Role:      RoleUser,
		CreatedAt: time.Date(2026, 2, 20, 12, 0, 0, 0, time.UTC),
		Parts:     []MessagePart{TextPart{Text: "hello"}},
	}
	b, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if !strings.Contains(string(b), `"type":"text"`) {
		t.Fatalf("missing type discriminator for text part")
	}
	var got Message
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(got.Parts) != 1 {
		t.Fatalf("expected one part, got %d", len(got.Parts))
	}
	assertPartEqual(t, TextPart{Text: "hello"}, got.Parts[0])
}

func TestMessagePartJSONRoundTripToolCall(t *testing.T) {
	want := ToolCallPart{
		ID:         "tc1",
		Name:       "search_files",
		Parameters: json.RawMessage(`{"path":"/tmp","depth":3}`),
	}
	msg := Message{
		ID:        "m2",
		SessionID: "s2",
		Role:      RoleAssistant,
		CreatedAt: time.Date(2026, 2, 20, 12, 0, 0, 0, time.UTC),
		Parts:     []MessagePart{want},
	}
	b, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if !strings.Contains(string(b), `"type":"tool_call"`) {
		t.Fatalf("missing type discriminator for tool call part")
	}
	var got Message
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(got.Parts) != 1 {
		t.Fatalf("expected one part, got %d", len(got.Parts))
	}
	assertPartEqual(t, want, got.Parts[0])
}

func TestMessagePartJSONRoundTripBinary(t *testing.T) {
	want := BinaryPart{
		MimeType: "image/png",
		Data:     []byte{0x01, 0x02, 0x7f, 0xff},
	}
	msg := Message{
		ID:        "m3",
		SessionID: "s3",
		Role:      RoleUser,
		CreatedAt: time.Date(2026, 2, 20, 12, 0, 0, 0, time.UTC),
		Parts:     []MessagePart{want},
	}
	b, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if !strings.Contains(string(b), `"type":"binary"`) {
		t.Fatalf("missing type discriminator for binary part")
	}
	var got Message
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(got.Parts) != 1 {
		t.Fatalf("expected one part, got %d", len(got.Parts))
	}
	assertPartEqual(t, want, got.Parts[0])
}

func TestMessagePartJSONRoundTripReasoning(t *testing.T) {
	want := ReasoningPart{Text: "thinking step"}
	msg := Message{
		ID:        "m4",
		SessionID: "s4",
		Role:      RoleAssistant,
		CreatedAt: time.Date(2026, 2, 20, 12, 0, 0, 0, time.UTC),
		Parts:     []MessagePart{want},
	}
	b, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if !strings.Contains(string(b), `"type":"reasoning"`) {
		t.Fatalf("missing type discriminator for reasoning part")
	}
	var got Message
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(got.Parts) != 1 {
		t.Fatalf("expected one part, got %d", len(got.Parts))
	}
	assertPartEqual(t, want, got.Parts[0])
}

func TestMessageJSONRoundTripMixedParts(t *testing.T) {
	wantToolResult := ToolResultPart{
		ToolCallID: "tc2",
		Content:    "ok",
		IsError:    false,
	}
	msg := Message{
		ID:        "m5",
		SessionID: "s5",
		Role:      RoleAssistant,
		CreatedAt: time.Date(2026, 2, 20, 12, 0, 0, 0, time.UTC),
		Parts: []MessagePart{
			TextPart{Text: "one"},
			ReasoningPart{Text: "two"},
			ToolCallPart{ID: "tc2", Name: "do", Parameters: json.RawMessage(`{}`)},
			BinaryPart{MimeType: "text/plain", Data: []byte("abc")},
			FinishPart{Reason: "done"},
			wantToolResult,
		},
	}
	b, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if !strings.Contains(string(b), `"type":"text"`) {
		t.Fatalf("missing type discriminator for text part")
	}
	var got Message
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if got.ID != "m5" || got.SessionID != "s5" || got.Role != RoleAssistant {
		t.Fatalf("message metadata changed: %#v", got)
	}
	if len(got.Parts) != 6 {
		t.Fatalf("expected six parts, got %d", len(got.Parts))
	}
	assertPartEqual(t, TextPart{Text: "one"}, got.Parts[0])
	assertPartEqual(t, ReasoningPart{Text: "two"}, got.Parts[1])
	assertPartEqual(t, ToolCallPart{ID: "tc2", Name: "do", Parameters: json.RawMessage(`{}`)}, got.Parts[2])
	assertPartEqual(t, BinaryPart{MimeType: "text/plain", Data: []byte("abc")}, got.Parts[3])
	assertPartEqual(t, FinishPart{Reason: "done"}, got.Parts[4])
	assertPartEqual(t, wantToolResult, got.Parts[5])
}

func TestMessageJSONEmptyParts(t *testing.T) {
	msg := Message{
		ID:        "m6",
		SessionID: "s6",
		Role:      RoleUser,
		CreatedAt: time.Date(2026, 2, 20, 12, 0, 0, 0, time.UTC),
		Parts:     []MessagePart{},
	}
	b, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if !strings.Contains(string(b), `"parts":[]`) {
		t.Fatalf("expected empty parts array in JSON: %s", string(b))
	}
	var got Message
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(got.Parts) != 0 {
		t.Fatalf("expected no parts, got %d", len(got.Parts))
	}
}

func TestSessionJSONRoundTrip(t *testing.T) {
	want := Session{
		ID:               "s1",
		ParentSessionID:  "p1",
		Title:            "title",
		MessageCount:     7,
		PromptTokens:     11,
		CompletionTokens: 22,
		SummaryMessageID: "m0",
		Cost:             3.14,
		CreatedAt:        time.Date(2026, 2, 20, 12, 0, 0, 0, time.UTC),
		UpdatedAt:        time.Date(2026, 2, 21, 12, 0, 0, 0, time.UTC),
	}
	b, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var got Session
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("session round-trip mismatch: %v != %v", want, got)
	}
}
