package tools

import (
	"context"

	"github.com/agusx1211/miclaw/config"
	"github.com/agusx1211/miclaw/memory"
)

type MainToolDeps struct {
	Sandbox     config.SandboxConfig
	Memory      *memory.Store
	Embed       *memory.EmbedClient
	Scheduler   *Scheduler
	SendMessage func(ctx context.Context, to, content string) error
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
		execToolWithSandbox(deps.Sandbox),
		processTool(),
		CronTool(deps.Scheduler),
		messageTool(deps.SendMessage),
		sleepTool(),
		MemorySearchTool(deps.Memory, deps.Embed),
		MemoryGetTool(deps.Memory),
	}

	return tools
}

func BridgeableToolNames() map[string]bool {
	return map[string]bool{
		"read":        true,
		"write":       true,
		"edit":        true,
		"apply_patch": true,
		"grep":        true,
		"glob":        true,
		"ls":          true,
		"exec":        true,
	}
}

func BridgeTools() []Tool {
	return []Tool{
		ReadTool(),
		writeTool(),
		editTool(),
		patchTool(),
		grepTool(),
		globTool(),
		lsTool(),
		execTool(),
	}
}
