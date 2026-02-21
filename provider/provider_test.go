package provider

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func collectEvents(t *testing.T, s string) []ProviderEvent {
	t.Helper()
	c := parseSSEStream(io.NopCloser(strings.NewReader(s)))
	e := make([]ProviderEvent, 0, 8)
	for v := range c {
		e = append(e, v)
	}
	return e
}

func response(status int, retryAfter string) *http.Response {
	h := make(http.Header)
	if retryAfter != "" {
		h.Set("Retry-After", retryAfter)
	}
	return &http.Response{
		StatusCode: status,
		Header:     h,
		Body:       io.NopCloser(strings.NewReader("x")),
	}
}

func TestParseSSEContentDeltas(t *testing.T) {
	s := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"Hel"}}]}`,
		`data: {"choices":[{"delta":{"content":"lo"}}]}`,
		`data: {"choices":[{"finish_reason":"stop"}]}`,
		`data: [DONE]`,
		"",
	}, "\n")
	e := collectEvents(t, s)
	if len(e) != 3 {
		t.Fatalf("expected 3 events, got %d", len(e))
	}
	if e[0].Type != EventContentDelta || e[0].Delta != "Hel" {
		t.Fatalf("unexpected first event: %#v", e[0])
	}
	if e[1].Type != EventContentDelta || e[1].Delta != "lo" {
		t.Fatalf("unexpected second event: %#v", e[1])
	}
	if e[2].Type != EventComplete {
		t.Fatalf("unexpected third event: %#v", e[2])
	}
}

func TestParseSSEToolCallStream(t *testing.T) {
	s := strings.Join([]string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"read","arguments":"{"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"path\":\"/tmp/a\"}"}}]}}]}`,
		`data: {"choices":[{"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
		"",
	}, "\n")
	e := collectEvents(t, s)
	if len(e) != 4 {
		t.Fatalf("expected 4 events, got %d", len(e))
	}
	if e[0].Type != EventToolUseStart || e[0].ToolCallID != "call_1" || e[0].ToolName != "read" {
		t.Fatalf("unexpected start event: %#v", e[0])
	}
	if e[1].Type != EventToolUseDelta || e[1].ToolCallID != "call_1" || e[1].Delta != `"path":"/tmp/a"}` {
		t.Fatalf("unexpected delta event: %#v", e[1])
	}
	if e[2].Type != EventToolUseStop || e[2].ToolCallID != "call_1" {
		t.Fatalf("unexpected stop event: %#v", e[2])
	}
	if e[3].Type != EventComplete {
		t.Fatalf("unexpected complete event: %#v", e[3])
	}
}

func TestParseSSEDoneTermination(t *testing.T) {
	e := collectEvents(t, "data: [DONE]\n\n")
	if len(e) != 0 {
		t.Fatalf("expected no events, got %d", len(e))
	}
}

func TestParseSSEMalformedLines(t *testing.T) {
	s := strings.Join([]string{
		"event: message",
		"junk",
		"data: invalid-json",
		`data: {"choices":[{"delta":{"content":"ok"}}]}`,
		`data: [DONE]`,
		"",
	}, "\n")
	e := collectEvents(t, s)
	if len(e) != 1 {
		t.Fatalf("expected 1 event, got %d", len(e))
	}
	if e[0].Type != EventContentDelta || e[0].Delta != "ok" {
		t.Fatalf("unexpected event: %#v", e[0])
	}
}

func TestParseSSEThinkingDelta(t *testing.T) {
	s := strings.Join([]string{
		`data: {"choices":[{"delta":{"thinking":"step one"}}]}`,
		`data: {"choices":[{"delta":{"reasoning":"step two"}}]}`,
		`data: [DONE]`,
		"",
	}, "\n")
	e := collectEvents(t, s)
	if len(e) != 2 {
		t.Fatalf("expected 2 events, got %d", len(e))
	}
	if e[0].Type != EventThinkingDelta || e[0].Delta != "step one" {
		t.Fatalf("unexpected first event: %#v", e[0])
	}
	if e[1].Type != EventThinkingDelta || e[1].Delta != "step two" {
		t.Fatalf("unexpected second event: %#v", e[1])
	}
}

func TestParseSSEWithUsage(t *testing.T) {
	s := strings.Join([]string{
		`data: {"choices":[{"finish_reason":"stop"}],"usage":{"prompt_tokens":1234,"completion_tokens":567,"cache_read_tokens":12,"cache_write_tokens":34}}`,
		`data: [DONE]`,
		"",
	}, "\n")
	e := collectEvents(t, s)
	if len(e) != 1 {
		t.Fatalf("expected 1 event, got %d", len(e))
	}
	if e[0].Type != EventComplete {
		t.Fatalf("unexpected event type: %#v", e[0])
	}
	if e[0].Usage == nil {
		t.Fatal("expected usage info on complete event")
	}
	if e[0].Usage.PromptTokens != 1234 || e[0].Usage.CompletionTokens != 567 {
		t.Fatalf("unexpected usage tokens: %#v", e[0].Usage)
	}
	if e[0].Usage.CacheReadTokens != 12 || e[0].Usage.CacheWriteTokens != 34 {
		t.Fatalf("unexpected cache usage: %#v", e[0].Usage)
	}
}

func TestParseSSEMultipleToolCalls(t *testing.T) {
	s := strings.Join([]string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","function":{"name":"read"}},{"index":1,"id":"call_b","function":{"name":"write"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":1,"function":{"arguments":"{\"path\":\"/tmp/file\"}"}}]}}]}`,
		`data: {"choices":[{"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
		"",
	}, "\n")
	e := collectEvents(t, s)
	if len(e) != 6 {
		t.Fatalf("expected 6 events, got %d", len(e))
	}
	if e[0].Type != EventToolUseStart || e[0].ToolCallID != "call_a" {
		t.Fatalf("unexpected first event: %#v", e[0])
	}
	if e[1].Type != EventToolUseStart || e[1].ToolCallID != "call_b" {
		t.Fatalf("unexpected second event: %#v", e[1])
	}
	if e[2].Type != EventToolUseDelta || e[2].ToolCallID != "call_b" {
		t.Fatalf("unexpected delta event: %#v", e[2])
	}
	if e[3].Type != EventToolUseStop || e[3].ToolCallID != "call_a" {
		t.Fatalf("unexpected third stop event: %#v", e[3])
	}
	if e[4].Type != EventToolUseStop || e[4].ToolCallID != "call_b" {
		t.Fatalf("unexpected fourth stop event: %#v", e[4])
	}
	if e[5].Type != EventComplete {
		t.Fatalf("unexpected complete event: %#v", e[5])
	}
}

func TestRetryOn429(t *testing.T) {
	n := 0
	ctx := context.Background()
	r, err := withRetry(ctx, 8, func() (*http.Response, error) {
		n++
		if n < 3 {
			return response(429, "0"), nil
		}
		return response(200, ""), nil
	})
	if err != nil {
		t.Fatalf("withRetry returned error: %v", err)
	}
	if r.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", r.StatusCode)
	}
	if n != 3 {
		t.Fatalf("expected 3 attempts, got %d", n)
	}
}

func TestRetryPassesThroughNonRetriable(t *testing.T) {
	n := 0
	ctx := context.Background()
	r, err := withRetry(ctx, 8, func() (*http.Response, error) {
		n++
		return response(500, ""), nil
	})
	if err != nil {
		t.Fatalf("withRetry returned error: %v", err)
	}
	if r.StatusCode != 500 {
		t.Fatalf("expected 500, got %d", r.StatusCode)
	}
	if n != 1 {
		t.Fatalf("expected 1 attempt, got %d", n)
	}
}

func TestRetryRespectsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n := 0
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	_, err := withRetry(ctx, 8, func() (*http.Response, error) {
		n++
		return response(429, "5"), nil
	})
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	if n != 1 {
		t.Fatalf("expected 1 attempt before cancellation, got %d", n)
	}
}
