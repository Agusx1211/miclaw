package tools

import (
	"context"
	"fmt"

	"github.com/agusx1211/miclaw/model"
)

func MainAgentTools() []Tool {
	tools := []Tool{
		ReadTool(),
		writeTool(),
		editTool(),
		patchTool(),
		grepTool(),
		globTool(),
		lsTool(),
		placeholder("exec", "placeholder exec tool", JSONSchema{Type: "object"}),
		placeholder("process", "placeholder process tool", JSONSchema{Type: "object"}),
		placeholder("cron", "placeholder cron tool", JSONSchema{Type: "object"}),
		placeholder("message", "placeholder message tool", JSONSchema{Type: "object"}),
		placeholder("agents_list", "placeholder agents_list tool", JSONSchema{Type: "object"}),
		placeholder("sessions_list", "placeholder sessions_list tool", JSONSchema{Type: "object"}),
		placeholder("sessions_history", "placeholder sessions_history tool", JSONSchema{Type: "object"}),
		placeholder("sessions_send", "placeholder sessions_send tool", JSONSchema{Type: "object"}),
		placeholder("sessions_spawn", "placeholder sessions_spawn tool", JSONSchema{Type: "object"}),
		placeholder("sessions_status", "placeholder sessions_status tool", JSONSchema{Type: "object"}),
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
