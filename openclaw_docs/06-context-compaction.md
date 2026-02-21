# Context Compaction

## 1. Overview

Context compaction prevents the agent from exceeding the LLM's context window by summarizing the conversation history when it grows too large. It runs as a middleware in the agent loop, triggered before each LLM turn when token count exceeds a threshold.

**Note:** There is no separate "heartbeat system" in the codebase. The only keepalive mechanism is HTTP client timeouts (720 seconds / 12 minutes default). The heartbeat-related files (HEARTBEAT.md, heartbeat prompt section) are about a *response pattern* where the agent responds with `HEARTBEAT_OK` to health-check messages, not a background keepalive system.

---

## 2. Trigger Mechanism

### AutoCompactMiddleware

```typescript
class AutoCompactMiddleware {
  threshold: number;  // Default: 200,000 tokens

  async before_turn(context: ConversationContext): MiddlewareResult {
    if (context.stats.contextTokens >= this.threshold) {
      return {
        action: "compact",
        metadata: {
          old_tokens: context.stats.contextTokens,
          threshold: this.threshold,
        },
      };
    }
    return { action: "continue" };
  }
}
```

### Configuration

```typescript
// In VibeConfig (config.py)
auto_compact_threshold: int = 200_000    // Default: 200K tokens
context_warnings: bool = false            // Warn at 50% of threshold
```

### When It Runs

Compaction runs **before each LLM turn** in the middleware pipeline:

```
AGENT LOOP ITERATION:
  1. Middleware pipeline: run before_turn()
     ├── AutoCompactMiddleware checks: contextTokens >= threshold?
     ├── If YES: return action=COMPACT
     └── If NO: return action=CONTINUE
  2. If COMPACT:
     ├── Emit CompactStartEvent
     ├── Call agent.compact()
     └── Emit CompactEndEvent
  3. Proceed with LLM turn (only if no STOP action)
```

### Context Token Tracking

After every LLM API call, `context_tokens` is updated:

```typescript
function updateStats(usage: LLMUsage, timeSeconds: number) {
  stats.lastTurnPromptTokens = usage.promptTokens;
  stats.lastTurnCompletionTokens = usage.completionTokens;
  stats.sessionPromptTokens += usage.promptTokens;
  stats.sessionCompletionTokens += usage.completionTokens;
  stats.contextTokens = usage.promptTokens + usage.completionTokens;
  if (timeSeconds > 0 && usage.completionTokens > 0) {
    stats.tokensPerSecond = usage.completionTokens / timeSeconds;
  }
}
```

### Context Warning (Optional)

If `context_warnings` is enabled, warns the user when context reaches 50% of threshold:

```typescript
class ContextWarningMiddleware {
  async before_turn(context) {
    if (context.stats.contextTokens >= this.threshold * 0.5) {
      // Emit warning to user
    }
  }
}
```

---

## 3. Compaction Process

### Full Compaction Flow

```typescript
async function compact(): string {
  try {
    // Step 1: Clean message history
    cleanMessageHistory();
    // - Fill missing tool responses
    // - Ensure assistant message after tools
    // - Save current state

    // Step 2: Find last user message (to preserve intent)
    let lastUserMessage = null;
    for (const msg of messages.reverse()) {
      if (msg.role === "user") {
        lastUserMessage = msg.content;
        break;
      }
    }

    // Step 3: Request summary from LLM
    messages.push({ role: "user", content: COMPACTION_PROMPT });
    stats.steps += 1;
    const summaryResult = await chat();  // Call LLM
    let summaryContent = summaryResult.message.content ?? "";

    // Step 4: Append last user request for continuity
    if (lastUserMessage) {
      summaryContent += `\n\nLast request from user was: ${lastUserMessage}`;
    }

    // Step 5: Replace entire history with compressed version
    const systemMessage = messages[0];  // Preserve system prompt
    const summaryMessage = { role: "user", content: summaryContent };
    messages = [systemMessage, summaryMessage];

    // Step 6: Recount tokens accurately
    const actualContextTokens = await backend.countTokens({
      model: activeModel,
      messages: messages,
      tools: availableTools,
      maxTokens: 16,  // Minimal probe
    });
    stats.contextTokens = actualContextTokens;

    // Step 7: Reset session
    resetSession();  // New session ID
    await interactionLogger.save(messages, stats, config, toolManager);
    middlewarePipeline.reset(ResetReason.COMPACT);

    return summaryContent;

  } catch (error) {
    await interactionLogger.save(messages, stats, config, toolManager);
    throw error;
  }
}
```

---

## 4. Summarization Prompt

The prompt used to request a summary from the LLM (stored in `prompts/compact.md`):

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

### Always Preserved

| Item | How |
|------|-----|
| **System prompt** | Kept as `messages[0]` (never removed) |
| **Last user message intent** | Appended to summary: `"Last request from user was: ..."` |
| **Active work details** | Requested in section 5 of summarization prompt |
| **File paths and code** | Requested in sections 4 and 5 |
| **Unresolved issues** | Requested in section 6 |

### Removed (Replaced by Summary)

| Item | Notes |
|------|-------|
| All intermediate assistant responses | Summarized |
| All tool call messages | Summarized |
| All tool result messages | Summarized |
| Middle conversation turns | Summarized |
| Historical exchanges | Only preserved if relevant to summary |

### Post-Compaction Message Structure

```
messages = [
  { role: "system", content: <full system prompt> },       // Preserved
  { role: "user", content: <summary + last user request> } // New
]
```

---

## 6. Message Cleaning Before Compaction

Before compaction, the message history is cleaned:

```typescript
function cleanMessageHistory() {
  const ACCEPTABLE_HISTORY_SIZE = 2;
  if (messages.length < ACCEPTABLE_HISTORY_SIZE) return;

  fillMissingToolResponses();
  ensureAssistantAfterTools();
}

function fillMissingToolResponses() {
  // Ensures every assistant message with tool_calls
  // has corresponding tool response messages
  // Fills missing responses with "Tool no response" messages
}

function ensureAssistantAfterTools() {
  // Ensures conversation never ends with tool message
  // Adds "Understood." assistant message if needed
}
```

This ensures the message history is valid for the summarization API call.

---

## 7. Token Recounting

After replacing the history with the summary, tokens are recounted accurately:

```typescript
async function countTokens(params: {
  model: ModelConfig;
  messages: Message[];
  tools: Tool[];
  maxTokens: number;         // 16 (minimal probe)
}): number {
  // Make a real API call with max_tokens=16
  // Extract prompt_tokens from usage
  // This gives accurate token count including tool schemas
  const result = await backend.complete({
    model, messages, tools,
    maxTokens: 16,  // Minimal generation
  });
  return result.usage.promptTokens;
}
```

This is necessary because token counting is provider-specific and must account for tool schemas, message formatting overhead, etc.

---

## 8. Session Reset After Compaction

```typescript
// After compaction:
resetSession();           // Generate new session ID
await interactionLogger.save(messages, stats, config, toolManager);
middlewarePipeline.reset(ResetReason.COMPACT);  // Reset middleware state
```

The session reset means:
- A new interaction log file is created
- Middleware counters (like warning flags) are reset
- The compacted conversation continues seamlessly

---

## 9. Compaction Events

Two events are emitted to track compaction for UI/logging:

```typescript
type CompactStartEvent = {
  currentContextTokens: number;
  threshold: number;
};

type CompactEndEvent = {
  oldContextTokens: number;
  newContextTokens: number;
  summaryLength: number;        // Character length of summary
};
```

**UI Integration:** Displays:
- During: "Compacting conversation history..."
- After: "Compaction complete: 200,000 → 15,000 tokens (-92%)"

---

## 10. Integration with Agent Loop

```
AGENT LOOP (each iteration):
    ↓
1. Run before_turn middleware
   ├── AutoCompactMiddleware:
   │   contextTokens >= 200,000?
   │     YES → action=COMPACT
   │     NO  → action=CONTINUE
   │
   ├── ContextWarningMiddleware (if enabled):
   │   contextTokens >= 100,000?
   │     YES → emit warning
   │
   └── Other middleware...
    ↓
2. Handle middleware result:
   ├── COMPACT:
   │   ├── Yield CompactStartEvent
   │   ├── agent.compact()
   │   │   ├── Clean history
   │   │   ├── Summarize via LLM
   │   │   ├── Replace messages
   │   │   ├── Recount tokens
   │   │   └── Reset session
   │   └── Yield CompactEndEvent
   │
   ├── STOP: exit loop
   └── CONTINUE: proceed
    ↓
3. Perform LLM turn (with compacted context if applicable)
    ↓
4. Handle tool calls
    ↓
5. Run after_turn middleware
    ↓
6. Loop continues...
```

---

## 11. Heartbeat Pattern (Not a Background System)

The "heartbeat" in OpenClaw is a **response pattern**, not a background keepalive. It works like this:

1. **HEARTBEAT.md** in workspace defines how the agent should respond to health checks
2. **heartbeat prompt section** in system prompt includes the pattern
3. When a channel/monitoring system sends a health-check message, the agent responds with `HEARTBEAT_OK`

This is purely request/response — there is no timer, no periodic pinging, no background process.

### HTTP Client Timeouts

The only actual timeout mechanism:

```typescript
// Default timeout: 720 seconds (12 minutes)
// HTTP keep-alive: max 5 connections, 10 total
```

---

## 12. Configuration Reference

```json
{
  "auto_compact_threshold": 200000,
  "context_warnings": false
}
```

| Setting | Default | Description |
|---------|---------|-------------|
| `auto_compact_threshold` | 200,000 | Token count that triggers compaction |
| `context_warnings` | false | Warn at 50% of threshold |

Set threshold to 0 or negative to disable auto-compaction.
