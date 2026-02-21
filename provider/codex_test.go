package provider

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/agusx1211/miclaw/config"
	"github.com/agusx1211/miclaw/model"
)

func codexProvider(baseURL, apiKey string, store bool, effort string) *Codex {
	cfg := config.ProviderConfig{
		BaseURL:        baseURL,
		APIKey:         apiKey,
		Model:          "codex-mini-latest",
		MaxTokens:      256,
		ThinkingEffort: effort,
		Store:          store,
	}
	return NewCodex(cfg)
}

func TestCodexStreamSuccessContent(t *testing.T) {
	c := &streamCapture{}
	srv := openRouterServer(t, c, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hel\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"lo\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	})
	defer srv.Close()

	p := codexProvider(srv.URL, "sk-codex-test", false, "")
	msgs := []model.Message{{Role: model.RoleUser, Parts: []model.MessagePart{model.TextPart{Text: "hello"}}}}
	ev := collectProviderEvents(t, p.Stream(context.Background(), msgs, nil))
	if len(ev) != 3 {
		t.Fatalf("expected 3 events, got %d", len(ev))
	}
	if ev[0].Type != EventContentDelta || ev[0].Delta != "hel" {
		t.Fatalf("unexpected first event: %#v", ev[0])
	}
	if ev[1].Type != EventContentDelta || ev[1].Delta != "lo" {
		t.Fatalf("unexpected second event: %#v", ev[1])
	}
	if ev[2].Type != EventComplete {
		t.Fatalf("unexpected completion event: %#v", ev[2])
	}
	req := c.firstRequest()
	if req.Model != "codex-mini-latest" || !req.Stream {
		t.Fatalf("unexpected request envelope: %#v", req)
	}
	if req.MaxOutputTokens != 256 || req.MaxTokens != 0 {
		t.Fatalf("unexpected token fields: %#v", req)
	}
}

func TestCodexStreamThinkingDelta(t *testing.T) {
	c := &streamCapture{}
	srv := openRouterServer(t, c, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"reasoning\":\"step one\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"thinking\":\"step two\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	})
	defer srv.Close()

	p := codexProvider(srv.URL, "sk-codex-test", false, "")
	msgs := []model.Message{{Role: model.RoleUser, Parts: []model.MessagePart{model.TextPart{Text: "think"}}}}
	ev := collectProviderEvents(t, p.Stream(context.Background(), msgs, nil))
	if len(ev) != 3 {
		t.Fatalf("expected 3 events, got %d", len(ev))
	}
	if ev[0].Type != EventThinkingDelta || ev[0].Delta != "step one" {
		t.Fatalf("unexpected first event: %#v", ev[0])
	}
	if ev[1].Type != EventThinkingDelta || ev[1].Delta != "step two" {
		t.Fatalf("unexpected second event: %#v", ev[1])
	}
	if ev[2].Type != EventComplete {
		t.Fatalf("unexpected completion event: %#v", ev[2])
	}
}

func TestCodexStreamDualToolCallID(t *testing.T) {
	c := &streamCapture{}
	srv := openRouterServer(t, c, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_abc|fc_xyz\",\"function\":{\"name\":\"read\",\"arguments\":\"{\"}}]}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"path\\\":\\\"/tmp/a\\\"}\"}}]}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"finish_reason\":\"tool_calls\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	})
	defer srv.Close()

	p := codexProvider(srv.URL, "sk-codex-tool", false, "")
	msgs := []model.Message{{Role: model.RoleUser, Parts: []model.MessagePart{model.TextPart{Text: "read file"}}}}
	ev := collectProviderEvents(t, p.Stream(context.Background(), msgs, nil))
	if len(ev) != 4 {
		t.Fatalf("expected 4 events, got %d", len(ev))
	}
	if ev[0].Type != EventToolUseStart || ev[0].ToolCallID != "call_abc" {
		t.Fatalf("unexpected tool start event: %#v", ev[0])
	}
	if ev[1].Type != EventToolUseDelta || ev[1].ToolCallID != "call_abc" {
		t.Fatalf("unexpected tool delta event: %#v", ev[1])
	}
	if ev[2].Type != EventToolUseStop || ev[2].ToolCallID != "call_abc" {
		t.Fatalf("unexpected tool stop event: %#v", ev[2])
	}
	if ev[3].Type != EventComplete {
		t.Fatalf("unexpected completion event: %#v", ev[3])
	}
}

func TestCodexRequestIncludesStore(t *testing.T) {
	c := &streamCapture{}
	srv := openRouterServer(t, c, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: [DONE]\n\n")
	})
	defer srv.Close()

	p := codexProvider(srv.URL, "sk-codex-test", true, "")
	msgs := []model.Message{{Role: model.RoleUser, Parts: []model.MessagePart{model.TextPart{Text: "ping"}}}}
	_ = collectProviderEvents(t, p.Stream(context.Background(), msgs, nil))
	req := c.firstRequest()
	if !req.Store {
		t.Fatalf("expected store=true in request, got %#v", req)
	}
}

func TestCodexRequestIncludesReasoningEffort(t *testing.T) {
	c := &streamCapture{}
	srv := openRouterServer(t, c, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: [DONE]\n\n")
	})
	defer srv.Close()

	p := codexProvider(srv.URL, "sk-codex-test", false, "medium")
	msgs := []model.Message{{Role: model.RoleUser, Parts: []model.MessagePart{model.TextPart{Text: "ping"}}}}
	_ = collectProviderEvents(t, p.Stream(context.Background(), msgs, nil))
	req := c.firstRequest()
	if req.Reasoning == nil || req.Reasoning.Effort != "medium" {
		t.Fatalf("expected reasoning effort in request, got %#v", req.Reasoning)
	}
}
