package agent

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/agusx1211/miclaw/model"
	"github.com/agusx1211/miclaw/provider"
	"github.com/agusx1211/miclaw/store"
	"github.com/agusx1211/miclaw/tooling"
)

type scriptedProvider struct {
	mu           sync.Mutex
	calls        int
	streams      []streamScript
	seenMessages [][]model.Message
	seenTools    [][]provider.ToolDef
	model        provider.ModelInfo
}

type streamScript func(context.Context, []model.Message, []provider.ToolDef) <-chan provider.ProviderEvent

func (p *scriptedProvider) Stream(ctx context.Context, msgs []model.Message, defs []provider.ToolDef) <-chan provider.ProviderEvent {
	p.mu.Lock()
	idx := p.calls
	p.calls++
	p.seenMessages = append(p.seenMessages, append([]model.Message(nil), msgs...))
	p.seenTools = append(p.seenTools, append([]provider.ToolDef(nil), defs...))
	script := p.streams[idx]
	p.mu.Unlock()
	return script(ctx, msgs, defs)
}

func (p *scriptedProvider) Model() provider.ModelInfo {
	return p.model
}

func (p *scriptedProvider) CallCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func eventStream(events ...provider.ProviderEvent) streamScript {
	return func(context.Context, []model.Message, []provider.ToolDef) <-chan provider.ProviderEvent {
		ch := make(chan provider.ProviderEvent, len(events))
		go func() {
			defer close(ch)
			for _, e := range events {
				ch <- e
			}
		}()
		return ch
	}
}

type echoTool struct {
	mu    sync.Mutex
	calls []model.ToolCallPart
	runFn func(context.Context)
}

func (t *echoTool) Name() string { return "echo" }

func (t *echoTool) Description() string { return "echo tool" }

func (t *echoTool) Parameters() tooling.JSONSchema {
	return tooling.JSONSchema{Type: "object"}
}

func (t *echoTool) Run(ctx context.Context, call model.ToolCallPart) (tooling.ToolResult, error) {
	if t.runFn != nil {
		t.runFn(ctx)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.calls = append(t.calls, call)
	return tooling.ToolResult{Content: "tool-ok", IsError: false}, nil
}

func (t *echoTool) Calls() []model.ToolCallPart {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]model.ToolCallPart(nil), t.calls...)
}

type sleepTool struct {
	mu    sync.Mutex
	calls []model.ToolCallPart
}

func (t *sleepTool) Name() string { return "sleep" }

func (t *sleepTool) Description() string { return "sleep tool" }

func (t *sleepTool) Parameters() tooling.JSONSchema {
	return tooling.JSONSchema{Type: "object"}
}

func (t *sleepTool) Run(_ context.Context, call model.ToolCallPart) (tooling.ToolResult, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.calls = append(t.calls, call)
	return tooling.ToolResult{Content: "sleeping", IsError: false}, nil
}

func (t *sleepTool) Calls() []model.ToolCallPart {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]model.ToolCallPart(nil), t.calls...)
}

type cancellationTool struct {
	started chan struct{}
}

func (t *cancellationTool) Name() string { return "slow" }

func (t *cancellationTool) Description() string { return "slow tool" }

func (t *cancellationTool) Parameters() tooling.JSONSchema {
	return tooling.JSONSchema{Type: "object"}
}

func (t *cancellationTool) Run(ctx context.Context, call model.ToolCallPart) (tooling.ToolResult, error) {
	if call.ID == "call1" {
		return tooling.ToolResult{Content: "first", IsError: false}, nil
	}
	if call.ID == "call2" {
		close(t.started)
		<-ctx.Done()
		return tooling.ToolResult{}, ctx.Err()
	}
	return tooling.ToolResult{Content: "third", IsError: false}, nil
}

func openAgentStore(t *testing.T) *store.SQLiteStore {
	t.Helper()
	p := filepath.Join(t.TempDir(), "agent.db")
	s, err := store.OpenSQLite(p)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Fatalf("close sqlite: %v", err)
		}
	})
	return s
}

func listMessages(t *testing.T, s *store.SQLiteStore) []*model.Message {
	t.Helper()
	msgs, err := s.Messages.List(100, 0)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	return msgs
}

func textPart(msg *model.Message) string {
	for _, part := range msg.Parts {
		if text, ok := part.(model.TextPart); ok {
			return text.Text
		}
	}
	return ""
}

func historyContains(messages []model.Message, target string) bool {
	for _, msg := range messages {
		if textPart(&msg) == target {
			return true
		}
	}
	return false
}

func TestRunStoresPrefixedInputAndAssistantReply(t *testing.T) {
	s := openAgentStore(t)
	st := &sleepTool{}
	p := &scriptedProvider{
		streams: []streamScript{
			eventStream(
				provider.ProviderEvent{Type: provider.EventContentDelta, Delta: "hel"},
				provider.ProviderEvent{Type: provider.EventContentDelta, Delta: "lo"},
				provider.ProviderEvent{Type: provider.EventComplete, Usage: &provider.UsageInfo{PromptTokens: 11, CompletionTokens: 7}},
			),
			eventStream(
				provider.ProviderEvent{Type: provider.EventToolUseStart, ToolCallID: "call-sleep", ToolName: "sleep"},
				provider.ProviderEvent{Type: provider.EventToolUseStop, ToolCallID: "call-sleep"},
				provider.ProviderEvent{Type: provider.EventComplete},
			),
		},
	}
	a := NewAgent(s.MessageStore(), []tooling.Tool{st}, p)

	err := a.RunOnce(context.Background(), Input{Source: "signal:dm:user-1", Content: "hi"})
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if p.CallCount() != 2 {
		t.Fatalf("expected 2 provider calls, got %d", p.CallCount())
	}

	msgs := listMessages(t, s)
	if len(msgs) != 4 {
		t.Fatalf("expected 4 stored messages, got %d", len(msgs))
	}
	if msgs[0].Role != model.RoleUser || textPart(msgs[0]) != "[signal:dm:user-1] hi" {
		t.Fatalf("unexpected first message: %#v", msgs[0])
	}
	if msgs[1].Role != model.RoleAssistant || textPart(msgs[1]) != "hello" {
		t.Fatalf("unexpected assistant message: %#v", msgs[1])
	}
	if msgs[2].Role != model.RoleAssistant {
		t.Fatalf("unexpected assistant message: %#v", msgs[2])
	}
	if msgs[3].Role != model.RoleTool {
		t.Fatalf("unexpected tool message: %#v", msgs[3])
	}
	if len(st.Calls()) != 1 {
		t.Fatalf("expected 1 sleep tool call, got %d", len(st.Calls()))
	}
}

func TestRunInjectsNewInputBetweenToolRounds(t *testing.T) {
	s := openAgentStore(t)
	p := &scriptedProvider{
		streams: []streamScript{
			eventStream(
				provider.ProviderEvent{Type: provider.EventToolUseStart, ToolCallID: "call1", ToolName: "echo"},
				provider.ProviderEvent{Type: provider.EventToolUseDelta, ToolCallID: "call1", Delta: `{"x":"1"}`},
				provider.ProviderEvent{Type: provider.EventToolUseStop, ToolCallID: "call1"},
				provider.ProviderEvent{Type: provider.EventComplete},
			),
			eventStream(
				provider.ProviderEvent{Type: provider.EventContentDelta, Delta: "done"},
				provider.ProviderEvent{Type: provider.EventToolUseStart, ToolCallID: "call2", ToolName: "sleep"},
				provider.ProviderEvent{Type: provider.EventToolUseStop, ToolCallID: "call2"},
				provider.ProviderEvent{Type: provider.EventComplete},
			),
		},
	}
	var a *Agent
	tool := &echoTool{runFn: func(context.Context) {
		a.Inject(Input{Source: "signal:dm:alice", Content: "stop"})
	}}
	st := &sleepTool{}
	a = NewAgent(s.MessageStore(), []tooling.Tool{tool, st}, p)

	err := a.RunOnce(context.Background(), Input{Source: "api", Content: "use tool"})
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if p.CallCount() != 2 {
		t.Fatalf("expected 2 provider calls, got %d", p.CallCount())
	}
	if len(tool.Calls()) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(tool.Calls()))
	}
	if !historyContains(p.seenMessages[1], "[signal:dm:alice] stop") {
		t.Fatalf("second model round did not include injected input: %#v", p.seenMessages[1])
	}

	msgs := listMessages(t, s)
	if len(msgs) != 6 {
		t.Fatalf("expected 6 stored messages, got %d", len(msgs))
	}
	if msgs[1].Role != model.RoleAssistant {
		t.Fatalf("expected assistant tool-call message at index 1, got role %q", msgs[1].Role)
	}
	if msgs[2].Role != model.RoleTool {
		t.Fatalf("expected tool result message at index 2, got role %q", msgs[2].Role)
	}
	if msgs[3].Role != model.RoleUser || textPart(msgs[3]) != "[signal:dm:alice] stop" {
		t.Fatalf("expected injected user message at index 3, got %#v", msgs[3])
	}
	if msgs[5].Role != model.RoleTool {
		t.Fatalf("expected tool result message at index 5, got role %q", msgs[5].Role)
	}
	if len(st.Calls()) != 1 {
		t.Fatalf("expected 1 sleep tool call, got %d", len(st.Calls()))
	}
}

func TestRunAutoSleepsAfterNoToolRoundLimit(t *testing.T) {
	s := openAgentStore(t)
	p := &scriptedProvider{
		streams: []streamScript{
			eventStream(
				provider.ProviderEvent{Type: provider.EventContentDelta, Delta: "one"},
				provider.ProviderEvent{Type: provider.EventComplete},
			),
			eventStream(
				provider.ProviderEvent{Type: provider.EventContentDelta, Delta: "two"},
				provider.ProviderEvent{Type: provider.EventComplete},
			),
			eventStream(
				provider.ProviderEvent{Type: provider.EventContentDelta, Delta: "three"},
				provider.ProviderEvent{Type: provider.EventComplete},
			),
		},
	}
	a := NewAgent(s.MessageStore(), nil, p)
	a.SetNoToolSleepRounds(2)

	err := a.RunOnce(context.Background(), Input{Source: "api", Content: "loop"})
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if p.CallCount() != 2 {
		t.Fatalf("expected 2 provider calls, got %d", p.CallCount())
	}
	msgs := listMessages(t, s)
	if len(msgs) != 3 {
		t.Fatalf("expected 3 stored messages, got %d", len(msgs))
	}
	if textPart(msgs[1]) != "one" || textPart(msgs[2]) != "two" {
		t.Fatalf("unexpected assistant messages: %#v", msgs)
	}
}

func TestRunOnceCancellation(t *testing.T) {
	s := openAgentStore(t)
	p := &scriptedProvider{streams: []streamScript{func(ctx context.Context, _ []model.Message, _ []provider.ToolDef) <-chan provider.ProviderEvent {
		ch := make(chan provider.ProviderEvent, 1)
		go func() {
			defer close(ch)
			<-ctx.Done()
			ch <- provider.ProviderEvent{Type: provider.EventError, Error: ctx.Err()}
		}()
		return ch
	}}}
	a := NewAgent(s.MessageStore(), nil, p)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	err := a.RunOnce(ctx, Input{Source: "api", Content: "stop"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
	if a.IsActive() {
		t.Fatal("agent should be inactive after cancellation")
	}
}

func TestRunToolsCancellationMarksRemaining(t *testing.T) {
	tool := &cancellationTool{started: make(chan struct{})}
	calls := []ToolCallPart{
		{ID: "call1", Name: "slow"},
		{ID: "call2", Name: "slow"},
		{ID: "call3", Name: "slow"},
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-tool.started
		cancel()
	}()
	msg, err := runTools(ctx, []tooling.Tool{tool}, calls)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
	if msg == nil {
		t.Fatal("expected non-nil tool message")
	}
	if len(msg.Parts) != 3 {
		t.Fatalf("expected 3 parts, got %d", len(msg.Parts))
	}
	p1 := msg.Parts[0].(model.ToolResultPart)
	if p1.ToolCallID != "call1" || p1.Content != "first" || p1.IsError {
		t.Fatalf("unexpected first part: %#v", p1)
	}
	p2 := msg.Parts[1].(model.ToolResultPart)
	if p2.ToolCallID != "call2" || p2.Content != "Cancelled" || !p2.IsError {
		t.Fatalf("unexpected second part: %#v", p2)
	}
	p3 := msg.Parts[2].(model.ToolResultPart)
	if p3.ToolCallID != "call3" || p3.Content != "Cancelled" || !p3.IsError {
		t.Fatalf("unexpected third part: %#v", p3)
	}
}
