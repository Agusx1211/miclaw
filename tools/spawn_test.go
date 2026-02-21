package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agusx1211/miclaw/model"
	"github.com/agusx1211/miclaw/provider"
	"github.com/agusx1211/miclaw/tooling"
)

type spawnScript func(context.Context, []model.Message, []provider.ToolDef) <-chan provider.ProviderEvent

type spawnProvider struct {
	mu           sync.Mutex
	fn           spawnScript
	model        provider.ModelInfo
	calls        int
	seenMessages [][]model.Message
	seenTools    [][]provider.ToolDef
}

func (p *spawnProvider) Stream(ctx context.Context, msgs []model.Message, defs []provider.ToolDef) <-chan provider.ProviderEvent {
	p.mu.Lock()
	p.calls++
	p.seenMessages = append(p.seenMessages, append([]model.Message(nil), msgs...))
	p.seenTools = append(p.seenTools, append([]provider.ToolDef(nil), defs...))
	fn := p.fn
	p.mu.Unlock()
	return fn(ctx, msgs, defs)
}

func (p *spawnProvider) Model() provider.ModelInfo {
	return p.model
}

func (p *spawnProvider) CallCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func (p *spawnProvider) SeenTools() [][]provider.ToolDef {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([][]provider.ToolDef, 0, len(p.seenTools))
	for _, defs := range p.seenTools {
		out = append(out, append([]provider.ToolDef(nil), defs...))
	}
	return out
}

func (p *spawnProvider) SeenMessages() [][]model.Message {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([][]model.Message, 0, len(p.seenMessages))
	for _, msgs := range p.seenMessages {
		out = append(out, append([]model.Message(nil), msgs...))
	}
	return out
}

func streamEvents(events ...provider.ProviderEvent) spawnScript {
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

func runSpawnCall(ctx context.Context, tl Tool, id string, params map[string]any) (ToolResult, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return ToolResult{}, err
	}
	call := model.ToolCallPart{ID: id, Name: tl.Name(), Parameters: raw}
	return tl.Run(ctx, call)
}

func parentCtx(sessionID string) context.Context {

	return tooling.WithSessionID(context.Background(), sessionID)
}

func TestSessionsSpawnSimpleResponse(t *testing.T) {
	activeSubagents.Reset()
	s := openSessionsTestStore(t)
	at := time.Date(2026, 2, 21, 10, 0, 0, 0, time.UTC)
	createSession(t, s, "parent", "parent", at)
	p := &spawnProvider{
		fn:    streamEvents(provider.ProviderEvent{Type: provider.EventContentDelta, Delta: "sub-done"}, provider.ProviderEvent{Type: provider.EventComplete}),
		model: provider.ModelInfo{Name: "test"},
	}
	tool := sessionsSpawnToolWithTimeout(s.SessionStore(), s.MessageStore(), p, nil, nil, time.Second)
	got, err := runSpawnCall(parentCtx("parent"), tool, "child-1", map[string]any{"prompt": "task", "title": "research"})
	if err != nil {
		t.Fatalf("run sessions_spawn: %v", err)
	}
	if got.Content != "sub-done" {
		t.Fatalf("unexpected content: %q", got.Content)
	}
	child, err := s.SessionStore().Get("child-1")
	if err != nil {
		t.Fatalf("get child session: %v", err)
	}
	if child.ParentSessionID != "parent" || child.Title != "research" {
		t.Fatalf("unexpected child session: %#v", child)
	}
}

func TestSessionsSpawnUsesReadTool(t *testing.T) {
	activeSubagents.Reset()
	s := openSessionsTestStore(t)
	path := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(path, []byte("alpha\nbeta\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	p := &spawnProvider{
		model: provider.ModelInfo{Name: "test"},
		fn: func(_ context.Context, msgs []model.Message, _ []provider.ToolDef) <-chan provider.ProviderEvent {
			if out := findToolOutput(msgs); out != "" {
				return streamEvents(
					provider.ProviderEvent{Type: provider.EventContentDelta, Delta: "read=>"},
					provider.ProviderEvent{Type: provider.EventContentDelta, Delta: out},
					provider.ProviderEvent{Type: provider.EventComplete},
				)(context.Background(), nil, nil)
			}
			args := fmt.Sprintf(`{"path":%q}`, path)
			return streamEvents(
				provider.ProviderEvent{Type: provider.EventToolUseStart, ToolCallID: "r1", ToolName: "read"},
				provider.ProviderEvent{Type: provider.EventToolUseDelta, ToolCallID: "r1", Delta: args},
				provider.ProviderEvent{Type: provider.EventToolUseStop, ToolCallID: "r1"},
				provider.ProviderEvent{Type: provider.EventComplete},
			)(context.Background(), nil, nil)
		},
	}
	tool := sessionsSpawnToolWithTimeout(s.SessionStore(), s.MessageStore(), p, nil, nil, time.Second)
	got, err := runSpawnCall(parentCtx("parent"), tool, "child-read", map[string]any{"prompt": "read file"})
	if err != nil {
		t.Fatalf("run sessions_spawn: %v", err)
	}
	if !strings.Contains(got.Content, "alpha") {
		t.Fatalf("expected read output in response, got %q", got.Content)
	}
}

func TestSessionsSpawnSubAgentCannotSpawn(t *testing.T) {
	activeSubagents.Reset()
	s := openSessionsTestStore(t)
	p := &spawnProvider{
		fn:    streamEvents(provider.ProviderEvent{Type: provider.EventContentDelta, Delta: "ok"}, provider.ProviderEvent{Type: provider.EventComplete}),
		model: provider.ModelInfo{Name: "test"},
	}
	tool := sessionsSpawnToolWithTimeout(s.SessionStore(), s.MessageStore(), p, nil, nil, time.Second)
	if _, err := runSpawnCall(parentCtx("p"), tool, "child-tools", map[string]any{"prompt": "tools?"}); err != nil {
		t.Fatalf("run sessions_spawn: %v", err)
	}
	defs := p.SeenTools()
	if len(defs) == 0 {
		t.Fatal("expected provider to receive tool defs")
	}
	for _, d := range defs[0] {
		if d.Name == "sessions_spawn" {
			t.Fatalf("unexpected sessions_spawn in sub-agent tool list: %#v", defs[0])
		}
	}
	if len(defs[0]) != 6 {
		t.Fatalf("expected 6 sub-agent tools, got %d", len(defs[0]))
	}
}

func TestSessionsSpawnTimeout(t *testing.T) {
	activeSubagents.Reset()
	s := openSessionsTestStore(t)
	p := &spawnProvider{
		model: provider.ModelInfo{Name: "test"},
		fn: func(ctx context.Context, _ []model.Message, _ []provider.ToolDef) <-chan provider.ProviderEvent {
			ch := make(chan provider.ProviderEvent)
			go func() {
				defer close(ch)
				<-ctx.Done()
			}()
			return ch
		},
	}
	tool := sessionsSpawnToolWithTimeout(s.SessionStore(), s.MessageStore(), p, nil, nil, 20*time.Millisecond)
	_, err := runSpawnCall(parentCtx("parent"), tool, "child-timeout", map[string]any{"prompt": "wait"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
}

func TestSessionsSpawnConcurrent(t *testing.T) {
	activeSubagents.Reset()
	s := openSessionsTestStore(t)
	p := &spawnProvider{
		model: provider.ModelInfo{Name: "test"},
		fn: func(_ context.Context, msgs []model.Message, _ []provider.ToolDef) <-chan provider.ProviderEvent {
			return streamEvents(
				provider.ProviderEvent{Type: provider.EventContentDelta, Delta: "ok:" + lastUserText(msgs)},
				provider.ProviderEvent{Type: provider.EventComplete},
			)(context.Background(), nil, nil)
		},
	}
	tool := sessionsSpawnToolWithTimeout(s.SessionStore(), s.MessageStore(), p, nil, nil, time.Second)
	const n = 5
	type result struct {
		i   int
		got ToolResult
		err error
	}
	out := make(chan result, n)
	for i := range n {
		go func(i int) {
			prompt := "task-" + strconv.Itoa(i)
			id := "child-" + strconv.Itoa(i)
			got, err := runSpawnCall(parentCtx("parent"), tool, id, map[string]any{"prompt": prompt})
			out <- result{i: i, got: got, err: err}
		}(i)
	}
	for range n {
		r := <-out
		if r.err != nil {
			t.Fatalf("spawn %d failed: %v", r.i, r.err)
		}
		want := "ok:task-" + strconv.Itoa(r.i)
		if r.got.Content != want {
			t.Fatalf("spawn %d content mismatch: want %q got %q", r.i, want, r.got.Content)
		}
	}
	if p.CallCount() != n {
		t.Fatalf("expected %d provider calls, got %d", n, p.CallCount())
	}
}

func TestSubagentsListShowsActive(t *testing.T) {
	activeSubagents.Reset()
	s := openSessionsTestStore(t)
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	p := &spawnProvider{
		model: provider.ModelInfo{Name: "test"},
		fn: func(_ context.Context, _ []model.Message, _ []provider.ToolDef) <-chan provider.ProviderEvent {
			ch := make(chan provider.ProviderEvent, 2)
			go func() {
				defer close(ch)
				started <- struct{}{}
				<-release
				ch <- provider.ProviderEvent{Type: provider.EventContentDelta, Delta: "done"}
				ch <- provider.ProviderEvent{Type: provider.EventComplete}
			}()
			return ch
		},
	}
	spawn := sessionsSpawnToolWithTimeout(s.SessionStore(), s.MessageStore(), p, nil, nil, time.Second)
	list := subagentsTool()
	done := make(chan error, 1)
	go func() {
		_, err := runSpawnCall(parentCtx("parent"), spawn, "child-live", map[string]any{"prompt": "hold", "title": "live"})
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for sub-agent start")
	}
	v, err := runSpawnCall(parentCtx("parent"), list, "list-1", map[string]any{})
	if err != nil {
		t.Fatalf("run subagents: %v", err)
	}
	if !strings.Contains(v.Content, "id=child-live") || !strings.Contains(v.Content, "title=live") {
		t.Fatalf("expected active child in list, got %q", v.Content)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("spawn run failed: %v", err)
	}
	v, err = runSpawnCall(parentCtx("parent"), list, "list-2", map[string]any{})
	if err != nil {
		t.Fatalf("run subagents after completion: %v", err)
	}
	if v.Content != "" {
		t.Fatalf("expected empty list after completion, got %q", v.Content)
	}
}

func TestSessionsSpawnUsesMinimalPrompt(t *testing.T) {
	activeSubagents.Reset()
	s := openSessionsTestStore(t)
	p := &spawnProvider{
		fn:    streamEvents(provider.ProviderEvent{Type: provider.EventContentDelta, Delta: "ok"}, provider.ProviderEvent{Type: provider.EventComplete}),
		model: provider.ModelInfo{Name: "test"},
	}
	tool := sessionsSpawnToolWithTimeout(s.SessionStore(), s.MessageStore(), p, nil, nil, time.Second)
	if _, err := runSpawnCall(parentCtx("parent"), tool, "child-prompt", map[string]any{"prompt": "probe"}); err != nil {
		t.Fatalf("run sessions_spawn: %v", err)
	}
	msgs := p.SeenMessages()
	if len(msgs) == 0 || len(msgs[0]) == 0 {
		t.Fatal("expected captured provider messages")
	}
	system := messageText(&msgs[0][0])
	if strings.Contains(system, "## Tool Call Style") || strings.Contains(system, "## Safety") {
		t.Fatalf("expected minimal system prompt, got:\n%s", system)
	}
}

func findToolOutput(msgs []model.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != model.RoleTool {
			continue
		}
		for _, part := range msgs[i].Parts {
			p, ok := part.(model.ToolResultPart)
			if ok {
				return p.Content
			}
		}
	}
	return ""
}

func lastUserText(msgs []model.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != model.RoleUser {
			continue
		}
		for _, part := range msgs[i].Parts {
			p, ok := part.(model.TextPart)
			if ok {
				return p.Text
			}
		}
	}
	return ""
}
