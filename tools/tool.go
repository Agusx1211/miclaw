package tools

import (
	"context"
	"fmt"

	"github.com/agusx1211/miclaw/model"
	"github.com/agusx1211/miclaw/tooling"
)

type Tool = tooling.Tool
type JSONSchema = tooling.JSONSchema
type ToolResult = tooling.ToolResult

type tool struct {
	name   string
	desc   string
	params JSONSchema
	runFn  func(ctx context.Context, call model.ToolCallPart) (ToolResult, error)
}

func (t tool) Name() string           { return t.name }
func (t tool) Description() string    { return t.desc }
func (t tool) Parameters() JSONSchema { return t.params }
func (t tool) Run(ctx context.Context, call model.ToolCallPart) (ToolResult, error) {
	if t.runFn == nil {
		panic(fmt.Sprintf("tool %q missing run function", t.name))
	}
	return t.runFn(ctx, call)
}
