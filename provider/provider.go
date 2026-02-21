package provider

import (
	"context"
	"encoding/json"

	"github.com/agusx1211/miclaw/agent"
)

type LLMProvider interface {
	Stream(ctx context.Context, messages []agent.Message, tools []ToolDef) <-chan ProviderEvent
	Model() ModelInfo
}

type ProviderEventType string

const (
	EventContentDelta  ProviderEventType = "content_delta"
	EventThinkingDelta ProviderEventType = "thinking_delta"
	EventToolUseStart  ProviderEventType = "tool_use_start"
	EventToolUseDelta  ProviderEventType = "tool_use_delta"
	EventToolUseStop   ProviderEventType = "tool_use_stop"
	EventComplete      ProviderEventType = "complete"
	EventError         ProviderEventType = "error"
)

type ProviderEvent struct {
	Type       ProviderEventType
	Delta      string
	ToolCallID string
	ToolName   string
	Usage      *UsageInfo
	Error      error
}

type UsageInfo struct {
	PromptTokens     int
	CompletionTokens int
	CacheReadTokens  int
	CacheWriteTokens int
}

type ModelInfo struct {
	ID                 string
	Name               string
	ContextWindow      int
	MaxOutput          int
	CostPerInputToken  float64
	CostPerOutputToken float64
}

type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}
