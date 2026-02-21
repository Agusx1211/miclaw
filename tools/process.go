package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"syscall"

	"github.com/agusx1211/miclaw/model"
)

const (
	processActionStatus = "status"
	processActionInput  = "input"
	processActionSignal = "signal"
	processActionPoll   = "poll"
)

type processParams struct {
	Action string
	PID    int
	Data   string
	Signal string
}

func processTool() Tool {
	return tool{
		name: "process",
		desc: "Manage background processes started by exec",
		params: JSONSchema{
			Type: "object",
			Properties: map[string]JSONSchema{
				"action": {
					Type: "string",
					Desc: "Action to perform: status, input, signal, poll",
					Enum: []string{
						processActionStatus,
						processActionInput,
						processActionSignal,
						processActionPoll,
					},
				},
				"pid": {
					Type: "integer",
					Desc: "Process ID",
				},
				"data": {
					Type: "string",
					Desc: "Data to write to process stdin for input action",
				},
				"signal": {
					Type: "string",
					Desc: "Signal name for signal action: SIGTERM, SIGINT, SIGKILL",
					Enum: []string{"SIGTERM", "SIGINT", "SIGKILL"},
				},
			},
			Required: []string{"action", "pid"},
		},
		runFn: runProcess,
	}
}

func runProcess(_ context.Context, call model.ToolCallPart) (ToolResult, error) {
	params, err := parseProcessParams(call.Parameters)
	if err != nil {
		return ToolResult{Content: err.Error(), IsError: true}, nil
	}
	switch params.Action {
	case processActionStatus:
		running, code, runtime, err := execProcessManager.Status(params.PID)
		if err != nil {
			return ToolResult{Content: err.Error(), IsError: true}, nil
		}
		return ToolResult{Content: formatProcessStatus(running, code, runtime)}, nil
	case processActionInput:
		err := execProcessManager.SendInput(params.PID, params.Data)
		if err != nil {
			return ToolResult{Content: err.Error(), IsError: true}, nil
		}
		return ToolResult{Content: fmt.Sprintf("sent input to process %d", params.PID)}, nil
	case processActionSignal:
		sig, err := parseSignalName(params.Signal)
		if err != nil {
			return ToolResult{Content: err.Error(), IsError: true}, nil
		}
		err = execProcessManager.Signal(params.PID, sig)
		if err != nil {
			return ToolResult{Content: err.Error(), IsError: true}, nil
		}
		return ToolResult{Content: fmt.Sprintf("sent %s to process %d", params.Signal, params.PID)}, nil
	default:
		output, err := execProcessManager.Poll(params.PID)
		if err != nil {
			return ToolResult{Content: err.Error(), IsError: true}, nil
		}
		return ToolResult{Content: output}, nil
	}
}

func parseProcessParams(raw json.RawMessage) (processParams, error) {
	var input struct {
		Action *string `json:"action"`
		PID    *int    `json:"pid"`
		Data   *string `json:"data"`
		Signal *string `json:"signal"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return processParams{}, fmt.Errorf("parse process parameters: %v", err)
	}
	if input.Action == nil || strings.TrimSpace(*input.Action) == "" {
		return processParams{}, errors.New("process parameter action is required")
	}
	if input.PID == nil {
		return processParams{}, errors.New("process parameter pid is required")
	}
	a := strings.TrimSpace(*input.Action)
	if a != processActionStatus && a != processActionInput && a != processActionSignal && a != processActionPoll {
		return processParams{}, errors.New("process action must be one of status, input, signal, poll")
	}
	params := processParams{Action: a, PID: *input.PID}
	if input.Data != nil {
		params.Data = *input.Data
	}
	if input.Signal != nil {
		params.Signal = strings.ToUpper(strings.TrimSpace(*input.Signal))
	}
	if params.Action == processActionInput && input.Data == nil {
		return processParams{}, errors.New("process parameter data is required for input action")
	}
	if params.Action == processActionSignal {
		if input.Signal == nil || params.Signal == "" {
			return processParams{}, errors.New("process parameter signal is required for signal action")
		}
		if _, err := parseSignalName(params.Signal); err != nil {
			return processParams{}, err
		}
	}
	return params, nil
}

func parseSignalName(name string) (syscall.Signal, error) {
	switch name {
	case "SIGTERM":
		return syscall.SIGTERM, nil
	case "SIGINT":
		return syscall.SIGINT, nil
	case "SIGKILL":
		return syscall.SIGKILL, nil
	default:
		return 0, errors.New("process signal must be SIGTERM, SIGINT, or SIGKILL")
	}
}

func formatProcessStatus(running bool, code int, runtime string) string {
	state := "completed"
	if running {
		state = "running"
	}
	return fmt.Sprintf(
		"state: %s\nrunning: %t\nexit code: %d\nruntime: %s",
		state,
		running,
		code,
		runtime,
	)
}
