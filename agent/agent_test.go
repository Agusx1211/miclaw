package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/agusx1211/miclaw/model"
	"github.com/agusx1211/miclaw/provider"
	"github.com/agusx1211/miclaw/tooling"
)

type memMessageStore struct {
	mu   sync.Mutex
	msgs []*model.Message
}

func (s *memMessageStore) Create(msg *model.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *msg
	s.msgs = append(s.msgs, &cp)
	return nil
}

func (s *memMessageStore) Get(id string) (*model.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, msg := range s.msgs {
		if msg.ID == id {
			cp := *msg
			return &cp, nil
		}
	}
	return nil, errors.New("not found")
}

func (s *memMessageStore) List(limit, offset int) ([]*model.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if offset > len(s.msgs) {
		return []*model.Message{}, nil
	}
	end := offset + limit
	if end > len(s.msgs) {
		end = len(s.msgs)
	}
	out := make([]*model.Message, 0, end-offset)
	for _, msg := range s.msgs[offset:end] {
		cp := *msg
		out = append(out, &cp)
	}
	return out, nil
}

func (s *memMessageStore) DeleteAll() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msgs = nil
	return nil
}

func (s *memMessageStore) Count() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.msgs), nil
}

func (s *memMessageStore) ReplaceAll(msgs []*model.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msgs = s.msgs[:0]
	for _, msg := range msgs {
		cp := *msg
		s.msgs = append(s.msgs, &cp)
	}
	return nil
}

type idleProvider struct{}

func (idleProvider) Stream(context.Context, []model.Message, []provider.ToolDef) <-chan provider.ProviderEvent {
	ch := make(chan provider.ProviderEvent, 4)
	ch <- provider.ProviderEvent{Type: provider.EventContentDelta, Delta: "ok"}
	ch <- provider.ProviderEvent{Type: provider.EventToolUseStart, ToolCallID: "sleep-1", ToolName: "sleep"}
	ch <- provider.ProviderEvent{Type: provider.EventToolUseStop, ToolCallID: "sleep-1"}
	ch <- provider.ProviderEvent{Type: provider.EventComplete}
	close(ch)
	return ch
}

func (idleProvider) Model() provider.ModelInfo {
	return provider.ModelInfo{ID: "stub", Name: "stub-model"}
}

type blockingProvider struct {
	started chan struct{}
}

func (p blockingProvider) Stream(ctx context.Context, _ []model.Message, _ []provider.ToolDef) <-chan provider.ProviderEvent {
	ch := make(chan provider.ProviderEvent, 1)
	go func() {
		defer close(ch)
		close(p.started)
		<-ctx.Done()
		ch <- provider.ProviderEvent{Type: provider.EventError, Error: ctx.Err()}
	}()
	return ch
}

func (blockingProvider) Model() provider.ModelInfo {
	return provider.ModelInfo{ID: "stub", Name: "stub-model"}
}

type stubTool struct{}

func (stubTool) Name() string { return "stub" }

func (stubTool) Description() string { return "stub tool" }

func (stubTool) Parameters() tooling.JSONSchema { return tooling.JSONSchema{Type: "object"} }

func (stubTool) Run(context.Context, model.ToolCallPart) (tooling.ToolResult, error) {
	return tooling.ToolResult{Content: "ok", IsError: false}, nil
}

type sleepStubTool struct{}

func (sleepStubTool) Name() string { return "sleep" }

func (sleepStubTool) Description() string { return "sleep tool" }

func (sleepStubTool) Parameters() tooling.JSONSchema { return tooling.JSONSchema{Type: "object"} }

func (sleepStubTool) Run(context.Context, model.ToolCallPart) (tooling.ToolResult, error) {
	return tooling.ToolResult{Content: "sleeping", IsError: false}, nil
}

func newTestAgent(t *testing.T) (*Agent, *memMessageStore) {
	t.Helper()
	store := &memMessageStore{}
	a := NewAgent(
		store,
		[]tooling.Tool{stubTool{}, sleepStubTool{}},
		idleProvider{},
	)
	if a == nil {
		t.Fatal("expected non-nil agent")
	}
	return a, store
}

func TestNewAgentInitialization(t *testing.T) {
	a, _ := newTestAgent(t)
	if a.IsActive() {
		t.Fatal("new agent should be inactive")
	}
	if a.pending == nil {
		t.Fatal("new agent queue must not be nil")
	}
	if a.pending.Len() != 0 {
		t.Fatalf("new agent queue must be empty, got %d", a.pending.Len())
	}
	if a.Events() == nil {
		t.Fatal("new agent events broker must not be nil")
	}
}

func TestAgentCancel(t *testing.T) {
	store := &memMessageStore{}
	started := make(chan struct{})
	a := NewAgent(store, nil, blockingProvider{started: started})
	a.Inject(Input{Source: "api", Content: "hello"})
	<-started
	if !a.IsActive() {
		t.Fatal("agent should be active after inject")
	}
	a.Cancel()
	deadline := time.Now().Add(time.Second)
	for a.IsActive() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if a.IsActive() {
		t.Fatal("agent should be inactive after cancel")
	}
}

func TestAgentEventsSubscription(t *testing.T) {
	a, _ := newTestAgent(t)
	ch, unsub := a.Events().Subscribe()
	defer unsub()

	want := AgentEvent{Type: EventCompact}
	a.Events().Publish(want)

	select {
	case got := <-ch:
		if got.Type != EventCompact {
			t.Fatalf("unexpected event type: %q", got.Type)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for event")
	}
}

func TestAgentInjectProcessesQueuedInputs(t *testing.T) {
	a, store := newTestAgent(t)
	a.Inject(Input{Source: "api", Content: "one"})
	a.Inject(Input{Source: "api", Content: "two"})
	deadline := time.Now().Add(time.Second)
	for a.IsActive() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if a.IsActive() {
		t.Fatal("expected inactive agent")
	}
	n, err := store.Count()
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 4 {
		t.Fatalf("expected 4 messages (2 user + 1 assistant + 1 tool), got %d", n)
	}
}
