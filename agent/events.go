package agent

type AgentEventType string

const (
	EventError    AgentEventType = "error"
	EventResponse AgentEventType = "response"
	EventCompact  AgentEventType = "compact"
)

type AgentEvent struct {
	Type      AgentEventType
	SessionID string
	Message   *Message
	Error     error
}
