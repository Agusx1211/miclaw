package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/agusx1211/miclaw/model"
	"github.com/agusx1211/miclaw/store"
	"github.com/google/uuid"
)

const (
	sessionsListDefaultLimit    = 20
	sessionsHistoryDefaultLimit = 50
)

type sessionsListParams struct {
	Limit  int
	Offset int
}

type sessionsHistoryParams struct {
	SessionID string
	Limit     int
	Offset    int
}

type sessionsSendParams struct {
	SessionID string
	Message   string
}

type sessionsStatusParams struct {
	SessionID string
}

func sessionsListTool(sessions store.SessionStore) Tool {
	return tool{
		name: "sessions_list",
		desc: "List sessions",
		params: JSONSchema{
			Type: "object",
			Properties: map[string]JSONSchema{
				"limit":  {Type: "integer", Desc: "Maximum number of sessions to return (default: 20)"},
				"offset": {Type: "integer", Desc: "Number of sessions to skip (default: 0)"},
			},
		},
		runFn: func(_ context.Context, call model.ToolCallPart) (ToolResult, error) {
			p, err := parseSessionsListParams(call.Parameters)
			if err != nil {
				return ToolResult{}, err
			}
			v, err := sessions.List(p.Limit, p.Offset)
			if err != nil {
				return ToolResult{}, err
			}

			return ToolResult{Content: formatSessionsList(v)}, nil
		},
	}
}

func sessionsHistoryTool(sessions store.SessionStore, messages store.MessageStore) Tool {
	return tool{
		name: "sessions_history",
		desc: "Get message history for a session",
		params: JSONSchema{
			Type:     "object",
			Required: []string{"session_id"},
			Properties: map[string]JSONSchema{
				"session_id": {Type: "string", Desc: "Session ID"},
				"limit":      {Type: "integer", Desc: "Maximum number of messages to return (default: 50)"},
				"offset":     {Type: "integer", Desc: "Number of messages to skip (default: 0)"},
			},
		},
		runFn: func(_ context.Context, call model.ToolCallPart) (ToolResult, error) {
			p, err := parseSessionsHistoryParams(call.Parameters)
			if err != nil {
				return ToolResult{}, err
			}
			if _, err := sessions.Get(p.SessionID); err != nil {
				return ToolResult{}, err
			}
			v, err := messages.ListBySession(p.SessionID, p.Limit, p.Offset)
			if err != nil {
				return ToolResult{}, err
			}

			return ToolResult{Content: formatSessionsHistory(v)}, nil
		},
	}
}

func sessionsSendTool(sessions store.SessionStore, messages store.MessageStore) Tool {
	return tool{
		name: "sessions_send",
		desc: "Send a user message to a session",
		params: JSONSchema{
			Type:     "object",
			Required: []string{"session_id", "message"},
			Properties: map[string]JSONSchema{
				"session_id": {Type: "string", Desc: "Session ID"},
				"message":    {Type: "string", Desc: "Message content"},
			},
		},
		runFn: func(_ context.Context, call model.ToolCallPart) (ToolResult, error) {
			p, err := parseSessionsSendParams(call.Parameters)
			if err != nil {
				return ToolResult{}, err
			}
			session, err := sessions.Get(p.SessionID)
			if err != nil {
				return ToolResult{}, err
			}
			now := time.Now().UTC()
			msg := &model.Message{
				ID:        uuid.NewString(),
				SessionID: p.SessionID,
				Role:      model.RoleUser,
				Parts:     []model.MessagePart{model.TextPart{Text: p.Message}},
				CreatedAt: now,
			}
			if err := messages.Create(msg); err != nil {
				return ToolResult{}, err
			}
			n, err := messages.CountBySession(p.SessionID)
			if err != nil {
				return ToolResult{}, err
			}
			session.MessageCount = n
			session.UpdatedAt = now
			if err := sessions.Update(session); err != nil {
				return ToolResult{}, err
			}

			return ToolResult{Content: fmt.Sprintf("message sent to session %s", p.SessionID)}, nil
		},
	}
}

func sessionsStatusTool(sessions store.SessionStore, messages store.MessageStore) Tool {
	return tool{
		name: "sessions_status",
		desc: "Return status for one session",
		params: JSONSchema{
			Type:     "object",
			Required: []string{"session_id"},
			Properties: map[string]JSONSchema{
				"session_id": {Type: "string", Desc: "Session ID"},
			},
		},
		runFn: func(_ context.Context, call model.ToolCallPart) (ToolResult, error) {
			p, err := parseSessionsStatusParams(call.Parameters)
			if err != nil {
				return ToolResult{}, err
			}
			session, err := sessions.Get(p.SessionID)
			if err != nil {
				return ToolResult{}, err
			}
			n, err := messages.CountBySession(p.SessionID)
			if err != nil {
				return ToolResult{}, err
			}

			return ToolResult{Content: formatSessionStatus(session, n)}, nil
		},
	}
}

func agentsListTool(modelName string, isActive func() bool) Tool {
	return tool{
		name:   "agents_list",
		desc:   "List available agents",
		params: JSONSchema{Type: "object"},
		runFn: func(_ context.Context, call model.ToolCallPart) (ToolResult, error) {
			if err := expectEmptyObject(call.Parameters); err != nil {
				return ToolResult{}, err
			}
			status := "idle"
			if isActive() {
				status = "active"
			}

			return ToolResult{Content: fmt.Sprintf("name=main\tmodel=%s\tstatus=%s", modelName, status)}, nil
		},
	}
}

func parseSessionsListParams(raw json.RawMessage) (sessionsListParams, error) {
	var input struct {
		Limit  *int `json:"limit"`
		Offset *int `json:"offset"`
	}
	if err := unmarshalObject(raw, &input); err != nil {
		return sessionsListParams{}, err
	}
	out := sessionsListParams{Limit: sessionsListDefaultLimit}
	if input.Limit != nil {
		out.Limit = *input.Limit
	}
	if input.Offset != nil {
		out.Offset = *input.Offset
	}
	return out, nil
}

func parseSessionsHistoryParams(raw json.RawMessage) (sessionsHistoryParams, error) {
	var input struct {
		SessionID *string `json:"session_id"`
		Limit     *int    `json:"limit"`
		Offset    *int    `json:"offset"`
	}
	if err := unmarshalObject(raw, &input); err != nil {
		return sessionsHistoryParams{}, err
	}
	if input.SessionID == nil || strings.TrimSpace(*input.SessionID) == "" {
		return sessionsHistoryParams{}, errors.New("session_id is required")
	}
	out := sessionsHistoryParams{SessionID: *input.SessionID, Limit: sessionsHistoryDefaultLimit}
	if input.Limit != nil {
		out.Limit = *input.Limit
	}
	if input.Offset != nil {
		out.Offset = *input.Offset
	}
	return out, nil
}

func parseSessionsSendParams(raw json.RawMessage) (sessionsSendParams, error) {
	var input struct {
		SessionID *string `json:"session_id"`
		Message   *string `json:"message"`
	}
	if err := unmarshalObject(raw, &input); err != nil {
		return sessionsSendParams{}, err
	}
	if input.SessionID == nil || strings.TrimSpace(*input.SessionID) == "" {
		return sessionsSendParams{}, errors.New("session_id is required")
	}
	if input.Message == nil {
		return sessionsSendParams{}, errors.New("message is required")
	}
	return sessionsSendParams{SessionID: *input.SessionID, Message: *input.Message}, nil
}

func parseSessionsStatusParams(raw json.RawMessage) (sessionsStatusParams, error) {
	var input struct {
		SessionID *string `json:"session_id"`
	}
	if err := unmarshalObject(raw, &input); err != nil {
		return sessionsStatusParams{}, err
	}
	if input.SessionID == nil || strings.TrimSpace(*input.SessionID) == "" {
		return sessionsStatusParams{}, errors.New("session_id is required")
	}
	return sessionsStatusParams{SessionID: *input.SessionID}, nil
}

func unmarshalObject(raw json.RawMessage, out any) error {
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	return json.Unmarshal(raw, out)
}

func expectEmptyObject(raw json.RawMessage) error {
	var input map[string]any
	if err := unmarshalObject(raw, &input); err != nil {
		return err
	}
	if len(input) != 0 {
		return errors.New("agents_list expects no parameters")
	}
	return nil
}

func formatSessionsList(items []*model.Session) string {
	lines := make([]string, 0, len(items))
	for _, s := range items {
		line := fmt.Sprintf(
			"%s\t%s\tmessages=%d\tcreated=%s\tupdated=%s",
			s.ID,
			s.Title,
			s.MessageCount,
			s.CreatedAt.Format(time.RFC3339Nano),
			s.UpdatedAt.Format(time.RFC3339Nano),
		)
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func formatSessionsHistory(items []*model.Message) string {
	lines := make([]string, 0, len(items))
	for _, msg := range items {
		lines = append(lines, fmt.Sprintf("%s\t%s", msg.Role, summarizeMessageParts(msg.Parts)))
	}
	return strings.Join(lines, "\n")
}

func summarizeMessageParts(parts []model.MessagePart) string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		out = append(out, summarizePart(part))
	}
	return strings.Join(out, " | ")
}

func summarizePart(part model.MessagePart) string {
	switch v := part.(type) {
	case model.TextPart:
		return v.Text
	case model.ReasoningPart:
		return "[reasoning] " + v.Text
	case model.ToolCallPart:
		return fmt.Sprintf("[tool_call name=%s id=%s]", v.Name, v.ID)
	case model.ToolResultPart:
		return fmt.Sprintf("[tool_result call=%s error=%t] %s", v.ToolCallID, v.IsError, v.Content)
	case model.FinishPart:
		return "[finish reason=" + v.Reason + "]"
	case model.BinaryPart:
		return fmt.Sprintf("[binary mime=%s bytes=%d]", v.MimeType, len(v.Data))
	}
	panic("unknown message part")
}

func formatSessionStatus(session *model.Session, messageCount int) string {
	return fmt.Sprintf(
		"id=%s\ntitle=%s\nmessage_count=%d\nprompt_tokens=%d\ncompletion_tokens=%d\ncost=%.6f\nupdated_at=%s",
		session.ID,
		session.Title,
		messageCount,
		session.PromptTokens,
		session.CompletionTokens,
		session.Cost,
		session.UpdatedAt.Format(time.RFC3339Nano),
	)
}
