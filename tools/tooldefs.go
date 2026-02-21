package tools

import "github.com/agusx1211/miclaw/tooling"

type ToolDef = tooling.ToolDef

func ToProviderDefs(tools []Tool) []ToolDef {

	return tooling.ToProviderDefs(tools)
}
