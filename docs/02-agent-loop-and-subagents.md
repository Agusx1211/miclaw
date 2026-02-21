# Agent Loop & Sub-agents

## 1. Singleton Agent

There is exactly one agent. All input channels (Signal, webhooks) feed messages into this agent's conversation thread. The agent is never duplicated, never forked. If a webhook fires while the agent is processing a Signal message, the webhook payload is queued and processed after the current turn completes.

### Agent State

```go
type Agent struct {
    sessions     SessionStore       // Session persistence
    messages     MessageStore       // Message persistence
    tools        []Tool             // Available tools
    provider     LLMProvider        // Streaming LLM backend
    active       atomic.Bool        // Is a request in progress?
    cancel       context.CancelFunc // Cancel current request
    eventBroker  *Broker[AgentEvent]
}
```

### Why Singleton

- Simpler state management. One conversation history. One context window.
- No routing logic. Every input goes to the same place.
- No concurrency between agents. The agent processes one turn at a time.
- Sub-agents handle parallelism when needed (read-only research tasks).

---

## 2. Main Conversation Loop

### High-Level Flow

```
Input arrives (Signal message, webhook payload)
    |
agent.Run(ctx, content, attachments)
    |
Queue if busy (agent processes one turn at a time)
    |
processGeneration(ctx, content, attachments):
    |
1. Fetch existing messages for session
    |
2. If summary exists (compaction happened):
   - Find summary message
   - Truncate history to summary point
   - Set summary as user message
    |
3. Create new user message in store
    |
4. Build conversation history: [...previous, newUserMsg]
    |
5. MAIN LOOP:
   +-----------------------------------------------+
   | Check cancellation (ctx.Done?)                 |
   |     |                                          |
   | Run middleware (compaction check)               |
   |     |                                          |
   | streamAndHandle(ctx, history)                  |
   |     |                                          |
   | Returns: (assistantMsg, toolResults, error)    |
   |     |                                          |
   | If FinishReason == ToolUse AND toolResults:    |
   |   -> Append assistantMsg + toolResults         |
   |   -> CONTINUE LOOP                             |
   |                                                |
   | Else (EndTurn, no tool calls):                 |
   |   -> Return final response                     |
   |   -> EXIT LOOP                                 |
   +-----------------------------------------------+
```

### streamAndHandle -- Streaming + Tool Execution

```
streamAndHandle(ctx, history):
    |
1. Create empty assistant message in store
    |
2. Start streaming: provider.Stream(ctx, history, tools)
   Returns: channel of ProviderEvents
    |
3. For each event from stream:
   - ContentDelta    -> append text to assistant message
   - ThinkingDelta   -> append reasoning to assistant message
   - ToolUseStart    -> add tool call to assistant message
   - ToolUseDelta    -> update tool call input
   - ToolUseStop     -> mark tool call finished
   - Complete        -> set finish reason, track usage
   - Error           -> return error

   After each event: persist message to store
    |
4. Extract tool calls from assistant message
    |
5. Execute each tool call sequentially:
   for each toolCall:
     - Check cancellation
     - Find matching tool by name
     - Execute: tool.Run(ctx, toolCall)
     - Store result: { toolCallID, content, isError }
    |
6. Create Tool role message with all results
    |
7. Return (assistantMsg, toolResultMsg, nil)
```

---

## 3. Tool Call Processing

### Sequential Execution

Tool calls are always executed sequentially, in order. This is deliberate:
- Simpler error handling
- Predictable execution order
- Cancellation can stop remaining tools

### Execution

```
For each toolCall in assistantMessage.ToolCalls:
    |
    Check ctx.Done? -> If cancelled, mark remaining as cancelled
    |
    Find tool by name in agent.tools
    |
    If not found: result = { content: "Tool not found: <name>", isError: true }
    |
    Execute: tool.Run(ctx, toolCall)
    |
    result = { toolCallID, content, isError }
    |
Create single Tool message with all results
Append to conversation history
```

### Cancellation

When the user cancels mid-execution:

1. `cancel()` called, context cancelled
2. Current tool finishes or detects cancellation
3. Remaining tools get: `{ content: "Cancelled", isError: true }`
4. Assistant message gets: finishReason = Canceled
5. Message persisted to store

---

## 4. Sub-agent System

### Overview

The main agent can spawn sub-agents via the `sessions_spawn` tool. Sub-agents are read-only research workers with a limited tool set. They run their own conversation loop but are owned by the singleton agent.

### Spawning

```
Main agent calls sessions_spawn tool with a prompt
    |
Create sub-agent with LIMITED tools:
  [read, grep, glob, ls, memory_search, memory_get]
  NO: write, edit, exec, process, cron, message, sessions_spawn
    |
Create child session (linked to parent)
    |
Sub-agent runs autonomously with its own conversation loop
    |
Sub-agent returns text result to parent
    |
Parent receives result as tool call output
```

### Sub-agent Constraints

Sub-agents run as goroutines in the same process, in the same container as the main agent. They are not separate containers or processes.

| Aspect | Main Agent | Sub-agent |
|--------|-----------|-----------|
| Tools | All 20 | 6 (read-only + memory) |
| Can write files | Yes | No |
| Can execute commands | Yes | No |
| Can spawn sub-agents | Yes | No |
| System prompt | Full mode | Minimal mode |
| Bootstrap files | All | AGENTS.md only |
| Has own session | Yes | Yes (child) |
| Cost tracking | Direct | Propagated to parent |
| Container | Shared | Shared |

### No Recursive Spawning

Sub-agents cannot spawn their own sub-agents. Depth is always 1. The `sessions_spawn` tool is not available to sub-agents.

---

## 5. Session System

### Session Structure

```go
type Session struct {
    ID               string
    ParentSessionID  string    // empty for root
    Title            string
    MessageCount     int
    PromptTokens     int
    CompletionTokens int
    SummaryMessageID string    // set after compaction
    Cost             float64
    CreatedAt        time.Time
    UpdatedAt        time.Time
}
```

### Session Types

| Type | ID Format | Parent | Purpose |
|------|-----------|--------|---------|
| Main | UUID | none | Singleton agent conversation |
| Task | toolCallID | main | Sub-agent work |

There is no separate title-generation session. Titles are generated inline or skipped.

---

## 6. Message System

### Message Structure

```go
type Message struct {
    ID        string
    Role      Role       // "assistant", "user", "tool"
    SessionID string
    Parts     []Part     // Polymorphic content
    Model     string     // Which model generated it
    CreatedAt time.Time
    UpdatedAt time.Time
}
```

### Part Types

```go
// Text content
type TextPart struct { Text string }

// Reasoning/thinking
type ReasoningPart struct { Thinking string }

// Tool call (in assistant messages)
type ToolCallPart struct {
    ID       string
    Name     string
    Input    string   // JSON
    Finished bool
}

// Tool result (in tool messages)
type ToolResultPart struct {
    ToolCallID string
    Name       string
    Content    string
    IsError    bool
}

// Finish marker
type FinishPart struct {
    Reason FinishReason  // EndTurn, ToolUse, Canceled
    Time   time.Time
}

// Binary content (images, attachments)
type BinaryPart struct {
    Path     string
    MimeType string
    Data     []byte
}
```

### History Building

When building history for an LLM call:

```
1. Fetch all messages for session (ordered by creation time)
2. If SummaryMessageID is set:
   - Find summary message
   - Truncate: keep only messages from summary onward
   - Set summary message role to User
3. Append new user message
4. Result: [system_prompt, ...history, new_user_message]
```

---

## 7. Event System

### Broker

```go
type Broker[T any] struct {
    subscribers map[chan Event[T]]struct{}
    mu          sync.RWMutex
    bufSize     int  // 64
}

// Publish: non-blocking broadcast to all subscribers
// Subscribe: returns channel, auto-cleanup on context cancel
```

### Agent Event Types

```go
type AgentEvent struct {
    Type      string   // "error", "response", "compact"
    Message   *Message
    Error     error
    SessionID string
    Done      bool
}
```

---

## 8. Input Queuing

Since the agent processes one turn at a time, concurrent inputs must be queued.

```
Input arrives while agent is busy:
    |
Enqueue in FIFO queue
    |
Agent finishes current turn
    |
Dequeue next input
    |
Process as new turn
```

This is simpler than debouncing. Signal group messages from the same sender within a short window can be coalesced before queuing (concatenate text, keep last timestamp).

---

## 9. Provider Interface

```go
type LLMProvider interface {
    // Streaming conversation
    Stream(ctx context.Context, messages []Message, tools []Tool) <-chan ProviderEvent
    // Model info
    Model() ModelInfo
}

type ProviderEvent struct {
    Type     ProviderEventType
    Content  string      // text delta
    Thinking string      // reasoning delta
    ToolCall *ToolCall   // tool call start/delta/stop
    Response *Response   // complete event with usage
    Error    error
}
```

### Retry Logic

```
maxRetries = 8
backoff = exponential with jitter

For each attempt:
  1. Send request
  2. If success: return
  3. If 429 (rate limit) or 529 (server overload):
     backoff = 2s * 2^(attempt-1) + jitter(20%)
     wait, retry
  4. Any other error: return immediately
```
