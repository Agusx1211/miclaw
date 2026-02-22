package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/agusx1211/miclaw/model"
)

type messageParams struct {
	To      string
	Content string
}

func messageTool(sendMessage func(ctx context.Context, to, content string) error) Tool {
	return tool{
		name: "message",
		desc: "Send a message to a recipient",
		params: JSONSchema{
			Type:     "object",
			Required: []string{"to", "content"},
			Properties: map[string]JSONSchema{
				"to": {
					Type: "string",
					Desc: "Message target (for example: signal:dm:user-uuid or signal:group:group-id)",
				},
				"content": {
					Type: "string",
					Desc: "Message content to send",
				},
			},
		},
		runFn: func(ctx context.Context, call model.ToolCallPart) (ToolResult, error) {
			params, err := parseMessageParams(call.Parameters)
			if err != nil {
				return ToolResult{IsError: true, Content: err.Error()}, nil
			}
			channel, _, err := parseMessageTarget(params.To)
			if err != nil {
				return ToolResult{IsError: true, Content: err.Error()}, nil
			}
			if channel != "signal" {
				return ToolResult{IsError: true, Content: fmt.Sprintf("unsupported channel: %s", channel)}, nil
			}
			if err := sendMessage(ctx, params.To, params.Content); err != nil {
				return ToolResult{IsError: true, Content: err.Error()}, nil
			}
			return ToolResult{Content: fmt.Sprintf("message sent to %s", params.To)}, nil
		},
	}
}

func parseMessageParams(raw json.RawMessage) (messageParams, error) {
	var input struct {
		To      *string `json:"to"`
		Content *string `json:"content"`
	}
	if err := unmarshalObject(raw, &input); err != nil {
		return messageParams{}, fmt.Errorf("parse message parameters: %v", err)
	}
	if input.To == nil || strings.TrimSpace(*input.To) == "" {
		return messageParams{}, errors.New("to is required")
	}
	if input.Content == nil || strings.TrimSpace(*input.Content) == "" {
		return messageParams{}, errors.New("content is required")
	}
	return messageParams{
		To:      strings.TrimSpace(*input.To),
		Content: strings.TrimSpace(*input.Content),
	}, nil
}

func parseMessageTarget(raw string) (string, string, error) {
	parts := strings.SplitN(raw, ":", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[1]) == "" {
		return "", "", errors.New("to must include channel and address, e.g. signal:dm:user-uuid")
	}
	return parts[0], parts[1], nil
}
