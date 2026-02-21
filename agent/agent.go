package agent

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/agusx1211/miclaw/provider"
	"github.com/agusx1211/miclaw/store"
	"github.com/agusx1211/miclaw/tools"
)

type Agent struct {
	sessions    store.SessionStore
	messages    store.MessageStore
	tools       []tools.Tool
	provider    provider.LLMProvider
	active      atomic.Bool
	cancel      context.CancelFunc
	eventBroker *Broker[AgentEvent]
	queue       *InputQueue

	mu    sync.Mutex
	runID atomic.Uint64
}

func NewAgent(
	sessions store.SessionStore,
	messages store.MessageStore,
	toolList []tools.Tool,
	prov provider.LLMProvider,
) *Agent {
	must(sessions != nil, "session store must not be nil")
	must(messages != nil, "message store must not be nil")
	must(prov != nil, "provider must not be nil")
	a := &Agent{
		sessions:    sessions,
		messages:    messages,
		tools:       append([]tools.Tool(nil), toolList...),
		provider:    prov,
		eventBroker: NewBroker[AgentEvent](),
		queue:       &InputQueue{},
	}
	must(a.provider != nil, "provider must not be nil")
	must(a.eventBroker != nil, "event broker must not be nil")
	must(a.queue != nil, "input queue must not be nil")
	return a
}

func (a *Agent) Enqueue(input Input) {
	must(a != nil, "agent must not be nil")
	must(a.queue != nil, "input queue must not be nil")
	a.queue.Push(input)
	a.mu.Lock()
	if a.active.Load() {
		a.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	runID := a.runID.Add(1)
	a.cancel = cancel
	a.active.Store(true)
	a.mu.Unlock()
	go a.awaitCancellation(ctx, runID)
}

func (a *Agent) Cancel() {
	must(a != nil, "agent must not be nil")
	must(a.queue != nil, "input queue must not be nil")
	a.mu.Lock()
	cancel := a.cancel
	a.cancel = nil
	a.active.Store(false)
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	must(!a.active.Load(), "agent must be inactive after cancel")
}

func (a *Agent) IsActive() bool {
	must(a != nil, "agent must not be nil")
	must(a.eventBroker != nil, "event broker must not be nil")
	return a.active.Load()
}

func (a *Agent) Events() *Broker[AgentEvent] {
	must(a != nil, "agent must not be nil")
	must(a.eventBroker != nil, "event broker must not be nil")
	return a.eventBroker
}

func (a *Agent) awaitCancellation(ctx context.Context, runID uint64) {
	must(a != nil, "agent must not be nil")
	must(ctx != nil, "context must not be nil")
	<-ctx.Done()
	a.mu.Lock()
	if a.runID.Load() == runID {
		a.cancel = nil
		a.active.Store(false)
	}
	a.mu.Unlock()
	must(runID > 0, "run id must be positive")
}
