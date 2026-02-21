package tools

import (
	"context"
	"fmt"

	"github.com/agusx1211/miclaw/model"
)

func MainAgentTools() []Tool {
	tools := []Tool{
		placeholder("read", "placeholder read tool", JSONSchema{Type: "object"}),
		writeTool(),
		editTool(),
		patchTool(),
		placeholder("grep", "placeholder grep tool", JSONSchema{Type: "object"}),
		placeholder("glob", "placeholder glob tool", JSONSchema{Type: "object"}),
		placeholder("ls", "placeholder ls tool", JSONSchema{Type: "object"}),
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
	must(len(tools) == 20, "main agent tool count must be 20")
	must(hasUniqueToolNames(tools), "main agent tool names must be unique")
	return tools
}

func SubAgentTools() []Tool {
	tools := []Tool{
		placeholder("read", "placeholder read tool", JSONSchema{Type: "object"}),
		placeholder("grep", "placeholder grep tool", JSONSchema{Type: "object"}),
		placeholder("glob", "placeholder glob tool", JSONSchema{Type: "object"}),
		placeholder("ls", "placeholder ls tool", JSONSchema{Type: "object"}),
		placeholder("memory_search", "placeholder memory_search tool", JSONSchema{Type: "object"}),
		placeholder("memory_get", "placeholder memory_get tool", JSONSchema{Type: "object"}),
	}
	must(len(tools) == 6, "sub-agent tool count must be 6")
	must(hasUniqueToolNames(tools), "sub-agent tool names must be unique")
	return tools
}

// placeholder creates a stub tool with the given name.
func placeholder(name, desc string, params JSONSchema) Tool {
	must(name != "", "placeholder tool name must not be empty")
	must(params.Type != "", "placeholder schema type must not be empty")
	return tool{
		name:   name,
		desc:   desc,
		params: params,
		runFn: func(ctx context.Context, call model.ToolCallPart) (ToolResult, error) {
			panic(fmt.Sprintf("tool not implemented: %s", name))
		},
	}
}

func hasUniqueToolNames(tools []Tool) bool {
	must(tools != nil, "tool slice must not be nil")
	must(len(tools) >= 0, "tool slice length must be non-negative")

	seen := map[string]struct{}{}
	for _, t := range tools {
		name := t.Name()
		if _, ok := seen[name]; ok {
			return false
		}
		seen[name] = struct{}{}
	}
	must(len(seen) <= len(tools), "unique name count must not exceed tool count")
	must(len(seen) >= 0, "unique name count must be non-negative")
	return true
}
