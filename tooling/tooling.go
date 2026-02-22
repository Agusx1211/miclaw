package tooling

import (
	"context"
	"encoding/json"

	"github.com/agusx1211/miclaw/model"
)

type Tool interface {
	Name() string
	Description() string
	Parameters() JSONSchema
	Run(ctx context.Context, call model.ToolCallPart) (ToolResult, error)
}

type JSONSchema struct {
	Type       string                `json:"type"`
	Properties map[string]JSONSchema `json:"properties,omitempty"`
	Required   []string              `json:"required,omitempty"`
	Items      *JSONSchema           `json:"items,omitempty"`
	Enum       []string              `json:"enum,omitempty"`
	Desc       string                `json:"description,omitempty"`
}

func (s JSONSchema) MarshalJSON() ([]byte, error) {
	m := map[string]any{}
	if s.Type != "" {
		m["type"] = s.Type
	}
	if s.Type == "object" {
		if s.Properties == nil {
			m["properties"] = map[string]JSONSchema{}
		} else {
			m["properties"] = s.Properties
		}
	} else if len(s.Properties) > 0 {
		m["properties"] = s.Properties
	}
	if len(s.Required) > 0 {
		m["required"] = s.Required
	}
	if s.Items != nil {
		m["items"] = s.Items
	}
	if len(s.Enum) > 0 {
		m["enum"] = s.Enum
	}
	if s.Desc != "" {
		m["description"] = s.Desc
	}
	return json.Marshal(m)
}

type ToolResult struct {
	Content string
	IsError bool
}

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
