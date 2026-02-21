# Context Compaction

## 1. Overview

When the conversation grows too large for the LLM's context window, the agent summarizes the history and replaces it with a compressed version. The system prompt is preserved. The last user intent is preserved. Everything else becomes a structured summary.

---

## 2. Trigger

### Middleware Check

Before each LLM turn, the agent checks whether context tokens exceed the threshold:

```go
const DefaultCompactThreshold = 200_000 // tokens

func (m *CompactMiddleware) BeforeTurn(stats SessionStats) Action {
    if stats.ContextTokens >= m.Threshold {
        return ActionCompact
    }
    return ActionContinue
}
```

### Token Tracking

After every LLM call, context tokens are updated:

```go
stats.ContextTokens = usage.PromptTokens + usage.CompletionTokens
```

### Configuration

```json
{
    "compaction": {
        "threshold": 200000
    }
}
```

Set threshold to 0 to disable auto-compaction.

---

## 3. Compaction Process

```
1. Clean message history
   +-- Fill missing tool responses (ensure every tool_call has a result)
   +-- Ensure valid message ordering
    |
2. Find last user message (preserve intent)
    |
3. Append summarization prompt to conversation
    |
4. Call LLM to generate summary
    |
5. Build compressed history:
   messages = [
     { role: "system", content: <system prompt> },
     { role: "user",   content: <summary + last user request> }
   ]
    |
6. Recount tokens via API probe (16 max_tokens request)
    |
7. Reset session state (new interaction log, reset middleware)
```

### Implementation

```go
func (a *Agent) Compact(ctx context.Context) (string, error) {
    // Step 1: Clean history
    a.cleanMessageHistory()

    // Step 2: Find last user message
    var lastUserMsg string
    for i := len(a.messages) - 1; i >= 0; i-- {
        if a.messages[i].Role == RoleUser {
            lastUserMsg = a.messages[i].TextContent()
            break
        }
    }

    // Step 3: Request summary
    a.messages = append(a.messages, Message{
        Role: RoleUser,
        Parts: []Part{TextPart{Text: compactionPrompt}},
    })
    result := a.chat(ctx) // Call LLM
    summary := result.Message.TextContent()

    // Step 4: Append last user intent
    if lastUserMsg != "" {
        summary += "\n\nLast request from user was: " + lastUserMsg
    }

    // Step 5: Replace history
    systemMsg := a.messages[0]
    a.messages = []Message{
        systemMsg,
        {Role: RoleUser, Parts: []Part{TextPart{Text: summary}}},
    }

    // Step 6: Recount tokens
    a.stats.ContextTokens = a.provider.CountTokens(ctx, a.messages, a.tools)

    // Step 7: Reset
    a.resetSession()

    return summary, nil
}
```

---

## 4. Summarization Prompt

```
Provide a detailed but concise summary of our conversation. Structure it as follows:

1. User's Primary Goals and Intent
   - Capture all explicit requests and objectives
   - Preserve exact priorities and constraints

2. Conversation Timeline and Progress
   - Chronological key phases
   - Initial requests and how addressed
   - Major decisions and rationale
   - Problems and solutions
   - Current state

3. Technical Context and Decisions
   - Technologies, frameworks, tools
   - Architectural patterns and design decisions
   - Key technical constraints
   - Important code patterns established

4. Files and Code Changes
   - Full file paths
   - Purpose and importance
   - Specific changes with key code snippets
   - Current state

5. Active Work and Last Actions (CRITICAL)
   - Specific task being addressed
   - Last completed action
   - Any partial work or mid-implementation state
   - Include relevant code snippets

6. Unresolved Issues and Pending Tasks
   - Errors still requiring attention
   - Tasks explicitly requested but not started
   - Decisions waiting for user input

7. Immediate Next Step
   - SPECIFIC next action based on:
     - User's most recent request
     - Current implementation state
     - Any ongoing interrupted work

Be precise with technical details, file names, and code.
The next agent reading this should be able to continue exactly
where we left off without asking clarifying questions.
```

---

## 5. What's Preserved vs Removed

### Preserved

| Item | How |
|------|-----|
| System prompt | Kept as messages[0] |
| Last user intent | Appended to summary: "Last request from user was: ..." |
| Active work details | Section 5 of summarization prompt |
| File paths and code | Sections 4 and 5 |
| Unresolved issues | Section 6 |

### Removed (replaced by summary)

- All intermediate assistant responses
- All tool call messages
- All tool result messages
- All historical exchanges

### Post-Compaction State

```
messages = [
    { role: "system", content: <full system prompt> },
    { role: "user",   content: <structured summary + last user request> }
]
```

---

## 6. Message Cleaning

Before compaction, the history is cleaned to ensure it's valid input for the LLM:

```go
func (a *Agent) cleanMessageHistory() {
    a.fillMissingToolResponses()
    a.ensureAssistantAfterTools()
}
```

- **fillMissingToolResponses:** Every assistant message with tool_calls must have corresponding tool result messages. Missing responses are filled with "Tool no response".
- **ensureAssistantAfterTools:** Conversation must not end with a tool message. Adds "Understood." assistant message if needed.

---

## 7. Token Recounting

After replacing history with the summary, tokens are recounted accurately by making a minimal API call:

```go
func (p *Provider) CountTokens(ctx context.Context, msgs []Message, tools []Tool) int {
    // Make real API call with max_tokens=16
    // Extract prompt_tokens from usage
    result := p.Complete(ctx, msgs, tools, 16)
    return result.Usage.PromptTokens
}
```

This is necessary because token counting is provider-specific and must account for tool schemas, message formatting overhead, etc.

---

## 8. Memory Integration

Compaction and memory work together:

1. **Before compaction:** The agent can use `memory_search` to recall past context at any time.
2. **During compaction:** The summarization prompt captures key information in the summary.
3. **After compaction:** If important context was lost, the agent can recover it via `memory_search` on the next turn.

The MEMORY.md file and memory fragments persist across compactions. They are not part of the conversation history that gets compressed. They are part of the system prompt (via bootstrap files) and searchable via the memory tools.

---

## 9. Integration with Agent Loop

```
AGENT LOOP (each iteration):
    |
1. Run middleware: CompactMiddleware.BeforeTurn()
   +-- contextTokens >= threshold?
   |   YES -> ActionCompact
   |   NO  -> ActionContinue
    |
2. If ActionCompact:
   +-- agent.Compact(ctx)
   |   +-- Clean history
   |   +-- Summarize via LLM
   |   +-- Replace messages
   |   +-- Recount tokens
   |   +-- Reset session
    |
3. Perform LLM turn (with compacted context if applicable)
    |
4. Handle tool calls
    |
5. Loop continues...
```
