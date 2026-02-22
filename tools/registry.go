package tools

import (
	"context"
	"time"

	"github.com/agusx1211/miclaw/config"
	"github.com/agusx1211/miclaw/memory"
)

type MainToolDeps struct {
	Sandbox     config.SandboxConfig
	Memory      *memory.Store
	Embed       *memory.EmbedClient
	Scheduler   *Scheduler
	SendMessage func(ctx context.Context, to, content string) error
	StartTyping func(ctx context.Context, to string, duration time.Duration) error
	StopTyping  func(ctx context.Context, to string) error
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
		MemorySearchTool(deps.Memory, deps.Embed),
		MemoryGetTool(deps.Memory),
	}
	if deps.StartTyping != nil && deps.StopTyping != nil {
		tools = append(tools, typingTool(deps.StartTyping, deps.StopTyping))
	}

	return tools
}
