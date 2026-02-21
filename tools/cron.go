package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/agusx1211/miclaw/model"
)

const (
	cronActionList   = "list"
	cronActionAdd    = "add"
	cronActionRemove = "remove"
)

type cronParams struct {
	Action     string
	ID         string
	Expression string
	Prompt     string
}

type cronRawParams struct {
	Action     *string `json:"action"`
	ID         *string `json:"id"`
	Expression *string `json:"expression"`
	Prompt     *string `json:"prompt"`
}

func CronTool(scheduler *Scheduler) Tool {
	return tool{
		name: "cron",
		desc: "Schedule recurring prompts",
		params: JSONSchema{
			Type:     "object",
			Required: []string{"action"},
			Properties: map[string]JSONSchema{
				"action": {
					Type: "string",
					Enum: []string{cronActionList, cronActionAdd, cronActionRemove},
					Desc: "Action to perform: list, add, remove",
				},
				"id":         {Type: "string", Desc: "Cron job ID for remove"},
				"expression": {Type: "string", Desc: "Cron expression"},
				"prompt":     {Type: "string", Desc: "Prompt text to inject"},
			},
		},
		runFn: func(_ context.Context, call model.ToolCallPart) (ToolResult, error) {
			params, err := parseCronParams(call.Parameters)
			if err != nil {
				return ToolResult{IsError: true, Content: err.Error()}, nil
			}
			switch params.Action {
			case cronActionList:
				jobs, err := scheduler.ListJobs()
				if err != nil {
					return ToolResult{IsError: true, Content: err.Error()}, nil
				}
				raw, err := json.Marshal(jobs)
				if err != nil {
					return ToolResult{IsError: true, Content: err.Error()}, nil
				}
				return ToolResult{Content: string(raw)}, nil
			case cronActionAdd:
				id, err := scheduler.AddJob(params.Expression, params.Prompt)
				if err != nil {
					return ToolResult{IsError: true, Content: err.Error()}, nil
				}
				raw, _ := json.Marshal(map[string]string{"id": id})
				return ToolResult{Content: string(raw)}, nil
			default:
				err := scheduler.RemoveJob(params.ID)
				if err != nil {
					return ToolResult{IsError: true, Content: err.Error()}, nil
				}
				return ToolResult{Content: fmt.Sprintf("removed cron job %s", params.ID)}, nil
			}
		},
	}
}

func parseCronParams(raw json.RawMessage) (cronParams, error) {
	var input cronRawParams
	if err := unmarshalObject(raw, &input); err != nil {
		return cronParams{}, err
	}
	if input.Action == nil || strings.TrimSpace(*input.Action) == "" {
		return cronParams{}, errors.New("action is required")
	}
	action := strings.TrimSpace(*input.Action)
	if action != cronActionList && action != cronActionAdd && action != cronActionRemove {
		return cronParams{}, errors.New("invalid action")
	}
	if action == cronActionAdd {
		if input.Expression == nil {
			return cronParams{}, errors.New("expression is required")
		}
		if input.Prompt == nil {
			return cronParams{}, errors.New("prompt is required")
		}
	}
	if action == cronActionRemove && input.ID == nil {
		return cronParams{}, errors.New("id is required")
	}
	p := cronParams{Action: action}
	if input.ID != nil {
		p.ID = *input.ID
	}
	if input.Expression != nil {
		p.Expression = *input.Expression
	}
	if input.Prompt != nil {
		p.Prompt = *input.Prompt
	}
	return p, nil
}
