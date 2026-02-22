package agent

type AgentEventType string

const (
	EventError   AgentEventType = "error"
	EventCompact AgentEventType = "compact"
)

type AgentEvent struct {
	Type   AgentEventType
	Error  error
	Source string
}
