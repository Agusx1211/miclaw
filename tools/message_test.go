package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/agusx1211/miclaw/model"
)

func runMessageCall(t *testing.T, tool Tool, params map[string]any) (ToolResult, error) {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	got, err := tool.Run(context.Background(), model.ToolCallPart{
		Name:       "message",
		Parameters: raw,
	})
	return got, err
}

func TestMessageToolSendsSignalMessage(t *testing.T) {
	var gotTarget, gotContent string
	tool := messageTool(func(_ context.Context, recipient, content string) error {
		gotTarget = recipient
		gotContent = content
		return nil
	})
	got, err := runMessageCall(t, tool, map[string]any{
		"target":  "signal:+15551234567",
		"content": "hi",
	})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if got.IsError {
		t.Fatalf("unexpected tool error: %q", got.Content)
	}
	if gotTarget != "+15551234567" || gotContent != "hi" {
		t.Fatalf("unexpected message payload: target=%q content=%q", gotTarget, gotContent)
	}
	if got.Content != "message sent to signal:+15551234567" {
		t.Fatalf("unexpected tool result: %q", got.Content)
	}
}

func TestMessageToolRejectsInvalidTarget(t *testing.T) {
	called := false
	tool := messageTool(func(context.Context, string, string) error {
		called = true
		return nil
	})
	got, err := runMessageCall(t, tool, map[string]any{
		"target":  "badformat",
		"content": "hi",
	})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if !got.IsError {
		t.Fatal("expected error")
	}
	if called {
		t.Fatal("sender should not be called for invalid target")
	}
	if !strings.Contains(got.Content, "target must include channel and address") {
		t.Fatalf("unexpected error: %q", got.Content)
	}
}

func TestMessageToolRejectsUnsupportedChannel(t *testing.T) {
	called := false
	tool := messageTool(func(context.Context, string, string) error {
		called = true
		return nil
	})
	got, err := runMessageCall(t, tool, map[string]any{
		"target":  "slack:channel",
		"content": "hi",
	})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if !got.IsError {
		t.Fatal("expected error")
	}
	if called {
		t.Fatal("sender should not be called for unsupported channel")
	}
	if !strings.Contains(got.Content, "unsupported channel") {
		t.Fatalf("unexpected error: %q", got.Content)
	}
}

func TestMessageToolRejectsEmptyContent(t *testing.T) {
	called := false
	tool := messageTool(func(context.Context, string, string) error {
		called = true
		return nil
	})
	got, err := runMessageCall(t, tool, map[string]any{
		"target":  "signal:+15551234567",
		"content": "   ",
	})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if !got.IsError {
		t.Fatal("expected error")
	}
	if called {
		t.Fatal("sender should not be called for empty content")
	}
	if !strings.Contains(got.Content, "content is required") {
		t.Fatalf("unexpected error: %q", got.Content)
	}
}

func TestMessageToolPropagatesSenderError(t *testing.T) {
	tool := messageTool(func(context.Context, string, string) error {
		return errors.New("gateway failure")
	})
	got, err := runMessageCall(t, tool, map[string]any{
		"target":  "signal:+15551234567",
		"content": "hi",
	})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if !got.IsError {
		t.Fatal("expected error")
	}
	if !strings.Contains(got.Content, "gateway failure") {
		t.Fatalf("unexpected error: %q", got.Content)
	}
}
