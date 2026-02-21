package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/agusx1211/miclaw/model"
	"github.com/agusx1211/miclaw/prompt"
	"github.com/agusx1211/miclaw/provider"
	"github.com/agusx1211/miclaw/tools"
	"github.com/google/uuid"
)

const sessionMessageLimit = 1_000_000

type toolCallState struct {
	id   string
	name string
	args strings.Builder
}

func (a *Agent) processGeneration(ctx context.Context, input Input) error {
	session, err := a.getOrCreateSession(input.SessionID)
	if err != nil {
		return err
	}
	msgs, err := a.messages.ListBySession(session.ID, sessionMessageLimit, 0)
	if err != nil {
		return err
	}
	user := newUserMessage(session.ID, input.Content)
	if err := a.messages.Create(user); err != nil {
		return err
	}
	msgs = append(msgs, user)
	total := &provider.UsageInfo{}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		hasToolCalls, err := a.streamAndHandle(ctx, session, msgs, a.tools)
		if err != nil {
			return err
		}
		mergeUsage(total, a.takeLastUsage())
		if !hasToolCalls {
			break
		}
		msgs, err = a.messages.ListBySession(session.ID, sessionMessageLimit, 0)
		if err != nil {
			return err
		}
	}

	return a.updateSessionUsage(session, total)
}

func (a *Agent) getOrCreateSession(id string) (*Session, error) {

	sid := strings.TrimSpace(id)
	if sid != "" {
		session, err := a.sessions.Get(sid)
		if err == nil {
			return session, nil
		}
		return a.createSession(sid)
	}

	return a.createSession(uuid.NewString())
}

func (a *Agent) createSession(id string) (*Session, error) {

	now := time.Now().UTC()
	s := &Session{ID: id, CreatedAt: now, UpdatedAt: now}
	if err := a.sessions.Create(s); err != nil {
		return nil, err
	}

	return s, nil
}

func newUserMessage(sessionID, content string) *Message {

	now := time.Now().UTC()
	msg := &Message{
		ID:        uuid.NewString(),
		SessionID: sessionID,
		Role:      RoleUser,
		Parts:     []MessagePart{TextPart{Text: content}},
		CreatedAt: now,
	}

	return msg
}

func (a *Agent) streamAndHandle(ctx context.Context, session *Session, messages []*Message, toolList []tools.Tool) (bool, error) {
	a.setLastUsage(nil)

	assistant := &Message{ID: uuid.NewString(), SessionID: session.ID, Role: RoleAssistant, CreatedAt: time.Now().UTC()}
	history := a.buildHistory(session, messages)
	text, reasoning, calls, usage, err := a.collectStream(ctx, history, toProviderDefs(toolList))
	if err != nil {
		return false, err
	}
	a.setLastUsage(usage)
	assistant.Parts = buildAssistantParts(text, reasoning, calls)
	if err := a.messages.Create(assistant); err != nil {
		return false, err
	}
	if len(calls) == 0 {
		a.eventBroker.Publish(AgentEvent{Type: EventResponse, SessionID: session.ID, Message: assistant})
		return false, nil
	}

	toolMsg, err := runTools(ctx, session.ID, toolList, calls)
	if toolMsg != nil {
		if err := a.messages.Create(toolMsg); err != nil {
			return false, err
		}
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (a *Agent) collectStream(ctx context.Context, history []model.Message, defs []provider.ToolDef) (string, string, []ToolCallPart, *provider.UsageInfo, error) {

	text := &strings.Builder{}
	reasoning := &strings.Builder{}
	calls := map[string]*toolCallState{}
	order := make([]string, 0, 4)
	var usage *provider.UsageInfo
	for event := range a.provider.Stream(ctx, history, defs) {
		switch event.Type {
		case provider.EventContentDelta:
			text.WriteString(event.Delta)
		case provider.EventThinkingDelta:
			reasoning.WriteString(event.Delta)
		case provider.EventToolUseStart:
			applyToolEvent(calls, &order, event, false)
		case provider.EventToolUseDelta:
			applyToolEvent(calls, &order, event, true)
		case provider.EventToolUseStop:
			applyToolEvent(calls, &order, event, false)
		case provider.EventComplete:
			usage = event.Usage
		case provider.EventError:
			return "", "", nil, nil, event.Error
		}
	}
	if err := ctx.Err(); err != nil {
		return "", "", nil, nil, err
	}

	return text.String(), reasoning.String(), finalizeToolCalls(order, calls), usage, nil
}

func applyToolEvent(calls map[string]*toolCallState, order *[]string, event provider.ProviderEvent, addDelta bool) {

	st := getToolCallState(calls, order, event.ToolCallID)
	if event.ToolName != "" {
		st.name = event.ToolName
	}
	if addDelta && event.Delta != "" {
		st.args.WriteString(event.Delta)
	}
}

func getToolCallState(calls map[string]*toolCallState, order *[]string, id string) *toolCallState {

	key := id
	if key == "" {
		key = uuid.NewString()
	}
	state, ok := calls[key]
	if ok {
		return state
	}
	state = &toolCallState{id: key}
	calls[key] = state
	*order = append(*order, key)
	return state
}

func finalizeToolCalls(order []string, calls map[string]*toolCallState) []ToolCallPart {

	out := make([]ToolCallPart, 0, len(order))
	for _, id := range order {
		state := calls[id]
		raw := strings.TrimSpace(state.args.String())
		if raw == "" {
			raw = "{}"
		}
		out = append(out, ToolCallPart{ID: state.id, Name: state.name, Parameters: json.RawMessage(raw)})
	}
	return out
}

func buildAssistantParts(text, reasoning string, calls []ToolCallPart) []MessagePart {

	parts := make([]MessagePart, 0, 2+len(calls))
	if text != "" {
		parts = append(parts, TextPart{Text: text})
	}
	if reasoning != "" {
		parts = append(parts, ReasoningPart{Text: reasoning})
	}
	for _, c := range calls {
		parts = append(parts, c)
	}
	return parts
}

func runTools(ctx context.Context, sessionID string, toolList []tools.Tool, calls []ToolCallPart) (*Message, error) {

	parts := make([]MessagePart, 0, len(calls))
	for i, call := range calls {
		if err := ctx.Err(); err != nil {
			parts = appendCancelled(parts, calls[i:])
			return newToolMessage(sessionID, parts), err
		}
		result := runTool(ctx, toolList, call)
		if err := ctx.Err(); err != nil {
			parts = append(parts, cancelledPart(call))
			parts = appendCancelled(parts, calls[i+1:])
			return newToolMessage(sessionID, parts), err
		}
		parts = append(parts, result)
	}
	return newToolMessage(sessionID, parts), nil
}

func appendCancelled(parts []MessagePart, calls []ToolCallPart) []MessagePart {
	for _, call := range calls {
		parts = append(parts, cancelledPart(call))
	}
	return parts
}

func cancelledPart(call ToolCallPart) ToolResultPart {
	return ToolResultPart{ToolCallID: call.ID, Content: "Cancelled", IsError: true}
}

func newToolMessage(sessionID string, parts []MessagePart) *Message {
	return &Message{ID: uuid.NewString(), SessionID: sessionID, Role: RoleTool, Parts: parts, CreatedAt: time.Now().UTC()}
}

func runTool(ctx context.Context, toolList []tools.Tool, call ToolCallPart) ToolResultPart {

	tool := findTool(toolList, call.Name)
	if tool == nil {
		return ToolResultPart{ToolCallID: call.ID, Content: fmt.Sprintf("tool not found: %s", call.Name), IsError: true}
	}
	result, err := tool.Run(ctx, model.ToolCallPart(call))
	if err != nil {
		return ToolResultPart{ToolCallID: call.ID, Content: err.Error(), IsError: true}
	}

	return ToolResultPart{ToolCallID: call.ID, Content: result.Content, IsError: result.IsError}
}

func findTool(toolList []tools.Tool, name string) tools.Tool {

	for _, tool := range toolList {
		if tool.Name() == name {
			return tool
		}
	}
	return nil
}

func toProviderDefs(toolList []tools.Tool) []provider.ToolDef {

	raw := tools.ToProviderDefs(toolList)
	defs := make([]provider.ToolDef, 0, len(raw))
	for _, def := range raw {
		defs = append(defs, provider.ToolDef{Name: def.Name, Description: def.Description, Parameters: def.Parameters})
	}
	return defs
}

func (a *Agent) buildHistory(session *Session, messages []*Message) []model.Message {

	out := []model.Message{a.systemMessage(session.ID)}
	if session.SummaryMessageID == "" {
		return append(out, flattenMessages(messages)...)
	}
	summary := findMessage(messages, session.SummaryMessageID)
	summary.Role = model.RoleUser
	out = append(out, summary)
	last := findLastUserMessage(messages)
	if last.ID != summary.ID {
		out = append(out, last)
	}

	return out
}

func (a *Agent) systemMessage(sessionID string) model.Message {

	txt := prompt.BuildSystemPrompt(prompt.SystemPromptParams{
		Mode:         "full",
		Workspace:    a.workspace,
		Skills:       a.skills,
		MemoryRecall: a.memory,
		DateTime:     time.Now().UTC(),
		Heartbeat:    a.heartbeat,
		RuntimeInfo:  a.runtimeInfo,
	})
	msg := model.Message{
		ID:        "system-" + uuid.NewString(),
		SessionID: sessionID,
		Role:      model.RoleUser,
		Parts:     []model.MessagePart{model.TextPart{Text: txt}},
		CreatedAt: time.Now().UTC(),
	}

	return msg
}

func flattenMessages(messages []*Message) []model.Message {

	out := make([]model.Message, 0, len(messages))
	for _, msg := range messages {
		out = append(out, model.Message(*msg))
	}
	return out
}

func findMessage(messages []*Message, id string) model.Message {

	for _, msg := range messages {
		if msg.ID == id {
			return model.Message(*msg)
		}
	}
	panic("summary message not found")
}

func findLastUserMessage(messages []*Message) model.Message {

	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == model.RoleUser {
			return model.Message(*messages[i])
		}
	}
	panic("last user message not found")
}

func mergeUsage(dst *provider.UsageInfo, src *provider.UsageInfo) {

	if src == nil {
		return
	}
	dst.PromptTokens += src.PromptTokens
	dst.CompletionTokens += src.CompletionTokens
	dst.CacheReadTokens += src.CacheReadTokens
	dst.CacheWriteTokens += src.CacheWriteTokens
}

func (a *Agent) updateSessionUsage(session *Session, usage *provider.UsageInfo) error {

	session.PromptTokens += usage.PromptTokens
	session.CompletionTokens += usage.CompletionTokens
	session.Cost += float64(usage.PromptTokens)*a.provider.Model().CostPerInputToken +
		float64(usage.CompletionTokens)*a.provider.Model().CostPerOutputToken
	count, err := a.messages.CountBySession(session.ID)
	if err != nil {
		return err
	}
	session.MessageCount = count
	session.UpdatedAt = time.Now().UTC()

	return a.sessions.Update(session)
}

func (a *Agent) setLastUsage(usage *provider.UsageInfo) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if usage == nil {
		a.lastUsage = nil
		return
	}
	v := *usage
	a.lastUsage = &v
}

func (a *Agent) takeLastUsage() *provider.UsageInfo {
	a.mu.Lock()
	defer a.mu.Unlock()
	usage := a.lastUsage
	a.lastUsage = nil
	return usage
}
