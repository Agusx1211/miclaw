package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agusx1211/miclaw/config"
	"github.com/agusx1211/miclaw/model"
)

type chatRequest struct {
	Model           string           `json:"model"`
	Messages        []chatMessageReq `json:"messages"`
	Tools           []chatToolReq    `json:"tools"`
	Stream          bool             `json:"stream"`
	MaxTokens       int              `json:"max_tokens"`
	MaxOutputTokens int              `json:"max_output_tokens"`
	Store           bool             `json:"store"`
	Reasoning       *chatReasoning   `json:"reasoning"`
}

type chatReasoning struct {
	Effort string `json:"effort"`
}

type chatMessageReq struct {
	Role       string            `json:"role"`
	Content    string            `json:"content"`
	ToolCalls  []chatToolCallReq `json:"tool_calls"`
	ToolCallID string            `json:"tool_call_id"`
}

type chatToolCallReq struct {
	ID       string              `json:"id"`
	Type     string              `json:"type"`
	Function chatFunctionCallReq `json:"function"`
}

type chatFunctionCallReq struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatToolReq struct {
	Type     string              `json:"type"`
	Function chatToolFunctionReq `json:"function"`
}

type chatToolFunctionReq struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type streamCapture struct {
	mu       sync.Mutex
	requests []chatRequest
	headers  []http.Header
	attempts int
}

func (c *streamCapture) push(r *http.Request, body chatRequest) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.attempts++
	h := make(http.Header)
	for k, v := range r.Header {
		h[k] = append([]string(nil), v...)
	}
	c.headers = append(c.headers, h)
	c.requests = append(c.requests, body)
}

func (c *streamCapture) firstRequest() chatRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.requests) == 0 {
		return chatRequest{}
	}
	return c.requests[0]
}

func (c *streamCapture) firstHeader(name string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.headers) == 0 {
		return ""
	}
	return c.headers[0].Get(name)
}

func (c *streamCapture) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.attempts
}

func openRouterServer(t *testing.T, capture *streamCapture, fn func(http.ResponseWriter, *http.Request)) *httptest.Server {
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

func collectProviderEvents(t *testing.T, ch <-chan ProviderEvent) []ProviderEvent {
	t.Helper()
	v := make([]ProviderEvent, 0, 8)
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				return v
			}
			v = append(v, e)
		case <-timer.C:
			t.Fatal("timed out waiting for provider events")
		}
	}
}

func openRouterProvider(baseURL, apiKey string) *OpenRouter {
	cfg := config.ProviderConfig{
		BaseURL:   baseURL,
		APIKey:    apiKey,
		Model:     "anthropic/claude-sonnet-4-5",
		MaxTokens: 128,
	}
	return NewOpenRouter(cfg)
}

func TestOpenRouterStreamSuccessContent(t *testing.T) {
	c := &streamCapture{}
	srv := openRouterServer(t, c, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hel\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"lo\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	})
	defer srv.Close()

	p := openRouterProvider(srv.URL, "sk-or-test")
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
	if req.Model != "anthropic/claude-sonnet-4-5" || !req.Stream || req.MaxTokens != 128 {
		t.Fatalf("unexpected request envelope: %#v", req)
	}
	if len(req.Messages) != 1 || req.Messages[0].Role != "user" || req.Messages[0].Content != "hello" {
		t.Fatalf("unexpected request messages: %#v", req.Messages)
	}
}

func TestOpenRouterStreamAttributionHeaders(t *testing.T) {
	c := &streamCapture{}
	srv := openRouterServer(t, c, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: [DONE]\n\n")
	})
	defer srv.Close()

	p := openRouterProvider(srv.URL, "sk-or-test")
	msgs := []model.Message{{Role: model.RoleUser, Parts: []model.MessagePart{model.TextPart{Text: "ping"}}}}
	_ = collectProviderEvents(t, p.Stream(context.Background(), msgs, nil))
	if got := c.firstHeader("HTTP-Referer"); got != "https://github.com/agusx1211/miclaw" {
		t.Fatalf("unexpected HTTP-Referer header: %q", got)
	}
	if got := c.firstHeader("X-Title"); got != "miclaw" {
		t.Fatalf("unexpected X-Title header: %q", got)
	}
}

func TestOpenRouterStreamAuthorizationHeader(t *testing.T) {
	c := &streamCapture{}
	srv := openRouterServer(t, c, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: [DONE]\n\n")
	})
	defer srv.Close()

	p := openRouterProvider(srv.URL, "sk-or-abc123")
	msgs := []model.Message{{Role: model.RoleUser, Parts: []model.MessagePart{model.TextPart{Text: "ping"}}}}
	_ = collectProviderEvents(t, p.Stream(context.Background(), msgs, nil))
	if got := c.firstHeader("Authorization"); got != "Bearer sk-or-abc123" {
		t.Fatalf("unexpected Authorization header: %q", got)
	}
}

func TestOpenRouterStreamToolCall(t *testing.T) {
	c := &streamCapture{}
	srv := openRouterServer(t, c, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":\"read\",\"arguments\":\"{\"}}]}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"path\\\":\\\"/tmp/a\\\"}\"}}]}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"finish_reason\":\"tool_calls\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	})
	defer srv.Close()

	p := openRouterProvider(srv.URL, "sk-or-tool")
	msgs := []model.Message{
		{Role: model.RoleUser, Parts: []model.MessagePart{model.TextPart{Text: "read file"}}},
		{Role: model.RoleAssistant, Parts: []model.MessagePart{
			model.ToolCallPart{ID: "call_1", Name: "read", Parameters: json.RawMessage(`{"path":"/tmp/a"}`)},
		}},
		{Role: model.RoleTool, Parts: []model.MessagePart{
			model.ToolResultPart{ToolCallID: "call_1", Content: "ok"},
		}},
	}
	tools := []ToolDef{{
		Name:        "read",
		Description: "read a file",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
	}}
	ev := collectProviderEvents(t, p.Stream(context.Background(), msgs, tools))
	if len(ev) != 4 {
		t.Fatalf("expected 4 events, got %d", len(ev))
	}
	if ev[0].Type != EventToolUseStart || ev[0].ToolCallID != "call_1" || ev[0].ToolName != "read" {
		t.Fatalf("unexpected tool start event: %#v", ev[0])
	}
	if ev[1].Type != EventToolUseDelta || ev[1].ToolCallID != "call_1" || ev[1].Delta != `"path":"/tmp/a"}` {
		t.Fatalf("unexpected tool delta event: %#v", ev[1])
	}
	if ev[2].Type != EventToolUseStop || ev[2].ToolCallID != "call_1" {
		t.Fatalf("unexpected tool stop event: %#v", ev[2])
	}
	if ev[3].Type != EventComplete {
		t.Fatalf("unexpected completion event: %#v", ev[3])
	}
	req := c.firstRequest()
	if len(req.Tools) != 1 || req.Tools[0].Type != "function" || req.Tools[0].Function.Name != "read" {
		t.Fatalf("unexpected tools payload: %#v", req.Tools)
	}
	if len(req.Messages) != 3 {
		t.Fatalf("expected 3 request messages, got %d", len(req.Messages))
	}
	if len(req.Messages[1].ToolCalls) != 1 || req.Messages[1].ToolCalls[0].ID != "call_1" {
		t.Fatalf("missing assistant tool call in payload: %#v", req.Messages[1])
	}
	if req.Messages[2].Role != "tool" || req.Messages[2].ToolCallID != "call_1" || req.Messages[2].Content != "ok" {
		t.Fatalf("missing tool result payload: %#v", req.Messages[2])
	}
}

func TestOpenRouterStreamErrorResponses(t *testing.T) {
	cases := []struct {
		name         string
		status       int
		retryAfter   string
		wantAttempts int
	}{
		{name: "unauthorized", status: 401, wantAttempts: 1},
		{name: "rate-limited", status: 429, retryAfter: "0", wantAttempts: 9},
		{name: "internal", status: 500, wantAttempts: 1},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			c := &streamCapture{}
			srv := openRouterServer(t, c, func(w http.ResponseWriter, _ *http.Request) {
				if tc.retryAfter != "" {
					w.Header().Set("Retry-After", tc.retryAfter)
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, "request failed")
			})
			defer srv.Close()

			p := openRouterProvider(srv.URL, "sk-or-test")
			msgs := []model.Message{{Role: model.RoleUser, Parts: []model.MessagePart{model.TextPart{Text: "ping"}}}}
			ev := collectProviderEvents(t, p.Stream(context.Background(), msgs, nil))
			if len(ev) != 1 || ev[0].Type != EventError || ev[0].Error == nil {
				t.Fatalf("unexpected events: %#v", ev)
			}
			if !strings.Contains(ev[0].Error.Error(), fmt.Sprintf("status %d", tc.status)) {
				t.Fatalf("unexpected error message: %v", ev[0].Error)
			}
			if c.count() != tc.wantAttempts {
				t.Fatalf("expected %d attempts, got %d", tc.wantAttempts, c.count())
			}
		})
	}
}
