package agent

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/agusx1211/miclaw/prompt"
	"github.com/agusx1211/miclaw/provider"
	"github.com/agusx1211/miclaw/store"
	"github.com/agusx1211/miclaw/tooling"
)

type Agent struct {
	messages          store.MessageStore
	tools             []tooling.Tool
	provider          provider.LLMProvider
	noToolSleepRounds int
	active            atomic.Bool
	cancel            context.CancelFunc
	eventBroker       *Broker[AgentEvent]
	pending           *InputQueue
	workspace         *prompt.Workspace
	skills            []prompt.SkillSummary
	memory            string
	heartbeat         string
	runtimeInfo       string
	promptMode        string
	trace             func(format string, args ...any)

	mu sync.Mutex
}

const defaultNoToolSleepRounds = 16

func NewAgent(
	messages store.MessageStore,
	toolList []tooling.Tool,
	prov provider.LLMProvider,
) *Agent {

	a := &Agent{
		messages:          messages,
		tools:             append([]tooling.Tool(nil), toolList...),
		provider:          prov,
		noToolSleepRounds: defaultNoToolSleepRounds,
		eventBroker:       NewBroker[AgentEvent](),
		pending:           &InputQueue{},
		workspace:         &prompt.Workspace{},
		skills:            []prompt.SkillSummary{},
		promptMode:        "full",
		trace:             func(string, ...any) {},
	}

	return a
}

func (a *Agent) RunOnce(ctx context.Context, input Input) error {

	if !a.active.CompareAndSwap(false, true) {
		return errors.New("agent is active")
	}
	defer a.active.Store(false)
	a.pending.Push(input)
	return a.run(ctx)
}

func (a *Agent) SetPromptMode(mode string) {

	a.promptMode = mode
}

func (a *Agent) SetWorkspace(ws *prompt.Workspace) {

	a.workspace = ws
}

func (a *Agent) SetSkills(skills []prompt.SkillSummary) {

	a.skills = skills
}

func (a *Agent) SetTrace(trace func(format string, args ...any)) {

	a.trace = trace
}

func (a *Agent) SetNoToolSleepRounds(rounds int) {

	a.noToolSleepRounds = rounds
}

func (a *Agent) Inject(input Input) {

	a.pending.Push(input)
	a.startWorker()
}

func (a *Agent) startWorker() {

	if !a.active.CompareAndSwap(false, true) {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	a.cancel = cancel
	a.mu.Unlock()
	go a.runAsync(ctx)
}

func (a *Agent) runAsync(ctx context.Context) {
	defer func() {
		a.mu.Lock()
		a.cancel = nil
		a.mu.Unlock()
		a.active.Store(false)
		if a.pending.Len() > 0 {
			a.startWorker()
		}
	}()
	a.tracef("wake")
	if err := a.run(ctx); err != nil {
		a.tracef("error=%v", err)
		a.eventBroker.Publish(AgentEvent{Type: EventError, Error: err})
	}
	a.tracef("sleep")
}

func (a *Agent) Cancel() {

	a.mu.Lock()
	cancel := a.cancel
	a.cancel = nil
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

func (a *Agent) tracef(format string, args ...any) {

	a.trace(format, args...)
}
