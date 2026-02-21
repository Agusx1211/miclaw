# Agent Loop & Sub-agents

## 1. Core Agent Architecture

### Agent Service Interface

```typescript
interface AgentService {
  // Run a conversation turn
  run(ctx: Context, sessionID: string, content: string, attachments?: Attachment[]): AsyncChannel<AgentEvent>;
  // Cancel a running request
  cancel(sessionID: string): void;
  // Check if session is busy
  isSessionBusy(sessionID: string): boolean;
  // Check if any session is busy
  isBusy(): boolean;
  // Summarize conversation
  summarize(ctx: Context, sessionID: string): Promise<void>;
}
```

### Agent Internal State

```typescript
struct Agent {
  sessions: SessionService;        // Session storage
  messages: MessageService;        // Message persistence
  tools: BaseTool[];               // Available tools for this agent
  provider: LLMProvider;           // Main LLM provider (streaming)
  titleProvider: LLMProvider;      // For title generation (coder agent only)
  summarizeProvider: LLMProvider;  // For summarization (coder agent only)
  activeRequests: Map<string, CancelFunction>;  // Session ID → cancel
  eventBroker: PubSubBroker<AgentEvent>;        // Event broadcasting
}
```

### Agent Types

| Agent Type | Purpose | Tools | Can Spawn Sub-agents |
|------------|---------|-------|---------------------|
| `coder` | Main agent, full capabilities | All tools (20+) | Yes |
| `task` | Sub-agent for research | Read-only: Glob, Grep, LS, View, SourceGraph | No |
| `summarizer` | Conversation summarization | None | No |
| `title` | Session title generation | None | No |

---

## 2. Main Conversation Loop

### High-Level Flow

```
User Input
    ↓
agent.run(ctx, sessionID, content, attachments)
    ↓
Creates buffered event channel
    ↓
Spawns goroutine:
    1. Store cancel function in activeRequests
    2. Call processGeneration()
    3. Publish result via pubsub
    4. Send result to event channel
    ↓
Returns event channel immediately (non-blocking)
```

### processGeneration() - The Core Loop

```
processGeneration(ctx, sessionID, content, attachmentParts):
    ↓
1. Fetch existing messages for session
    ↓
2. If summary exists (session.SummaryMessageID != ""):
   - Find summary message
   - Truncate history to summary point
   - Change summary role: Assistant → User (for context)
    ↓
3. Create new User message in database
    ↓
4. Build conversation history: [...previous, newUserMsg]
    ↓
5. If first message in session → spawn title generation (async)
    ↓
6. MAIN LOOP:
   ┌─────────────────────────────────────────────────┐
   │ Check cancellation (ctx.Done?)                   │
   │     ↓                                           │
   │ streamAndHandleEvents(ctx, sessionID, history)  │
   │     ↓                                           │
   │ Returns: (assistantMsg, toolResults, error)     │
   │     ↓                                           │
   │ If FinishReason == ToolUse AND toolResults:     │
   │   → Append assistantMsg + toolResults to history│
   │   → CONTINUE LOOP                               │
   │                                                 │
   │ Else (EndTurn, or no tool calls):               │
   │   → Return final AgentEvent with response       │
   │   → EXIT LOOP                                   │
   └─────────────────────────────────────────────────┘
```

### streamAndHandleEvents() - Streaming + Tool Execution

```
streamAndHandleEvents(ctx, sessionID, msgHistory):
    ↓
1. Create empty assistant message in database
    ↓
2. Start streaming: provider.StreamResponse(ctx, msgHistory, tools)
   Returns: channel of ProviderEvents
    ↓
3. For each event from stream:
   - EventContentDelta    → assistantMsg.appendContent(text)
   - EventThinkingDelta   → assistantMsg.appendReasoning(text)
   - EventToolUseStart    → assistantMsg.addToolCall(toolCall)
   - EventToolUseDelta    → assistantMsg.updateToolCall(delta)
   - EventToolUseStop     → assistantMsg.finishToolCall(id)
   - EventComplete        → set finish reason, track usage
   - EventError           → return error

   After each event: persist message update to database
    ↓
4. After stream ends, extract tool calls from message
    ↓
5. Execute each tool call sequentially:
   for each toolCall:
     - Check cancellation
     - Find matching tool by name
     - Execute: tool.run(ctx, toolCall)
     - Store result: { toolCallID, content, metadata, isError }
    ↓
6. Create Tool role message with all results
    ↓
7. Return (assistantMsg, toolResultMsg, nil)
```

---

## 3. Tool Call Processing

### Execution Pipeline

```
Assistant response contains ToolCalls[]
    ↓
For i, toolCall in toolCalls:
    ↓
    Check ctx.Done? → If cancelled, mark remaining tools as cancelled
    ↓
    Find tool by name in agent.tools[]
    ↓
    If tool not found:
        result[i] = { content: "Tool not found: <name>", isError: true }
        continue
    ↓
    Execute: tool.run(ctx, toolCall)
    ↓
    If error == PermissionDenied:
        result[i] = { content: "Permission denied", isError: true }
        Mark remaining tools as cancelled
        Set finishReason = PermissionDenied
        Break
    ↓
    result[i] = { content: toolResult.content, metadata, isError }
    ↓
Create single Tool message with all results
Append to conversation history
```

### Tool Call Cancellation

When a user cancels mid-tool-execution:

```
1. cancel() called → ctx cancelled
2. Current tool finishes (or detects cancellation)
3. All remaining tools get:
   { content: "Tool execution canceled by user", isError: true }
4. Assistant message gets: finishReason = Canceled
5. Message persisted to database
6. Error propagated: ErrRequestCancelled
```

### Parallel vs Sequential Tool Calls

Tool calls are executed **sequentially** in the order they appear in the response. This is a deliberate design choice:
- Simpler error handling
- Predictable execution order
- Permission denial can halt remaining tools

---

## 4. Sub-agent System

### Sub-agent Spawning (Agent Tool)

The `agent` tool allows the main coder agent to spawn task sub-agents:

```typescript
// Agent tool implementation
async function agentTool.run(ctx, toolCall) {
  // 1. Parse prompt from tool call input
  const params = parseInput(toolCall.input);  // { prompt: string }

  // 2. Create new Task Agent with LIMITED tools
  const taskAgent = new Agent("task", sessions, messages, TaskAgentTools());
  // TaskAgentTools = [Glob, Grep, LS, SourceGraph, View]
  // NO: write, edit, exec, bash, message, spawn, browser, etc.

  // 3. Create child session
  const childSession = sessions.createTaskSession(
    toolCall.id,        // Child session ID = tool call ID
    sessionID,          // Parent session ID
    "New Agent Session" // Title
  );

  // 4. Run task agent autonomously
  const eventChannel = taskAgent.run(ctx, childSession.id, params.prompt);
  const result = await eventChannel;  // Wait for completion

  // 5. Propagate costs to parent
  const updatedChild = sessions.get(childSession.id);
  const parent = sessions.get(sessionID);
  parent.cost += updatedChild.cost;
  sessions.save(parent);

  // 6. Return text result to parent agent
  return { content: result.message.content.toString() };
}
```

### Sub-agent Constraints

| Aspect | Main Agent (Coder) | Sub-agent (Task) |
|--------|-------------------|-------------------|
| Tools | 20+ (all) | 5 (read-only) |
| Can write files | Yes | No |
| Can execute commands | Yes | No |
| Can spawn sub-agents | Yes | No |
| Has own session | Yes | Yes (child) |
| Cost tracking | Direct | Propagated to parent |
| System prompt | Full mode | Minimal mode |
| Bootstrap files | All | AGENTS.md + TOOLS.md only |

### Sub-agent Depth (OpenClaw Extension)

OpenClaw supports deeper sub-agent hierarchies with depth-based tool restrictions:

```typescript
// Tools always denied to sub-agents
const SUBAGENT_TOOL_DENY_ALWAYS = [
  "gateway", "agents_list", "whatsapp_login",
  "session_status", "cron",
  "memory_search", "memory_get",  // Pass context in spawn instead
  "sessions_send"                  // Use announce chain instead
];

// Additional tools denied to leaf sub-agents (depth >= maxSpawnDepth)
const SUBAGENT_TOOL_DENY_LEAF = [
  "sessions_list", "sessions_history", "sessions_spawn"
];
```

---

## 5. Session Hierarchy

### Session Structure

```typescript
type Session = {
  id: string;               // Unique session ID
  parentSessionID: string;  // Links to parent (empty for root)
  title: string;            // Human-readable title
  messageCount: number;
  promptTokens: number;
  completionTokens: number;
  summaryMessageID: string; // For conversation summarization
  cost: number;             // Accumulated cost
  createdAt: number;
  updatedAt: number;
};
```

### Session Types

| Session Type | ID Format | Parent | Purpose |
|-------------|-----------|--------|---------|
| Main | UUID | none | Primary conversation |
| Title | `"title-" + parentID` | main | Title generation |
| Task | `toolCallID` | main | Sub-agent work |
| Summarize | same session | n/a | Summarization uses same session |

### Session Key Format (OpenClaw)

OpenClaw uses structured session keys for routing:

```
Main DM:    <agent>:<account-id>:signal:dm:<sender-uuid>
Group:      <agent>:<account-id>:signal:group:<group-id>
Sub-agent:  agent:<agentId>:sub:<depth>:<parentKey>
```

---

## 6. Message System

### Message Structure

```typescript
type Message = {
  id: string;
  role: "assistant" | "user" | "system" | "tool";
  sessionID: string;
  parts: ContentPart[];   // Polymorphic content
  model: string;          // Which model generated it
  createdAt: number;
  updatedAt: number;
};
```

### Content Part Types

```typescript
// Text content
type TextContent = { type: "text"; text: string; };

// Reasoning/thinking (extended thinking models)
type ReasoningContent = { type: "reasoning"; thinking: string; };

// Tool calls made by assistant
type ToolCall = {
  type: "tool_call";
  id: string;
  name: string;
  input: string;       // JSON string of parameters
  finished: boolean;
};

// Tool results returned to assistant
type ToolResult = {
  type: "tool_result";
  toolCallID: string;
  name: string;
  content: string;
  metadata?: string;
  isError: boolean;
};

// Finish marker
type Finish = {
  type: "finish";
  reason: FinishReason;  // "end_turn" | "tool_use" | "canceled" | "permission_denied"
  time: number;
};

// Binary/image content
type BinaryContent = { type: "binary"; path: string; mimeType: string; data: bytes; };
type ImageURLContent = { type: "image_url"; url: string; detail?: string; };
```

### Message Serialization

Messages are stored with their parts serialized as JSON:

```json
[
  { "type": "text", "data": { "text": "Hello" } },
  { "type": "tool_call", "data": { "id": "tc_1", "name": "bash", "input": "{\"command\":\"ls\"}" } },
  { "type": "tool_result", "data": { "tool_call_id": "tc_1", "content": "file1.txt\nfile2.txt" } },
  { "type": "finish", "data": { "reason": "end_turn", "time": 1234567890 } }
]
```

### Conversation History Management

When building history for an API call:

```
1. Fetch all messages for session (ordered by creation time)
2. If summary exists:
   - Find summary message by session.summaryMessageID
   - Truncate: keep only messages from summary onward
   - Change summary message role from Assistant → User
3. Append new user message
4. Result: [system_prompt, ...history, new_user_message]
```

---

## 7. Event System (Pub/Sub)

### Broker Architecture

```typescript
type Broker<T> = {
  subscribers: Map<Channel<Event<T>>, void>;
  maxEvents: number;  // Buffer size per subscriber: 64
};

// Publish: non-blocking broadcast to all subscribers
broker.publish(eventType, payload):
  for each subscriber channel:
    try send Event{type, payload} (non-blocking, drops if full)

// Subscribe: returns channel, auto-cleanup on context cancel
broker.subscribe(ctx):
  ch = new BufferedChannel(64)
  add ch to subscribers
  spawn goroutine: wait for ctx.Done → remove ch, close ch
  return ch
```

### Agent Event Types

```typescript
type AgentEventType = "error" | "response" | "summarize";

type AgentEvent = {
  type: AgentEventType;
  message?: Message;     // The response message
  error?: Error;         // Error if type = "error"
  sessionID?: string;    // For summarize events
  progress?: string;     // Summarization progress
  done: boolean;         // Indicates completion
};
```

### Provider Event Types (Streaming)

```typescript
type ProviderEventType =
  | "content_start"
  | "content_delta"     // Text being generated
  | "content_stop"
  | "thinking_delta"    // Reasoning/thinking
  | "tool_use_start"    // Beginning tool call
  | "tool_use_delta"    // Tool input being generated
  | "tool_use_stop"     // Tool call complete
  | "complete"          // Final response with finish reason + usage
  | "error"
  | "warning";

type ProviderEvent = {
  type: ProviderEventType;
  content?: string;
  thinking?: string;
  response?: ProviderResponse;
  toolCall?: ToolCall;
  error?: Error;
};
```

### Event Flow Diagram

```
LLM API (streaming)
    ↓ ProviderEvents
Provider.StreamResponse()
    ↓ channel<ProviderEvent>
Agent.streamAndHandleEvents()
    ↓ processes events, updates message, persists
    ↓
Agent.processGeneration()
    ↓ AgentEvent
Agent.run()
    ↓ channel<AgentEvent>     ↓ pubsub.Publish()
Caller                     All Subscribers
(direct result)            (UI, logging, etc.)
```

---

## 8. Provider Communication

### Provider Interface

```typescript
interface LLMProvider {
  // Non-streaming (for title generation, summarization)
  sendMessages(ctx, messages: Message[], tools: BaseTool[]): ProviderResponse;
  // Streaming (for main conversation)
  streamResponse(ctx, messages: Message[], tools: BaseTool[]): Channel<ProviderEvent>;
  // Get model info
  model(): Model;
}
```

### Message Conversion (Anthropic Example)

```
Internal Message          →    Anthropic API Format
─────────────────────           ──────────────────────
User + TextContent        →    anthropic.NewUserMessage(text)
User + BinaryContent      →    anthropic.NewUserMessage(image_block)
Assistant + TextContent   →    anthropic.NewAssistantMessage(text)
Assistant + ToolCalls     →    anthropic.NewAssistantMessage(tool_use_blocks)
Tool + ToolResults        →    anthropic.NewUserMessage(tool_result_blocks)
```

### Retry Logic

```
maxRetries = 8
backoff = exponential with jitter

For each attempt:
  1. Send request to provider
  2. If success: return response
  3. If error:
     - Status 429 (rate limit) or 529 (server error):
       backoffMs = 2000 * (2 ^ (attempt - 1))  // 2s, 4s, 8s, 16s...
       jitter = 20% of backoff
       wait(backoff + jitter)
       Check Retry-After header (use if present)
       Continue to next attempt
     - Any other error: return error immediately
  4. If max retries exceeded: return error
```

---

## 9. Title Generation

Runs asynchronously on the first message of a session:

```
1. Check: is this the first message? (len(messages) == 0)
2. If yes, spawn background goroutine:
   a. Call titleProvider.sendMessages([userMessage], noTools)
   b. Extract title from response
   c. Update session.title = trimmed response
   d. Save session
3. Main processing continues without waiting
```

---

## 10. Conversation Summarization

Runs on-demand when called by the user:

```
agent.summarize(ctx, sessionID):
    ↓
1. Check session not busy
2. Create cancellable context
3. Store cancel in activeRequests[sessionID + "-summarize"]
4. Spawn goroutine:
   a. Fetch all messages for session
   b. Append summarization prompt:
      "Provide a detailed but concise summary of our conversation above..."
   c. Call summarizeProvider.sendMessages(messages + prompt, noTools)
   d. Create summary message (role: Assistant) in session
   e. Set session.summaryMessageID = summaryMsg.id
   f. Save session
   g. Publish AgentEvent{type: "summarize", done: true}
```

On next conversation turn, the history is truncated to the summary point (see section 6).

---

## 11. Cost Tracking

### Per-Turn Tracking

After each LLM API call:

```typescript
function trackUsage(sessionID, model, usage) {
  const cost =
    (model.costPer1MInCached / 1e6) * usage.cacheCreationTokens +
    (model.costPer1MOutCached / 1e6) * usage.cacheReadTokens +
    (model.costPer1MIn / 1e6) * usage.inputTokens +
    (model.costPer1MOut / 1e6) * usage.outputTokens;

  session.cost += cost;
  session.completionTokens = usage.outputTokens + usage.cacheReadTokens;
  session.promptTokens = usage.inputTokens + usage.cacheCreationTokens;
  sessions.save(session);
}
```

### Sub-agent Cost Propagation

After sub-agent completes:
```
parentSession.cost += childSession.cost;
sessions.save(parentSession);
```

---

## 12. Cancellation & State Management

### Active Request Tracking

```typescript
// Map<sessionID, CancelFunction>
activeRequests: ConcurrentMap;

// On run: store cancel
activeRequests.store(sessionID, cancelFn);

// On cancel: retrieve and invoke
agent.cancel(sessionID):
  if fn = activeRequests.loadAndDelete(sessionID):
    fn()  // Cancels context
  if fn = activeRequests.loadAndDelete(sessionID + "-summarize"):
    fn()  // Also cancel summarization

// On completion: cleanup
activeRequests.delete(sessionID);

// Busy check
isSessionBusy(sessionID): activeRequests.has(sessionID)
isBusy(): activeRequests.size > 0
```

### Error Handling

```
processGeneration errors:
  - context.Canceled → mark as canceled, persist, return ErrRequestCancelled
  - PermissionDenied (from tool) → mark remaining tools cancelled, continue loop
  - Provider error → propagate up, publish error event
  - Panic → recover, publish error event

All errors:
  - Persist current message state to database
  - Remove from activeRequests
  - Publish error event via pubsub
```

---

## 13. Middleware Pipeline (OpenClaw Extension)

OpenClaw adds a middleware pipeline that runs before each LLM turn:

```typescript
interface Middleware {
  before_turn(context: ConversationContext): MiddlewareResult;
}

type MiddlewareResult = {
  action: "continue" | "compact" | "stop";
  metadata?: Record<string, unknown>;
};

// Example: AutoCompactMiddleware
class AutoCompactMiddleware {
  threshold: number;  // default: 200,000 tokens

  before_turn(context) {
    if (context.stats.contextTokens >= this.threshold) {
      return { action: "compact", metadata: { oldTokens, threshold } };
    }
    return { action: "continue" };
  }
}
```

The middleware runs at the top of each loop iteration, before calling the LLM:

```
MAIN LOOP:
  1. Run middleware pipeline (before_turn)
     - If COMPACT: run compaction, emit events
     - If STOP: exit loop
     - If CONTINUE: proceed
  2. Call LLM
  3. Process response + tool calls
  4. Run middleware pipeline (after_turn)
  5. If more tool calls: loop
```

---

## 14. Complete Data Flow Diagram

```
╔════════════════════════════════════════════════════════════════════╗
║                        AGENT LOOP                                  ║
╠════════════════════════════════════════════════════════════════════╣
║                                                                    ║
║  User Message                                                      ║
║       ↓                                                           ║
║  agent.run(ctx, sessionID, content)                               ║
║       ↓                                                           ║
║  [Spawn goroutine]                                                 ║
║       ↓                                                           ║
║  processGeneration()                                               ║
║  ┌────────────────────────────────────────────┐                   ║
║  │ 1. Load session messages from DB            │                   ║
║  │ 2. Apply summary truncation (if exists)     │                   ║
║  │ 3. Create user message in DB                │                   ║
║  │ 4. Build conversation history               │                   ║
║  │ 5. Title generation (if first message)      │                   ║
║  └──────────────┬─────────────────────────────┘                   ║
║                 ↓                                                  ║
║  ┌──── LOOP ────────────────────────────────────┐                 ║
║  │                                               │                 ║
║  │  [Middleware: check compaction]                │                 ║
║  │       ↓                                       │                 ║
║  │  streamAndHandleEvents()                      │                 ║
║  │  ┌─────────────────────────────┐              │                 ║
║  │  │ Create empty assistant msg   │              │                 ║
║  │  │       ↓                     │              │                 ║
║  │  │ provider.StreamResponse()    │              │                 ║
║  │  │       ↓                     │              │                 ║
║  │  │ For each ProviderEvent:     │              │                 ║
║  │  │  - Update assistant message  │              │                 ║
║  │  │  - Persist to DB            │              │                 ║
║  │  │       ↓                     │              │                 ║
║  │  │ Execute tool calls (seq.)   │              │                 ║
║  │  │  - Permission check          │              │                 ║
║  │  │  - tool.run(ctx, call)      │              │                 ║
║  │  │  - Collect results          │              │                 ║
║  │  │       ↓                     │              │                 ║
║  │  │ Create Tool message in DB   │              │                 ║
║  │  └─────────────────────────────┘              │                 ║
║  │       ↓                                       │                 ║
║  │  FinishReason == ToolUse?                     │                 ║
║  │    YES → append to history, CONTINUE          │                 ║
║  │    NO  → return final response, EXIT          │                 ║
║  └───────────────────────────────────────────────┘                 ║
║       ↓                                                           ║
║  Publish AgentEvent to all subscribers                             ║
║  Send result to event channel                                      ║
║  Cleanup activeRequests                                            ║
║                                                                    ║
╚════════════════════════════════════════════════════════════════════╝
```
