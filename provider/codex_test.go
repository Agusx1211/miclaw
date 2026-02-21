package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestCodexStreamChatgptResponsesOAuth(t *testing.T) {
	claims := `{"https://api.openai.com/auth":{"chatgpt_account_id":"acc_123"}}`
	token := "x." + base64.RawURLEncoding.EncodeToString([]byte(claims)) + ".y"
	gotPath := ""
	gotAccount := ""
	gotOriginator := ""
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAccount = r.Header.Get("ChatGPT-Account-ID")
		gotOriginator = r.Header.Get("originator")
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if err := json.Unmarshal(b, &body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n")
	}))
	defer srv.Close()

	cfg := config.ProviderConfig{
		BaseURL:        srv.URL + "/backend-api/codex",
		APIKey:         token,
		Model:          "gpt-5.2-codex",
		MaxTokens:      256,
		ThinkingEffort: "none",
		Store:          true,
	}
	p := NewCodex(cfg)
	msgs := []model.Message{{Role: model.RoleUser, Parts: []model.MessagePart{model.TextPart{Text: "hello"}}}}
	ev := collectProviderEvents(t, p.Stream(context.Background(), msgs, nil))
	if len(ev) != 2 {
		t.Fatalf("expected 2 events, got %d", len(ev))
	}
	if ev[0].Type != EventContentDelta || ev[0].Delta != "ok" {
		t.Fatalf("unexpected first event: %#v", ev[0])
	}
	if ev[1].Type != EventComplete {
		t.Fatalf("unexpected second event: %#v", ev[1])
	}
	if gotPath != "/backend-api/codex/responses" {
		t.Fatalf("unexpected path: %q", gotPath)
	}
	if gotAccount != "acc_123" {
		t.Fatalf("unexpected ChatGPT-Account-ID header: %q", gotAccount)
	}
	if gotOriginator != "codex_cli_rs" {
		t.Fatalf("unexpected originator header: %q", gotOriginator)
	}
	if body["tool_choice"] != "auto" || body["stream"] != true {
		t.Fatalf("unexpected request body: %#v", body)
	}
	if body["store"] != false {
		t.Fatalf("codex responses requires store=false: %#v", body["store"])
	}
	if _, ok := body["max_output_tokens"]; ok {
		t.Fatalf("max_output_tokens must be omitted: %#v", body)
	}
	if _, ok := body["reasoning"]; ok {
		t.Fatalf("reasoning must be omitted for effort none: %#v", body["reasoning"])
	}
	if strings.TrimSpace(fmt.Sprint(body["instructions"])) == "" {
		t.Fatalf("instructions must be non-empty: %#v", body["instructions"])
	}
	if !strings.Contains(fmt.Sprint(body["model"]), "gpt-5.2-codex") {
		t.Fatalf("unexpected model in request: %#v", body)
	}
}

func TestCodexStreamStatusErrorPrefix(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, "bad request")
	}))
	defer srv.Close()

	p := codexProvider(srv.URL, "sk-codex-test", false, "")
	msgs := []model.Message{{Role: model.RoleUser, Parts: []model.MessagePart{model.TextPart{Text: "hello"}}}}
	ev := collectProviderEvents(t, p.Stream(context.Background(), msgs, nil))
	if len(ev) != 1 || ev[0].Type != EventError || ev[0].Error == nil {
		t.Fatalf("unexpected events: %#v", ev)
	}
	if !strings.Contains(ev[0].Error.Error(), "codex stream failed") {
		t.Fatalf("unexpected error: %v", ev[0].Error)
	}
}
