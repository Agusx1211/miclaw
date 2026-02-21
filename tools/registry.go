package tools

import (
	"context"

	"github.com/agusx1211/miclaw/config"
	"github.com/agusx1211/miclaw/memory"
	"github.com/agusx1211/miclaw/provider"
	"github.com/agusx1211/miclaw/store"
)

type MainToolDeps struct {
	Sessions    store.SessionStore
	Messages    store.MessageStore
	Provider    provider.LLMProvider
	Sandbox     config.SandboxConfig
	Memory      *memory.Store
	Embed       *memory.EmbedClient
	Scheduler   *Scheduler
	SendMessage func(ctx context.Context, recipient, content string) error
	Model       string
	IsActive    func() bool
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
		agentsListTool(deps.Model, deps.IsActive),
		sessionsListTool(deps.Sessions),
		sessionsHistoryTool(deps.Sessions, deps.Messages),
		sessionsSendTool(deps.Sessions, deps.Messages),
		sessionsSpawnTool(deps.Sessions, deps.Messages, deps.Provider, deps.Memory, deps.Embed),
		sessionsStatusTool(deps.Sessions, deps.Messages),
		MemorySearchTool(deps.Memory, deps.Embed),
		MemoryGetTool(deps.Memory),
		subagentsTool(),
	}

	return tools
}

func SubAgentTools(store *memory.Store, embedClient *memory.EmbedClient) []Tool {
	tools := []Tool{
		ReadTool(),
		grepTool(),
		globTool(),
		lsTool(),
		MemorySearchTool(store, embedClient),
		MemoryGetTool(store),
	}

	return tools
}
