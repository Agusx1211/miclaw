package tools

import (
	"context"
	"fmt"

	"github.com/agusx1211/miclaw/model"
	"github.com/agusx1211/miclaw/store"
)

type MainToolDeps struct {
	Sessions store.SessionStore
	Messages store.MessageStore
	Model    string
	IsActive func() bool
}

func MainAgentTools(deps MainToolDeps) []Tool {
	tools := []Tool{
		ReadTool(),
		writeTool(),
		editTool(),
		patchTool(),
		grepTool(),
		globTool(),
		lsTool(),
		execTool(),
		placeholder("process", "placeholder process tool", JSONSchema{Type: "object"}),
		placeholder("cron", "placeholder cron tool", JSONSchema{Type: "object"}),
		placeholder("message", "placeholder message tool", JSONSchema{Type: "object"}),
		agentsListTool(deps.Model, deps.IsActive),
		sessionsListTool(deps.Sessions),
		sessionsHistoryTool(deps.Sessions, deps.Messages),
		sessionsSendTool(deps.Sessions, deps.Messages),
		placeholder("sessions_spawn", "placeholder sessions_spawn tool", JSONSchema{Type: "object"}),
		sessionsStatusTool(deps.Sessions, deps.Messages),
		placeholder("memory_search", "placeholder memory_search tool", JSONSchema{Type: "object"}),
		placeholder("memory_get", "placeholder memory_get tool", JSONSchema{Type: "object"}),
		placeholder("subagents", "placeholder subagents tool", JSONSchema{Type: "object"}),
	}

	return tools
}

func SubAgentTools() []Tool {
	tools := []Tool{
		ReadTool(),
		grepTool(),
		globTool(),
		lsTool(),
		placeholder("memory_search", "placeholder memory_search tool", JSONSchema{Type: "object"}),
		placeholder("memory_get", "placeholder memory_get tool", JSONSchema{Type: "object"}),
	}

	return tools
}

// placeholder creates a stub tool with the given name.
func placeholder(name, desc string, params JSONSchema) Tool {

	return tool{
		name:   name,
		desc:   desc,
		params: params,
		runFn: func(ctx context.Context, call model.ToolCallPart) (ToolResult, error) {
			panic(fmt.Sprintf("tool not implemented: %s", name))
		},
	}
}
