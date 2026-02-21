package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agusx1211/miclaw/config"
	"github.com/agusx1211/miclaw/model"
)

func lmStudioServer(t *testing.T, capture *streamCapture, fn func(http.ResponseWriter, *http.Request)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		var req chatRequest
		if err := json.Unmarshal(b, &req); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		capture.push(r, req)
		fn(w, r)
	}))
}

func lmStudioProvider(baseURL, apiKey string) *LMStudio {
	cfg := config.ProviderConfig{
		BaseURL:   baseURL,
		APIKey:    apiKey,
		Model:     "qwen2.5",
		MaxTokens: 128,
	}
	return NewLMStudio(cfg)
}

func TestLMStudioStreamSuccessContent(t *testing.T) {
	c := &streamCapture{}
	srv := lmStudioServer(t, c, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hello \"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"world\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	})
	defer srv.Close()

	p := lmStudioProvider(srv.URL, "lmstudio")
	msgs := []model.Message{{Role: model.RoleUser, Parts: []model.MessagePart{model.TextPart{Text: "say hi"}}}}
	ev := collectProviderEvents(t, p.Stream(context.Background(), msgs, nil))
	if len(ev) != 3 {
		t.Fatalf("expected 3 events, got %d", len(ev))
	}
	if ev[0].Type != EventContentDelta || ev[0].Delta != "hello " {
		t.Fatalf("unexpected first event: %#v", ev[0])
	}
	if ev[1].Type != EventContentDelta || ev[1].Delta != "world" {
		t.Fatalf("unexpected second event: %#v", ev[1])
	}
	if ev[2].Type != EventComplete {
		t.Fatalf("unexpected completion: %#v", ev[2])
	}
	req := c.firstRequest()
	if req.Model != "qwen2.5" || !req.Stream || req.MaxTokens != 128 {
		t.Fatalf("unexpected request: %#v", req)
	}
}

func TestLMStudioStreamToolCalls(t *testing.T) {
	c := &streamCapture{}
	srv := lmStudioServer(t, c, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_read\",\"function\":{\"name\":\"read\",\"arguments\":\"{\"}}]}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"path\\\":\\\"/tmp/a\\\"}\"}}]}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"finish_reason\":\"tool_calls\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	})
	defer srv.Close()

	p := lmStudioProvider(srv.URL, "lmstudio-tool")
	msgs := []model.Message{{Role: model.RoleUser, Parts: []model.MessagePart{model.TextPart{Text: "read file"}}}}
	tools := []ToolDef{{
		Name:        "read",
		Description: "read a file",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}, "required":["path"]}`),
	}}
	ev := collectProviderEvents(t, p.Stream(context.Background(), msgs, tools))
	if len(ev) != 4 {
		t.Fatalf("expected 4 events, got %d", len(ev))
	}
	if ev[0].Type != EventToolUseStart || ev[0].ToolCallID != "call_read" {
		t.Fatalf("unexpected first event: %#v", ev[0])
	}
	if ev[1].Type != EventToolUseDelta || ev[1].ToolCallID != "call_read" {
		t.Fatalf("unexpected second event: %#v", ev[1])
	}
	if ev[2].Type != EventToolUseStop || ev[2].ToolCallID != "call_read" {
		t.Fatalf("unexpected third event: %#v", ev[2])
	}
	if ev[3].Type != EventComplete {
		t.Fatalf("unexpected completion event: %#v", ev[3])
	}
}

func TestLMStudioStreamConnectionError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatalf("handler must not be called")
	}))
	srv.Close()

	p := lmStudioProvider(srv.URL, "lmstudio")
	msgs := []model.Message{{Role: model.RoleUser, Parts: []model.MessagePart{model.TextPart{Text: "ping"}}}}
	ev := collectProviderEvents(t, p.Stream(context.Background(), msgs, nil))
	if len(ev) != 1 || ev[0].Type != EventError {
		t.Fatalf("expected 1 error event, got %#v", ev)
	}
}

func TestLMStudioModel(t *testing.T) {
	p := lmStudioProvider("http://127.0.0.1:1234/v1", "lmstudio")
	m := p.Model()
	if m.ID != "qwen2.5" || m.Name != "qwen2.5" || m.MaxOutput != 128 {
		t.Fatalf("unexpected model: %#v", m)
	}
	if m.CostPerInputToken != 0 || m.CostPerOutputToken != 0 {
		t.Fatalf("expected zero costs, got %#v", m)
	}
}
