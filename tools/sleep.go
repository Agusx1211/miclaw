package tools

import (
	"context"

	"github.com/agusx1211/miclaw/model"
)

func sleepTool() Tool {
	return tool{
		name: "sleep",
		desc: "Mark work complete and sleep until new input arrives",
		params: JSONSchema{
			Type: "object",
		},
		runFn: func(context.Context, model.ToolCallPart) (ToolResult, error) {
			return ToolResult{Content: "sleeping"}, nil
		},
	}
}
