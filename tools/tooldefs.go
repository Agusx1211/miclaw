package tools

import "encoding/json"

type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

func ToProviderDefs(tools []Tool) []ToolDef {
	defs := make([]ToolDef, 0, len(tools))
	for _, t := range tools {
		parameters, err := json.Marshal(t.Parameters())
		if err != nil {
			panic(err)
		}
		defs = append(defs, ToolDef{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  parameters,
		})
	}
	return defs
}
