package agent

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/agusx1211/miclaw/prompt"
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
	workspace   *prompt.Workspace
	skills      []prompt.SkillSummary
	memory      string
	heartbeat   string
	runtimeInfo string
	lastUsage   *provider.UsageInfo

	mu    sync.Mutex
	runID atomic.Uint64
}

func NewAgent(
	sessions store.SessionStore,
	messages store.MessageStore,
	toolList []tools.Tool,
	prov provider.LLMProvider,
) *Agent {

	a := &Agent{
		sessions:    sessions,
		messages:    messages,
		tools:       append([]tools.Tool(nil), toolList...),
		provider:    prov,
		eventBroker: NewBroker[AgentEvent](),
		queue:       &InputQueue{},
		workspace:   &prompt.Workspace{},
		skills:      []prompt.SkillSummary{},
	}

	return a
}

func (a *Agent) Enqueue(input Input) {

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
	go a.processQueue(ctx, runID)
}

func (a *Agent) Cancel() {

	a.mu.Lock()
	cancel := a.cancel
	a.cancel = nil
	a.active.Store(false)
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}

}

func (a *Agent) IsActive() bool {

	return a.active.Load()
}

func (a *Agent) Events() *Broker[AgentEvent] {

	return a.eventBroker
}

func (a *Agent) processQueue(ctx context.Context, runID uint64) {
	defer func() {
		a.mu.Lock()
		if a.runID.Load() == runID {
			a.cancel = nil
			a.active.Store(false)
		}
		a.mu.Unlock()
	}()
	for {
		if ctx.Err() != nil {
			return
		}
		input, ok := a.queue.Pop()
		if !ok {
			return
		}
		if err := a.processGeneration(ctx, input); err != nil {
			a.eventBroker.Publish(AgentEvent{Type: EventError, SessionID: input.SessionID, Error: err})
		}
	}
}
