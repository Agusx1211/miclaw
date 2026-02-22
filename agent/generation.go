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
	"github.com/agusx1211/miclaw/tooling"
	"github.com/google/uuid"
)

const threadMessageLimit = 1_000_000
const traceTextLimit = 180

type toolCallState struct {
	id   string
	name string
	args strings.Builder
}

func (a *Agent) run(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		pending := a.pending.Drain()
		if len(pending) == 0 {
			return nil
		}
		a.tracef("pending=%d", len(pending))
		if err := a.injectInputs(pending); err != nil {
			return err
		}
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			hasToolCalls, err := a.streamAndHandle(ctx, a.tools)
			if err != nil {
				return err
			}
			if !hasToolCalls {
				break
			}
			if more := a.pending.Drain(); len(more) > 0 {
				if err := a.injectInputs(more); err != nil {
					return err
				}
			}
		}
	}
}

func (a *Agent) injectInputs(inputs []Input) error {
	for _, input := range inputs {
		source := strings.TrimSpace(input.Source)
		if source == "" {
			source = "unknown"
		}
		a.tracef("in source=%s msg=%q", source, compactTraceText(input.Content))
		msg := newUserMessage(formatInput(input))
		if err := a.messages.Create(msg); err != nil {
			return err
		}
	}
	return nil
}

func formatInput(input Input) string {
	source := strings.TrimSpace(input.Source)
	content := strings.TrimSpace(input.Content)
	if source == "" {
		return content
	}
	if content == "" {
		return "[" + source + "]"
	}
	return "[" + source + "] " + content
}

func newUserMessage(content string) *Message {

	now := time.Now().UTC()
	msg := &Message{
		ID:        uuid.NewString(),
		Role:      RoleUser,
		Parts:     []MessagePart{TextPart{Text: content}},
		CreatedAt: now,
	}

	return msg
}

func (a *Agent) streamAndHandle(ctx context.Context, toolList []tooling.Tool) (bool, error) {
	msgs, err := a.messages.List(threadMessageLimit, 0)
	if err != nil {
		return false, err
	}
	assistant := &Message{ID: uuid.NewString(), Role: RoleAssistant, CreatedAt: time.Now().UTC()}
	history := a.buildHistory(msgs)
	text, reasoning, calls, _, err := a.collectStream(ctx, history, toProviderDefs(toolList))
	if err != nil {
		return false, err
	}
	if reasoning != "" {
		a.tracef("think=%q", compactTraceText(reasoning))
	}
	if text != "" {
		a.tracef("mono=%q", compactTraceText(text))
	}
	for _, call := range calls {
		a.tracef("tool_call id=%s name=%s args=%q", call.ID, call.Name, compactTraceText(string(call.Parameters)))
	}
	assistant.Parts = buildAssistantParts(text, reasoning, calls)
	if err := a.messages.Create(assistant); err != nil {
		return false, err
	}
	if len(calls) == 0 {
		return false, nil
	}

	toolMsg, err := runTools(ctx, toolList, calls)
	if toolMsg != nil {
		if err := a.messages.Create(toolMsg); err != nil {
			return false, err
		}
		traceToolResults(a, calls, toolMsg)
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

func runTools(ctx context.Context, toolList []tooling.Tool, calls []ToolCallPart) (*Message, error) {

	parts := make([]MessagePart, 0, len(calls))
	for i, call := range calls {
		if err := ctx.Err(); err != nil {
			parts = appendCancelled(parts, calls[i:])
			return newToolMessage(parts), err
		}
		result := runTool(ctx, toolList, call)
		if err := ctx.Err(); err != nil {
			parts = append(parts, cancelledPart(call))
			parts = appendCancelled(parts, calls[i+1:])
			return newToolMessage(parts), err
		}
		parts = append(parts, result)
	}
	return newToolMessage(parts), nil
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

func newToolMessage(parts []MessagePart) *Message {
	return &Message{ID: uuid.NewString(), Role: RoleTool, Parts: parts, CreatedAt: time.Now().UTC()}
}

func runTool(ctx context.Context, toolList []tooling.Tool, call ToolCallPart) ToolResultPart {

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

func findTool(toolList []tooling.Tool, name string) tooling.Tool {

	for _, tool := range toolList {
		if tool.Name() == name {
			return tool
		}
	}
	return nil
}

func toProviderDefs(toolList []tooling.Tool) []provider.ToolDef {

	raw := tooling.ToProviderDefs(toolList)
	defs := make([]provider.ToolDef, 0, len(raw))
	for _, def := range raw {
		defs = append(defs, provider.ToolDef{Name: def.Name, Description: def.Description, Parameters: def.Parameters})
	}
	return defs
}

func traceToolResults(a *Agent, calls []ToolCallPart, msg *Message) {

	for _, part := range msg.Parts {
		result, ok := part.(ToolResultPart)
		if !ok {
			continue
		}
		a.tracef(
			"tool_result id=%s name=%s err=%t out=%q",
			result.ToolCallID,
			findToolCallName(calls, result.ToolCallID),
			result.IsError,
			compactTraceText(result.Content),
		)
	}
}

func findToolCallName(calls []ToolCallPart, id string) string {

	for _, call := range calls {
		if call.ID == id {
			return call.Name
		}
	}
	return "unknown"
}

func compactTraceText(raw string) string {

	clean := strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if len(clean) <= traceTextLimit {
		return clean
	}
	return clean[:traceTextLimit-3] + "..."
}

func (a *Agent) buildHistory(messages []*Message) []model.Message {

	out := []model.Message{a.systemMessage()}
	return append(out, flattenMessages(messages)...)
}

func (a *Agent) systemMessage() model.Message {

	mode := a.promptMode
	if mode == "" {
		mode = "full"
	}
	txt := prompt.BuildSystemPrompt(prompt.SystemPromptParams{
		Mode:         mode,
		Workspace:    a.workspace,
		Skills:       a.skills,
		MemoryRecall: a.memory,
		DateTime:     time.Now().UTC(),
		Heartbeat:    a.heartbeat,
		RuntimeInfo:  a.runtimeInfo,
	})
	msg := model.Message{
		ID:        "system-" + uuid.NewString(),
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

func findLastUserMessage(messages []*Message) model.Message {

	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == model.RoleUser {
			return model.Message(*messages[i])
		}
	}
	panic("last user message not found")
}
