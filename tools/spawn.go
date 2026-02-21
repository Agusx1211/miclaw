package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/agusx1211/miclaw/agent"
	"github.com/agusx1211/miclaw/model"
	"github.com/agusx1211/miclaw/provider"
	"github.com/agusx1211/miclaw/store"
	"github.com/agusx1211/miclaw/tooling"
	"github.com/google/uuid"
)

const sessionsSpawnTimeout = 5 * time.Minute

type sessionsSpawnParams struct {
	Prompt string
	Title  string
}

type subagentInfo struct {
	ID              string
	ParentSessionID string
	Title           string
	StartedAt       time.Time
}

type subagentRegistry struct {
	mu    sync.Mutex
	items map[string]subagentInfo
}

var activeSubagents = newSubagentRegistry()

func sessionsSpawnTool(sessions store.SessionStore, messages store.MessageStore, prov provider.LLMProvider) Tool {

	return sessionsSpawnToolWithTimeout(sessions, messages, prov, sessionsSpawnTimeout)
}

func sessionsSpawnToolWithTimeout(
	sessions store.SessionStore,
	messages store.MessageStore,
	prov provider.LLMProvider,
	timeout time.Duration,
) Tool {
	return tool{
		name: "sessions_spawn",
		desc: "Spawn a read-only sub-agent and return its response",
		params: JSONSchema{
			Type:     "object",
			Required: []string{"prompt"},
			Properties: map[string]JSONSchema{
				"prompt": {Type: "string", Desc: "Prompt for the sub-agent"},
				"title":  {Type: "string", Desc: "Optional label for the child session"},
			},
		},
		runFn: func(ctx context.Context, call model.ToolCallPart) (ToolResult, error) {
			return runSessionsSpawn(ctx, sessions, messages, prov, call, timeout)
		},
	}
}

func subagentsTool() Tool {
	return tool{
		name:   "subagents",
		desc:   "List active sub-agents for this session",
		params: JSONSchema{Type: "object"},
		runFn: func(ctx context.Context, call model.ToolCallPart) (ToolResult, error) {
			if err := expectEmptyObject(call.Parameters); err != nil {
				return ToolResult{}, err
			}
			parentID := tooling.SessionIDFromContext(ctx)
			items := activeSubagents.List(parentID)
			return ToolResult{Content: formatSubagents(items)}, nil
		},
	}
}

func runSessionsSpawn(
	ctx context.Context,
	sessions store.SessionStore,
	messages store.MessageStore,
	prov provider.LLMProvider,
	call model.ToolCallPart,
	timeout time.Duration,
) (ToolResult, error) {
	p, err := parseSessionsSpawnParams(call.Parameters)
	if err != nil {
		return ToolResult{}, err
	}
	parentID := tooling.SessionIDFromContext(ctx)
	id := childSessionID(call.ID)
	if err := createSubagentSession(sessions, id, parentID, p.Title); err != nil {
		return ToolResult{}, err
	}
	info := subagentInfo{ID: id, ParentSessionID: parentID, Title: p.Title, StartedAt: time.Now().UTC()}
	activeSubagents.Add(info)
	defer activeSubagents.Remove(id)

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	text, err := runSubagent(runCtx, sessions, messages, prov, id, p.Prompt)
	if err != nil {
		return ToolResult{}, err
	}
	return ToolResult{Content: text}, nil
}

func parseSessionsSpawnParams(raw json.RawMessage) (sessionsSpawnParams, error) {
	var input struct {
		Prompt *string `json:"prompt"`
		Title  *string `json:"title"`
	}
	if err := unmarshalObject(raw, &input); err != nil {
		return sessionsSpawnParams{}, err
	}
	if input.Prompt == nil || strings.TrimSpace(*input.Prompt) == "" {
		return sessionsSpawnParams{}, errors.New("prompt is required")
	}
	out := sessionsSpawnParams{Prompt: strings.TrimSpace(*input.Prompt)}
	if input.Title != nil {
		out.Title = strings.TrimSpace(*input.Title)
	}
	return out, nil
}

func childSessionID(callID string) string {

	v := strings.TrimSpace(callID)
	if v != "" {
		return v
	}
	return uuid.NewString()
}

func createSubagentSession(sessions store.SessionStore, id, parentID, title string) error {

	now := time.Now().UTC()
	s := &model.Session{ID: id, ParentSessionID: parentID, Title: title, CreatedAt: now, UpdatedAt: now}
	return sessions.Create(s)
}

func runSubagent(
	ctx context.Context,
	sessions store.SessionStore,
	messages store.MessageStore,
	prov provider.LLMProvider,
	sessionID, prompt string,
) (string, error) {
	a := agent.NewAgent(sessions, messages, SubAgentTools(), prov)
	a.SetPromptMode("minimal")
	events, unsub := a.Events().Subscribe()
	defer unsub()
	done := make(chan error, 1)
	go func() {
		done <- a.RunOnce(ctx, agent.Input{SessionID: sessionID, Content: prompt, Source: agent.SourceAPI})
	}()
	return waitSubagentResponse(ctx, sessionID, events, done)
}

func waitSubagentResponse(
	ctx context.Context,
	sessionID string,
	events <-chan agent.AgentEvent,
	done <-chan error,
) (string, error) {
	text := ""
	for {
		select {
		case ev := <-events:
			if ev.Type == agent.EventResponse && ev.SessionID == sessionID {
				text = messageText(ev.Message)
			}
		case err := <-done:
			if err != nil {
				return "", err
			}
			return text, nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

func messageText(msg *model.Message) string {

	parts := make([]string, 0, len(msg.Parts))
	for _, part := range msg.Parts {
		p, ok := part.(model.TextPart)
		if ok && p.Text != "" {
			parts = append(parts, p.Text)
		}
	}
	return strings.Join(parts, "")
}

func formatSubagents(items []subagentInfo) string {

	lines := make([]string, 0, len(items))
	for _, item := range items {
		line := fmt.Sprintf(
			"id=%s\ttitle=%s\tstarted_at=%s",
			item.ID,
			item.Title,
			item.StartedAt.Format(time.RFC3339Nano),
		)
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func newSubagentRegistry() *subagentRegistry {

	return &subagentRegistry{items: map[string]subagentInfo{}}
}

func (r *subagentRegistry) Add(v subagentInfo) {

	r.mu.Lock()
	defer r.mu.Unlock()
	r.items[v.ID] = v
}

func (r *subagentRegistry) Remove(id string) {

	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.items, id)
}

func (r *subagentRegistry) List(parentID string) []subagentInfo {

	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]subagentInfo, 0, len(r.items))
	for _, item := range r.items {
		if item.ParentSessionID == parentID {
			out = append(out, item)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].StartedAt.Equal(out[j].StartedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].StartedAt.Before(out[j].StartedAt)
	})
	return out
}

func (r *subagentRegistry) Reset() {

	r.mu.Lock()
	defer r.mu.Unlock()
	r.items = map[string]subagentInfo{}
}
