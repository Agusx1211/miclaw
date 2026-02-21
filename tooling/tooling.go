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

type sessionIDKey struct{}

func WithSessionID(ctx context.Context, sessionID string) context.Context {

	return context.WithValue(ctx, sessionIDKey{}, sessionID)
}

func SessionIDFromContext(ctx context.Context) string {

	v, _ := ctx.Value(sessionIDKey{}).(string)
	return v
}
