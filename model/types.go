package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type Role string

const (
	RoleAssistant Role = "assistant"
	RoleUser      Role = "user"
	RoleTool      Role = "tool"
)

type Session struct {
	ID               string    `json:"id"`
	ParentSessionID  string    `json:"parent_session_id"`
	Title            string    `json:"title"`
	MessageCount     int       `json:"message_count"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	SummaryMessageID string    `json:"summary_message_id"`
	Cost             float64   `json:"cost"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type Message struct {
	ID        string        `json:"id"`
	SessionID string        `json:"session_id"`
	Role      Role          `json:"role"`
	Parts     []MessagePart `json:"parts"`
	CreatedAt time.Time     `json:"created_at"`
}

type MessagePart interface {
	partTag() string
}

type TextPart struct {
	Text string `json:"text"`
}

func (TextPart) partTag() string { return "text" }

type ReasoningPart struct {
	Text string `json:"text"`
}

func (ReasoningPart) partTag() string { return "reasoning" }

type ToolCallPart struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Parameters json.RawMessage `json:"parameters"`
}

func (ToolCallPart) partTag() string { return "tool_call" }

type ToolResultPart struct {
	ToolCallID string `json:"tool_call_id"`
	Content    string `json:"content"`
	IsError    bool   `json:"is_error"`
}

func (ToolResultPart) partTag() string { return "tool_result" }

type FinishPart struct {
	Reason string `json:"reason"`
}

func (FinishPart) partTag() string { return "finish" }

type BinaryPart struct {
	MimeType string `json:"mime_type"`
	Data     []byte `json:"data"`
}

func (BinaryPart) partTag() string { return "binary" }

type ToolCall = ToolCallPart
type ToolResult = ToolResultPart

type messageJSON struct {
	ID        string            `json:"id"`
	SessionID string            `json:"session_id"`
	Role      Role              `json:"role"`
	Parts     []json.RawMessage `json:"parts"`
	CreatedAt time.Time         `json:"created_at"`
}

func (m Message) MarshalJSON() ([]byte, error) {
	w := messageJSON{
		ID:        m.ID,
		SessionID: m.SessionID,
		Role:      m.Role,
		Parts:     make([]json.RawMessage, 0, len(m.Parts)),
		CreatedAt: m.CreatedAt,
	}
	for _, p := range m.Parts {
		raw, err := marshalPart(p)
		if err != nil {
			return nil, err
		}
		w.Parts = append(w.Parts, raw)
	}
	return json.Marshal(w)
}

func (m *Message) UnmarshalJSON(b []byte) error {
	var w messageJSON
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	parts := make([]MessagePart, 0, len(w.Parts))
	for _, raw := range w.Parts {
		p, err := unmarshalPart(raw)
		if err != nil {
			return err
		}
		parts = append(parts, p)
	}
	*m = Message{
		ID:        w.ID,
		SessionID: w.SessionID,
		Role:      w.Role,
		Parts:     parts,
		CreatedAt: w.CreatedAt,
	}
	return nil
}

func marshalPart(p MessagePart) (json.RawMessage, error) {
	typ := p.partTag()
	switch v := p.(type) {
	case TextPart:
		return json.Marshal(struct {
			Type string `json:"type"`
			TextPart
		}{typ, v})
	case ReasoningPart:
		return json.Marshal(struct {
			Type string `json:"type"`
			ReasoningPart
		}{typ, v})
	case ToolCallPart:
		return json.Marshal(struct {
			Type string `json:"type"`
			ToolCallPart
		}{typ, v})
	case ToolResultPart:
		return json.Marshal(struct {
			Type string `json:"type"`
			ToolResultPart
		}{typ, v})
	case FinishPart:
		return json.Marshal(struct {
			Type string `json:"type"`
			FinishPart
		}{typ, v})
	case BinaryPart:
		return json.Marshal(struct {
			Type string `json:"type"`
			BinaryPart
		}{typ, v})
	}
	panic(fmt.Sprintf("unknown message part type: %T", p))
}

type partType struct {
	Type string `json:"type"`
}

func unmarshalPart(raw json.RawMessage) (MessagePart, error) {
	var pt partType
	if err := json.Unmarshal(raw, &pt); err != nil {
		return nil, err
	}
	if pt.Type == "" {
		return nil, errors.New("message part missing type")
	}
	switch pt.Type {
	case "text":
		var p TextPart
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		return p, nil
	case "reasoning":
		var p ReasoningPart
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		return p, nil
	case "tool_call":
		var p ToolCallPart
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		return p, nil
	case "tool_result":
		var p ToolResultPart
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		return p, nil
	case "finish":
		var p FinishPart
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		return p, nil
	case "binary":
		var p BinaryPart
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		return p, nil
	default:
		return nil, fmt.Errorf("unknown message part type: %s", pt.Type)
	}
}
