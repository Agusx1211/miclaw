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
	Target  string
	Content string
}

func messageTool(sendMessage func(ctx context.Context, recipient, content string) error) Tool {
	return tool{
		name: "message",
		desc: "Send a message to a recipient",
		params: JSONSchema{
			Type:     "object",
			Required: []string{"target", "content"},
			Properties: map[string]JSONSchema{
				"target": {
					Type: "string",
					Desc: "Message target (for example: signal:+15551234567)",
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
			channel, recipient, err := parseMessageTarget(params.Target)
			if err != nil {
				return ToolResult{IsError: true, Content: err.Error()}, nil
			}
			if channel != "signal" {
				return ToolResult{IsError: true, Content: fmt.Sprintf("unsupported channel: %s", channel)}, nil
			}
			if err := sendMessage(ctx, recipient, params.Content); err != nil {
				return ToolResult{IsError: true, Content: err.Error()}, nil
			}
			return ToolResult{Content: fmt.Sprintf("message sent to %s", params.Target)}, nil
		},
	}
}

func parseMessageParams(raw json.RawMessage) (messageParams, error) {
	var input struct {
		Target  *string `json:"target"`
		Content *string `json:"content"`
	}
	if err := unmarshalObject(raw, &input); err != nil {
		return messageParams{}, fmt.Errorf("parse message parameters: %v", err)
	}
	if input.Target == nil || strings.TrimSpace(*input.Target) == "" {
		return messageParams{}, errors.New("target is required")
	}
	if input.Content == nil || strings.TrimSpace(*input.Content) == "" {
		return messageParams{}, errors.New("content is required")
	}
	return messageParams{
		Target:  strings.TrimSpace(*input.Target),
		Content: strings.TrimSpace(*input.Content),
	}, nil
}

func parseMessageTarget(raw string) (string, string, error) {
	parts := strings.SplitN(raw, ":", 2)
	if len(parts) != 2 {
		return "", "", errors.New("target must include channel and address, e.g. signal:+15551234567")
	}
	return parts[0], parts[1], nil
}
