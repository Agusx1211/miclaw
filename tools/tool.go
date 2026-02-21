package tools

import (
	"context"
	"fmt"

	"github.com/agusx1211/miclaw/agent"
)

type Tool interface {
	Name() string
	Description() string
	Parameters() JSONSchema
	Run(ctx context.Context, call agent.ToolCallPart) (ToolResult, error)
}

type JSONSchema struct {
	Type       string                `json:"type"`
	Properties map[string]JSONSchema `json:"properties,omitempty"`
	Required   []string              `json:"required,omitempty"`
	Items      *JSONSchema           `json:"items,omitempty"`
	Enum       []string              `json:"enum,omitempty"`
	Desc       string                `json:"description,omitempty"`
}

type ToolResult struct {
	Content string
	IsError bool
}

type tool struct {
	name string
	desc string
	params JSONSchema
	runFn func(ctx context.Context, call agent.ToolCallPart) (ToolResult, error)
}

func (t tool) Name() string          { return t.name }
func (t tool) Description() string   { return t.desc }
func (t tool) Parameters() JSONSchema { return t.params }
func (t tool) Run(ctx context.Context, call agent.ToolCallPart) (ToolResult, error) {
	if t.runFn == nil {
		panic(fmt.Sprintf("tool %q missing run function", t.name))
	}
	return t.runFn(ctx, call)
}
