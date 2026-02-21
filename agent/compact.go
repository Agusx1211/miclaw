package agent

import (
	"context"
	"time"

	"github.com/agusx1211/miclaw/model"
	"github.com/google/uuid"
)

const compactPrompt = `Provide a detailed but concise summary of our conversation. Structure it as follows:
1. User Primary Goals and Intent
2. Conversation Timeline and Progress
3. Technical Context and Decisions
4. Files and Code Changes
5. Active Work and Last Actions (CRITICAL)
6. Unresolved Issues and Pending Tasks
7. Immediate Next Step
Be precise with technical details, file names, and code.`

func (a *Agent) Compact(ctx context.Context, session *Session) error {
	msgs, err := a.messages.ListBySession(session.ID, sessionMessageLimit, 0)
	if err != nil {
		return err
	}
	cleaned := cleanHistory(msgs)
	history := append(
		flattenMessages(cleaned),
		model.Message{
			ID:        uuid.NewString(),
			SessionID: session.ID,
			Role:      model.RoleUser,
			Parts:     []model.MessagePart{model.TextPart{Text: compactPrompt}},
			CreatedAt: time.Now().UTC(),
		},
	)
	summary, _, _, _, err := a.collectStream(ctx, history, nil)
	if err != nil {
		return err
	}
	summaryMsg := &Message{
		ID:        uuid.NewString(),
		SessionID: session.ID,
		Role:      model.RoleUser,
		Parts: []MessagePart{TextPart{
			Text: summary + "\n\nLast request from user was: " + lastUserText(findLastUserMessage(cleaned)),
		}},
		CreatedAt: time.Now().UTC(),
	}
	if err := a.messages.ReplaceSessionMessages(session.ID, []*Message{summaryMsg}); err != nil {
		return err
	}

	_, _, _, usage, err := a.collectStream(
		ctx,
		[]model.Message{a.systemMessage(session.ID), model.Message(*summaryMsg)},
		nil,
	)
	if err != nil {
		return err
	}
	session.SummaryMessageID = summaryMsg.ID
	session.PromptTokens = usage.PromptTokens
	session.CompletionTokens = usage.CompletionTokens
	session.MessageCount, err = a.messages.CountBySession(session.ID)
	if err != nil {
		return err
	}
	session.UpdatedAt = time.Now().UTC()
	if err := a.sessions.Update(session); err != nil {
		return err
	}
	a.eventBroker.Publish(AgentEvent{Type: EventCompact, SessionID: session.ID})
	return nil
}

func lastUserText(msg model.Message) string {
	for _, part := range msg.Parts {
		if text, ok := part.(TextPart); ok {
			return text.Text
		}
	}
	return ""
}

func cleanHistory(messages []*Message) []*Message {
	out := make([]*Message, 0, len(messages))
	for i := 0; i < len(messages); i++ {
		msg := messages[i]
		if msg.Role != RoleAssistant {
			if msg.Role != RoleTool {
				out = append(out, msg)
			}
			continue
		}
		out = append(out, msg)
		callIDs := collectToolCallIDs(msg.Parts)
		if len(callIDs) == 0 {
			continue
		}

		pending := make(map[string]struct{}, len(callIDs))
		for _, id := range callIDs {
			pending[id] = struct{}{}
		}

		next := i + 1
		for ; next < len(messages) && messages[next].Role == RoleTool; next++ {
			toolMsg := extractToolResults(messages[next], pending)
			if toolMsg != nil {
				out = append(out, toolMsg)
			}
		}
		for _, id := range callIDs {
			if _, ok := pending[id]; !ok {
				continue
			}
			out = append(out, toolNoResponse(msg.SessionID, id))
		}
		i = next - 1
	}
	if len(out) > 0 && out[len(out)-1].Role == RoleTool {
		return append(out, assistantFollowup(out[len(out)-1].SessionID))
	}
	return out
}

func collectToolCallIDs(parts []MessagePart) []string {
	out := make([]string, 0, 4)
	for _, part := range parts {
		if call, ok := part.(ToolCallPart); ok {
			out = append(out, call.ID)
		}
	}
	return out
}

func extractToolResults(msg *Message, pending map[string]struct{}) *Message {
	out := &Message{ID: msg.ID, SessionID: msg.SessionID, Role: msg.Role, CreatedAt: msg.CreatedAt}
	for _, part := range msg.Parts {
		result, ok := part.(ToolResultPart)
		if !ok {
			continue
		}
		if _, ok := pending[result.ToolCallID]; !ok {
			continue
		}
		delete(pending, result.ToolCallID)
		out.Parts = append(out.Parts, result)
	}
	if len(out.Parts) == 0 {
		return nil
	}
	return out
}

func toolNoResponse(sessionID, callID string) *Message {
	return &Message{
		ID:        uuid.NewString(),
		SessionID: sessionID,
		Role:      RoleTool,
		Parts:     []MessagePart{ToolResultPart{ToolCallID: callID, Content: "Tool no response", IsError: true}},
		CreatedAt: time.Now().UTC(),
	}
}

func assistantFollowup(sessionID string) *Message {
	return &Message{
		ID:        uuid.NewString(),
		SessionID: sessionID,
		Role:      RoleAssistant,
		Parts:     []MessagePart{TextPart{Text: "Understood."}},
		CreatedAt: time.Now().UTC(),
	}
}
