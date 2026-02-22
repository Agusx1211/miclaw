package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/agusx1211/miclaw/model"
)

type typingParams struct {
	To       string
	State    string
	Duration time.Duration
}

func typingTool(
	startTyping func(ctx context.Context, to string, duration time.Duration) error,
	stopTyping func(ctx context.Context, to string) error,
) Tool {
	return tool{
		name: "typing",
		desc: "Control typing indicator (on/off)",
		params: JSONSchema{
			Type:     "object",
			Required: []string{"to"},
			Properties: map[string]JSONSchema{
				"to": {
					Type: "string",
					Desc: "Message target (for example: signal:dm:user-uuid)",
				},
				"state": {
					Type: "string",
					Desc: "Typing state: on or off",
				},
				"seconds": {
					Type: "integer",
					Desc: "Optional timeout when state is on",
				},
			},
		},
		runFn: func(ctx context.Context, call model.ToolCallPart) (ToolResult, error) {
			params, err := parseTypingParams(call.Parameters)
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
			if params.State == "off" {
				if err := stopTyping(ctx, params.To); err != nil {
					return ToolResult{IsError: true, Content: err.Error()}, nil
				}
				return ToolResult{Content: fmt.Sprintf("typing off for %s", params.To)}, nil
			}
			if err := startTyping(ctx, params.To, params.Duration); err != nil {
				return ToolResult{IsError: true, Content: err.Error()}, nil
			}
			return ToolResult{Content: fmt.Sprintf("typing on for %s", params.To)}, nil
		},
	}
}

func parseTypingParams(raw json.RawMessage) (typingParams, error) {
	var input struct {
		To      *string `json:"to"`
		State   *string `json:"state"`
		Seconds *int    `json:"seconds"`
	}
	if err := unmarshalObject(raw, &input); err != nil {
		return typingParams{}, fmt.Errorf("parse typing parameters: %v", err)
	}
	if input.To == nil || strings.TrimSpace(*input.To) == "" {
		return typingParams{}, errors.New("to is required")
	}
	state := "on"
	if input.State != nil {
		v := strings.ToLower(strings.TrimSpace(*input.State))
		if v != "" {
			state = v
		}
	}
	if state != "on" && state != "off" {
		return typingParams{}, errors.New("state must be on or off")
	}
	duration := time.Duration(0)
	if input.Seconds != nil {
		if state != "on" {
			return typingParams{}, errors.New("seconds is only valid when state is on")
		}
		if *input.Seconds < 1 {
			return typingParams{}, errors.New("seconds must be >= 1")
		}
		duration = time.Duration(*input.Seconds) * time.Second
	}
	return typingParams{
		To:       strings.TrimSpace(*input.To),
		State:    state,
		Duration: duration,
	}, nil
}
