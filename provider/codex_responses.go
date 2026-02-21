package provider

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/agusx1211/miclaw/model"
)

type codexResponsesRequest struct {
	Model             string               `json:"model"`
	Instructions      string               `json:"instructions"`
	Input             []codexResponseInput `json:"input"`
	Tools             []codexResponseTool  `json:"tools,omitempty"`
	ToolChoice        string               `json:"tool_choice"`
	ParallelToolCalls bool                 `json:"parallel_tool_calls"`
	Stream            bool                 `json:"stream"`
	Store             bool                 `json:"store"`
	Reasoning         *codexReasoning      `json:"reasoning,omitempty"`
}

type codexResponseInput struct {
	Type      string              `json:"type"`
	Role      string              `json:"role,omitempty"`
	Content   []codexResponseText `json:"content,omitempty"`
	CallID    string              `json:"call_id,omitempty"`
	Name      string              `json:"name,omitempty"`
	Arguments string              `json:"arguments,omitempty"`
	Output    *string             `json:"output,omitempty"`
}

type codexResponseText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type codexResponseTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

func marshalCodexResponsesRequest(
	modelID string,
	effort string,
	messages []model.Message,
	tools []ToolDef,
) ([]byte, error) {
	instructions, inputMessages := codexResponseInstructions(messages)
	req := codexResponsesRequest{
		Model:             modelID,
		Instructions:      instructions,
		Input:             encodeResponsesInput(inputMessages),
		Tools:             encodeResponsesTools(tools),
		ToolChoice:        "auto",
		ParallelToolCalls: true,
		Stream:            true,
		Store:             false,
	}
	if isCodexResponsesEffort(effort) {
		req.Reasoning = &codexReasoning{Effort: effort}
	}
	return json.Marshal(req)
}

func codexResponseInstructions(messages []model.Message) (string, []model.Message) {
	if len(messages) == 0 {
		return "You are a helpful assistant.", nil
	}
	first := messages[0]
	if strings.HasPrefix(first.ID, "system-") {
		txt := strings.TrimSpace(messageTextForResponses(first))
		if txt != "" {
			return txt, messages[1:]
		}
	}
	return "You are a helpful assistant.", messages
}

func isCodexResponsesEffort(effort string) bool {
	switch strings.TrimSpace(effort) {
	case "low", "medium", "high", "xhigh":
		return true
	default:
		return false
	}
}

func encodeResponsesInput(messages []model.Message) []codexResponseInput {
	out := make([]codexResponseInput, 0, len(messages))
	for _, msg := range messages {
		out = append(out, encodeResponsesMessage(msg)...)
	}
	return out
}

func encodeResponsesMessage(msg model.Message) []codexResponseInput {
	out := make([]codexResponseInput, 0, len(msg.Parts)+1)
	text := messageTextForResponses(msg)
	if text != "" && (msg.Role == model.RoleUser || msg.Role == model.RoleAssistant) {
		contentType := "input_text"
		if msg.Role == model.RoleAssistant {
			contentType = "output_text"
		}
		out = append(out, codexResponseInput{
			Type:    "message",
			Role:    string(msg.Role),
			Content: []codexResponseText{{Type: contentType, Text: text}},
		})
	}
	for _, part := range msg.Parts {
		switch p := part.(type) {
		case model.ToolCallPart:
			args := strings.TrimSpace(string(p.Parameters))
			if args == "" {
				args = "{}"
			}
			out = append(out, codexResponseInput{
				Type:      "function_call",
				CallID:    p.ID,
				Name:      p.Name,
				Arguments: args,
			})
		case model.ToolResultPart:
			out = append(out, codexResponseInput{
				Type:   "function_call_output",
				CallID: p.ToolCallID,
				Output: stringRef(p.Content),
			})
		}
	}
	return out
}

func stringRef(v string) *string {
	return &v
}

func messageTextForResponses(msg model.Message) string {
	parts := make([]string, 0, len(msg.Parts))
	for _, part := range msg.Parts {
		switch p := part.(type) {
		case model.TextPart:
			if p.Text != "" {
				parts = append(parts, p.Text)
			}
		case model.ReasoningPart:
			if p.Text != "" {
				parts = append(parts, p.Text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

func encodeResponsesTools(tools []ToolDef) []codexResponseTool {
	out := make([]codexResponseTool, 0, len(tools))
	for _, tool := range tools {
		out = append(out, codexResponseTool{
			Type:        "function",
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  tool.Parameters,
		})
	}
	return out
}

func chatgptAccountID(token string) string {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Auth struct {
			ChatGPTAccountID string `json:"chatgpt_account_id"`
		} `json:"https://api.openai.com/auth"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	return strings.TrimSpace(claims.Auth.ChatGPTAccountID)
}
