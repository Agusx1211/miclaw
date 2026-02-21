package agent

import "github.com/agusx1211/miclaw/model"

type Role = model.Role

const (
	RoleAssistant Role = model.RoleAssistant
	RoleUser      Role = model.RoleUser
	RoleTool      Role = model.RoleTool
)

type Session = model.Session
type Message = model.Message

type MessagePart = model.MessagePart

type TextPart = model.TextPart
type ReasoningPart = model.ReasoningPart
type ToolCallPart = model.ToolCallPart
type ToolResultPart = model.ToolResultPart
type FinishPart = model.FinishPart
type BinaryPart = model.BinaryPart

type ToolCall = model.ToolCall
type ToolResult = model.ToolResult
