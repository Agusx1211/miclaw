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
}

func (t *echoTool) Name() string { return "echo" }

func (t *echoTool) Description() string { return "echo tool" }

func (t *echoTool) Parameters() tooling.JSONSchema {
	return tooling.JSONSchema{Type: "object"}
}

func (t *echoTool) Run(_ context.Context, call model.ToolCallPart) (tooling.ToolResult, error) {
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

func waitEvent(t *testing.T, ch <-chan AgentEvent) AgentEvent {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
		return AgentEvent{}
	}
}

func waitInactive(t *testing.T, a *Agent) {
	t.Helper()
	deadline := time.After(time.Second)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for a.IsActive() {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for inactive agent")
		case <-tick.C:
		}
	}
}

func listSessionMessages(t *testing.T, s *store.SQLiteStore, sessionID string) []*model.Message {
	t.Helper()
	msgs, err := s.Messages.ListBySession(sessionID, 100, 0)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	return msgs
}

func TestProcessGenerationSimpleResponse(t *testing.T) {
	s := openAgentStore(t)
	p := &scriptedProvider{
		model: provider.ModelInfo{CostPerInputToken: 0.1, CostPerOutputToken: 0.2},
		streams: []streamScript{eventStream(
			provider.ProviderEvent{Type: provider.EventContentDelta, Delta: "hel"},
			provider.ProviderEvent{Type: provider.EventContentDelta, Delta: "lo"},
			provider.ProviderEvent{Type: provider.EventComplete, Usage: &provider.UsageInfo{PromptTokens: 11, CompletionTokens: 7}},
		)},
	}
	a := NewAgent(s.SessionStore(), s.MessageStore(), nil, p)
	evCh, unsub := a.Events().Subscribe()
	defer unsub()

	err := a.processGeneration(context.Background(), Input{SessionID: "s1", Content: "hi", Source: SourceAPI})
	if err != nil {
		t.Fatalf("process generation: %v", err)
	}

	ev := waitEvent(t, evCh)
	if ev.Type != EventResponse || ev.SessionID != "s1" {
		t.Fatalf("unexpected event: %#v", ev)
	}
	if len(ev.Message.Parts) != 1 {
		t.Fatalf("unexpected event message parts: %#v", ev.Message.Parts)
	}
	text, ok := ev.Message.Parts[0].(model.TextPart)
	if !ok || text.Text != "hello" {
		t.Fatalf("unexpected assistant text: %#v", ev.Message.Parts[0])
	}

	msgs := listSessionMessages(t, s, "s1")
	if len(msgs) != 2 {
		t.Fatalf("expected 2 stored messages, got %d", len(msgs))
	}
	if msgs[0].Role != model.RoleUser || msgs[1].Role != model.RoleAssistant {
		t.Fatalf("unexpected roles: %q %q", msgs[0].Role, msgs[1].Role)
	}
}

func TestProcessGenerationWithToolCalls(t *testing.T) {
	s := openAgentStore(t)
	tool := &echoTool{}
	p := &scriptedProvider{
		streams: []streamScript{
			eventStream(
				provider.ProviderEvent{Type: provider.EventToolUseStart, ToolCallID: "call1", ToolName: "echo"},
				provider.ProviderEvent{Type: provider.EventToolUseDelta, ToolCallID: "call1", Delta: `{"x":"1"}`},
				provider.ProviderEvent{Type: provider.EventToolUseStop, ToolCallID: "call1"},
				provider.ProviderEvent{Type: provider.EventComplete, Usage: &provider.UsageInfo{PromptTokens: 5, CompletionTokens: 3}},
			),
			eventStream(
				provider.ProviderEvent{Type: provider.EventContentDelta, Delta: "done"},
				provider.ProviderEvent{Type: provider.EventComplete, Usage: &provider.UsageInfo{PromptTokens: 8, CompletionTokens: 4}},
			),
		},
	}
	a := NewAgent(s.SessionStore(), s.MessageStore(), []tooling.Tool{tool}, p)
	evCh, unsub := a.Events().Subscribe()
	defer unsub()

	err := a.processGeneration(context.Background(), Input{SessionID: "s1", Content: "use tool", Source: SourceAPI})
	if err != nil {
		t.Fatalf("process generation: %v", err)
	}
	if p.CallCount() != 2 {
		t.Fatalf("expected 2 provider calls, got %d", p.CallCount())
	}
	if len(tool.Calls()) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(tool.Calls()))
	}

	ev := waitEvent(t, evCh)
	if ev.Type != EventResponse {
		t.Fatalf("unexpected event: %#v", ev)
	}
	finalText := ev.Message.Parts[0].(model.TextPart)
	if finalText.Text != "done" {
		t.Fatalf("unexpected final response: %#v", ev.Message.Parts[0])
	}

	msgs := listSessionMessages(t, s, "s1")
	if len(msgs) != 4 {
		t.Fatalf("expected 4 stored messages, got %d", len(msgs))
	}
	if msgs[2].Role != model.RoleTool {
		t.Fatalf("expected tool message at index 2, got role %q", msgs[2].Role)
	}
	part := msgs[2].Parts[0].(model.ToolResultPart)
	if part.ToolCallID != "call1" || part.Content != "tool-ok" || part.IsError {
		t.Fatalf("unexpected tool result part: %#v", part)
	}
}

func TestProcessGenerationCancellation(t *testing.T) {
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
	a := NewAgent(s.SessionStore(), s.MessageStore(), nil, p)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	err := a.processGeneration(ctx, Input{SessionID: "s1", Content: "stop", Source: SourceAPI})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
	if a.IsActive() {
		t.Fatal("agent should be inactive after cancellation")
	}
}

func TestProcessGenerationCreatesSession(t *testing.T) {
	s := openAgentStore(t)
	p := &scriptedProvider{streams: []streamScript{eventStream(
		provider.ProviderEvent{Type: provider.EventContentDelta, Delta: "ok"},
		provider.ProviderEvent{Type: provider.EventComplete},
	)}}
	a := NewAgent(s.SessionStore(), s.MessageStore(), nil, p)

	err := a.processGeneration(context.Background(), Input{Content: "new", Source: SourceWebhook})
	if err != nil {
		t.Fatalf("process generation: %v", err)
	}
	sessions, err := s.Sessions.List(10, 0)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected one session, got %d", len(sessions))
	}
	if sessions[0].ID == "" {
		t.Fatal("expected generated session id")
	}
}

func TestStreamAndHandleAccumulatesContent(t *testing.T) {
	s := openAgentStore(t)
	now := time.Date(2026, 2, 21, 0, 0, 0, 0, time.UTC)
	session := &model.Session{ID: "s1", CreatedAt: now, UpdatedAt: now}
	if err := s.Sessions.Create(session); err != nil {
		t.Fatalf("create session: %v", err)
	}
	p := &scriptedProvider{streams: []streamScript{eventStream(
		provider.ProviderEvent{Type: provider.EventContentDelta, Delta: "a"},
		provider.ProviderEvent{Type: provider.EventContentDelta, Delta: "b"},
		provider.ProviderEvent{Type: provider.EventContentDelta, Delta: "c"},
		provider.ProviderEvent{Type: provider.EventComplete},
	)}}
	a := NewAgent(s.SessionStore(), s.MessageStore(), nil, p)

	msgs := []*model.Message{{
		ID:        "u1",
		SessionID: "s1",
		Role:      model.RoleUser,
		Parts:     []model.MessagePart{model.TextPart{Text: "hello"}},
		CreatedAt: now,
	}}
	hasToolCalls, err := a.streamAndHandle(context.Background(), session, msgs, nil)
	if err != nil {
		t.Fatalf("stream and handle: %v", err)
	}
	if hasToolCalls {
		t.Fatal("expected no tool calls")
	}

	stored := listSessionMessages(t, s, "s1")
	if len(stored) != 1 {
		t.Fatalf("expected one assistant message, got %d", len(stored))
	}
	if len(stored[0].Parts) != 1 {
		t.Fatalf("expected one message part, got %d", len(stored[0].Parts))
	}
	text := stored[0].Parts[0].(model.TextPart)
	if text.Text != "abc" {
		t.Fatalf("unexpected merged text: %#v", text)
	}
}

func TestEnqueueProcessesAllInputsFIFO(t *testing.T) {
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
	a := NewAgent(s.SessionStore(), s.MessageStore(), nil, p)
	evCh, unsub := a.Events().Subscribe()
	defer unsub()
	inputs := []Input{
		{SessionID: "s1", Content: "first", Source: SourceAPI},
		{SessionID: "s2", Content: "second", Source: SourceAPI},
		{SessionID: "s3", Content: "third", Source: SourceAPI},
	}
	for _, in := range inputs {
		a.Enqueue(in)
	}
	for i, sessionID := range []string{"s1", "s2", "s3"} {
		ev := waitEvent(t, evCh)
		if ev.Type != EventResponse {
			t.Fatalf("event %d type = %q", i, ev.Type)
		}
		if ev.SessionID != sessionID {
			t.Fatalf("event %d session = %q", i, ev.SessionID)
		}
	}
	if p.CallCount() != 3 {
		t.Fatalf("expected 3 provider calls, got %d", p.CallCount())
	}
	waitInactive(t, a)
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
	msg, err := runTools(ctx, "s1", []tooling.Tool{tool}, calls)
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
