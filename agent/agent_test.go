package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/agusx1211/miclaw/model"
	"github.com/agusx1211/miclaw/provider"
	"github.com/agusx1211/miclaw/tooling"
)

type stubSessionStore struct{}

func (stubSessionStore) Create(*model.Session) error             { return nil }
func (stubSessionStore) Get(string) (*model.Session, error)      { return nil, errors.New("not found") }
func (stubSessionStore) Update(*model.Session) error             { return nil }
func (stubSessionStore) List(int, int) ([]*model.Session, error) { return []*model.Session{}, nil }
func (stubSessionStore) Delete(string) error                     { return nil }

type stubMessageStore struct{}

func (stubMessageStore) Create(*model.Message) error        { return nil }
func (stubMessageStore) Get(string) (*model.Message, error) { return nil, errors.New("not found") }
func (stubMessageStore) ListBySession(string, int, int) ([]*model.Message, error) {
	return []*model.Message{}, nil
}
func (stubMessageStore) DeleteBySession(string) error                          { return nil }
func (stubMessageStore) CountBySession(string) (int, error)                    { return 0, nil }
func (stubMessageStore) ReplaceSessionMessages(string, []*model.Message) error { return nil }

type stubProvider struct{}

func (stubProvider) Stream(context.Context, []model.Message, []provider.ToolDef) <-chan provider.ProviderEvent {
	ch := make(chan provider.ProviderEvent)
	close(ch)
	return ch
}

func (stubProvider) Model() provider.ModelInfo {
	return provider.ModelInfo{ID: "stub", Name: "stub-model"}
}

type stubTool struct{}

func (stubTool) Name() string {
	return "stub"
}

func (stubTool) Description() string {
	return "stub tool"
}

func (stubTool) Parameters() tooling.JSONSchema {
	return tooling.JSONSchema{Type: "object"}
}

func (stubTool) Run(context.Context, model.ToolCallPart) (tooling.ToolResult, error) {
	return tooling.ToolResult{Content: "ok", IsError: false}, nil
}

func newTestAgent(t *testing.T) *Agent {
	t.Helper()
	a := NewAgent(
		stubSessionStore{},
		stubMessageStore{},
		[]tooling.Tool{stubTool{}},
		stubProvider{},
	)
	if a == nil {
		t.Fatal("expected non-nil agent")
	}
	return a
}

func TestNewAgentInitialization(t *testing.T) {
	a := newTestAgent(t)
	if a.IsActive() {
		t.Fatal("new agent should be inactive")
	}
	if a.queue == nil {
		t.Fatal("new agent queue must not be nil")
	}
	if a.queue.Len() != 0 {
		t.Fatalf("new agent queue must be empty, got %d", a.queue.Len())
	}
	if a.Events() == nil {
		t.Fatal("new agent events broker must not be nil")
	}
}

func TestAgentCancel(t *testing.T) {
	a := newTestAgent(t)
	a.Enqueue(Input{SessionID: "s1", Content: "hello", Source: SourceAPI})
	if !a.IsActive() {
		t.Fatal("agent should be active after enqueue")
	}
	a.Cancel()
	if a.IsActive() {
		t.Fatal("agent should be inactive after cancel")
	}
}

func TestAgentEventsSubscription(t *testing.T) {
	a := newTestAgent(t)
	ch, unsub := a.Events().Subscribe()
	defer unsub()

	msg := &Message{
		ID:        "m1",
		SessionID: "s1",
		Role:      RoleAssistant,
		Parts: []MessagePart{
			TextPart{Text: "done"},
			ToolCallPart{ID: "tc1", Name: "noop", Parameters: json.RawMessage(`{"a":1}`)},
		},
		CreatedAt: time.Date(2026, 2, 21, 0, 0, 0, 0, time.UTC),
	}
	want := AgentEvent{Type: EventResponse, SessionID: "s1", Message: msg}
	a.Events().Publish(want)

	select {
	case got := <-ch:
		if got.Type != EventResponse {
			t.Fatalf("unexpected event type: %q", got.Type)
		}
		if got.SessionID != "s1" {
			t.Fatalf("unexpected session id: %q", got.SessionID)
		}
		if got.Message == nil || got.Message.ID != "m1" {
			t.Fatalf("unexpected message: %#v", got.Message)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for event")
	}
}

func TestAgentEnqueueStartsWorkerOnce(t *testing.T) {
	a := newTestAgent(t)
	a.Enqueue(Input{SessionID: "s1", Content: "one", Source: SourceAPI})
	a.Enqueue(Input{SessionID: "s1", Content: "two", Source: SourceWebhook})
	if !a.IsActive() {
		t.Fatal("agent should be active after enqueue")
	}
	if a.queue.Len() != 2 {
		t.Fatalf("expected queue len 2, got %d", a.queue.Len())
	}
	a.Cancel()
}
