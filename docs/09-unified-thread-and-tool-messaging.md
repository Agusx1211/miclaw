# Unified Thread & Tool-Based Messaging

## 1. Summary

Replace the current session-per-source and sub-agent model with a single conversation thread. The agent communicates with the outside world exclusively through tools. Its text output (the assistant message content) becomes a private internal monologue -- never shown to anyone. When it has nothing to do, it stops. When input arrives, it wakes.

Three changes, tightly coupled:

1. **One thread.** No sessions. No sub-agents. Every input (Signal, webhook, cron) is injected into the same conversation history.
2. **Tool-based messaging.** The agent calls a `message` tool to talk to users. The text it emits is its own thinking, invisible to the outside.
3. **Mid-turn injection.** New inputs are appended to the conversation between tool-call rounds, so the agent sees them without finishing its current turn.

---

## 2. Why

### The current model

Today the agent processes inputs sequentially. A Signal message arrives, the agent runs a full turn (LLM call -> tool calls -> LLM call -> ... -> final text response), the response is extracted from the assistant message's text content, routed back to Signal, and only then does the next queued input get processed.

This creates three problems:

**Problem 1: The agent must stop thinking to speak.** The turn ends when the assistant emits text with no tool calls. If the agent wants to say "working on it" and keep going, it can't. Speaking *is* finishing.

**Problem 2: The agent is deaf while working.** During a multi-step tool chain (say, five sequential exec calls), new messages pile up in the queue. The agent cannot see them until it finishes the entire turn. If a user sends "actually, stop" two seconds in, the agent won't notice for minutes.

**Problem 3: Sessions and sub-agents add complexity for little value.** The session system exists to separate conversations (Signal DMs vs groups vs webhooks). Sub-agents exist for parallel research. But this is a singleton agent -- it has one personality, one memory, one workspace. Splitting its history into isolated sessions means it loses cross-channel context. And sub-agents are just expensive re-prompts of the same model with a subset of tools.

### The new model

The agent is a single continuous stream of consciousness. Everything that happens -- a Signal message, a webhook firing, a cron job, the agent's own thoughts -- goes into one linear history. The agent talks to users by calling tools. Its text output is internal narration that nobody else sees.

This is closer to how a person works. You don't "finish thinking" before you speak. You don't go deaf while writing an email. You can notice an interruption, say "one sec", and go back to what you were doing. And when there's nothing left to do, you stop.

---

## 3. The Single Thread

### Before

```
Session "signal:dm:alice"   -> [msg1, msg2, msg3, ...]
Session "signal:group:team" -> [msg1, msg2, msg3, ...]
Session "webhook:deploy"    -> [msg1, msg2, msg3, ...]
Sub-agent "task-abc123"     -> [msg1, msg2, msg3, ...]
```

### After

```
The Thread -> [msg1, msg2, msg3, msg4, msg5, ...]
```

One conversation. One history. One context window.

### What this eliminates

- `SessionStore` -- gone. No sessions to create, list, update, or query.
- `Session` struct -- gone.
- `SessionID` on messages -- gone. Every message belongs to The Thread.
- `SummaryMessageID` on sessions -- replaced by a single compaction pointer on the thread.
- `sessions_list`, `sessions_history`, `sessions_send`, `sessions_spawn`, `sessions_status`, `subagents` tools -- all gone.
- `agents_list` tool -- gone (there's one agent, it knows who it is).
- Sub-agent spawning -- gone entirely. If the agent needs to research something, it uses `read`, `grep`, `glob`, `exec` directly. One agent, one tool set.
- The `InputQueue` type -- gone. Inputs are injected directly into the thread between tool rounds.

### What stays

- `MessageStore` -- still needed, but simpler. Messages are appended to the one thread, queried in order.
- `Message` struct -- same shape, minus `SessionID`.
- Compaction -- still needed, still works the same way, just operates on the single thread.
- All non-session tools -- `read`, `write`, `edit`, `apply_patch`, `grep`, `glob`, `ls`, `exec`, `process`, `cron`, `message`, `memory_search`, `memory_get`.

---

## 4. Tool-Based Messaging

### The shift

Today: the agent's assistant text IS the reply. The event broker extracts it and routes it to Signal.

New: the agent's assistant text is internal monologue. To talk to someone, the agent calls the `message` tool.

### The `message` tool

Already exists in the codebase. Currently defined as:

```go
type MessageParams struct {
    To      string `json:"to"`      // "signal:+15551234567", "signal:group:<id>"
    Content string `json:"content"` // message text
}
```

This becomes the **only** way the agent communicates outward. The system prompt tells the agent:

> Your text responses are internal thoughts -- only you can see them. To send a message to a user or group, call the `message` tool with the target and content. You MUST use the `message` tool to communicate. Never assume your text output will be seen by anyone.

### What the agent's text output becomes

A stream of consciousness. The agent can use it to:

- Reason about what's happening: "Alice just asked about the deploy. Let me check the last webhook..."
- Plan multi-step actions: "I need to: 1) read the log, 2) grep for errors, 3) message Alice with the result."
- Track state: "I sent the deploy notification. Now waiting for the next event."
- React to injected messages: "New message from Bob while I was working on Alice's request. I'll respond to Bob first since it's quick."

This is free-form and unstructured. The model decides what to think about. The text is stored in the message history (the LLM needs to see its own previous thoughts), but it's never shown to users.

### Event system changes

Currently `EventResponse` fires when the agent produces a text response, and the Signal pipeline subscribes to deliver it. With tool-based messaging, this changes:

- The `message` tool itself handles delivery (calls Signal RPC, sends webhook response, etc.)
- The event broker can still emit events for observability, but they're no longer the delivery mechanism.
- `EventResponse` can be repurposed or removed. The agent's text output is not a "response" anymore.

---

## 5. Mid-Turn Message Injection

This is the key mechanism that makes the single thread work without blocking.

### The seam

The agent loop is:

```
call LLM -> get response -> execute tools -> call LLM again
```

Between "execute tools" and "call LLM again", there's a natural pause. The conversation history is being rebuilt for the next LLM call. At this point, we check for new inputs and inject them.

### How it works

```
AGENT WAKES (input arrived while idle):
    |
1.  Drain pending inputs
    +-- For each pending input:
    |   +-- Create user message with source tag: "[signal:alice] hey"
    |   +-- Append to message store
    +-- If no pending inputs: STOP (agent goes idle)
    |
2.  Build history from message store
    |
3.  Call LLM with history + tools
    |
4.  Get response (assistant message with text + optional tool calls)
    +-- Store assistant message (internal monologue + tool calls)
    |
5.  If tool calls:
    +-- Execute tools sequentially
    +-- Store tool results
    +-- GOTO 1  <--- injection point: drain new inputs before next LLM call
    |
6.  If no tool calls:
    +-- Agent's work is done for now
    +-- GOTO 1 (check if more input arrived while it was working)
    +-- If nothing pending: STOP (agent goes idle)
```

Step 1 is the injection point. Before every LLM call, we drain any pending inputs and add them to the history. The LLM sees them as new user messages and can react.

### What the LLM sees

```
[system prompt]
...
[assistant] Let me check the deploy logs.
[assistant tool_call] exec {"command": "tail -50 /var/log/deploy.log"}
[tool] <log output>
[user] [signal:alice] hey, did the deploy finish?     <-- injected between tool rounds
[assistant] Alice is asking about the deploy. I just pulled the logs, let me check...
[assistant tool_call] message {"to": "signal:alice", "content": "Yes, deploy finished 2 minutes ago. All green."}
[tool] message sent
[assistant] Done with Alice's question. Back to what I was doing.
```

The model naturally sees the new message in context and can decide whether to address it immediately or continue with its current task.

### Compared to the current queue

Today: inputs queue up, agent processes them one at a time, each input gets a full turn.

New: inputs are injected into the running conversation. The agent sees them in real-time (at tool-call boundaries) and decides how to handle them. It might respond immediately, batch responses, or note them for later.

The `InputQueue` is replaced by a simpler mechanism -- just a thread-safe slice of pending inputs that gets drained at each loop iteration. The queue still exists conceptually (inputs can arrive faster than tool rounds), but it's no longer a sequential processing queue. It's a mailbox.

### Edge case: agent is idle

When the agent has no tool calls and emits a text-only response (or no response at all), the loop exits (step 6). The agent stops. It consumes no resources and makes no LLM calls. It stays stopped until new input arrives, at which point the loop restarts from step 1.

The agent is not a daemon that polls for work. It's event-driven: wake on input, do work, stop when done. The difference from the current model is that when the agent IS busy (in a tool loop), new inputs don't wait for the entire turn to finish -- they get injected at the next tool boundary.

### Edge case: agent is mid-stream

The LLM is actively streaming tokens (step 3). A new input arrives. We do NOT interrupt the stream. The input sits in the pending queue until the next tool round (step 5 -> step 1). If the LLM produces no tool calls, the input waits until the next iteration.

This is a deliberate choice. Interrupting a stream mid-generation would mean discarding partially-generated tokens, which is wasteful and could confuse the model. The worst-case latency for injection is one full LLM response, which is seconds, not minutes.

### Edge case: rapid-fire inputs

Multiple messages arrive between two tool rounds. All of them get injected as separate user messages before the next LLM call. The model sees all of them at once and can address them in whatever order it wants.

---

## 6. System Prompt Changes

The system prompt must communicate the new contract:

1. **Your text is private.** "When you produce text in your response, it is your internal thinking. No one sees it. Use it to reason, plan, and track state."

2. **Use tools to communicate.** "To send a message to anyone, use the `message` tool. Specify the target (`signal:<number>`, `signal:group:<id>`) and the content. This is the ONLY way to reach people."

3. **Messages arrive inline.** "User messages appear in the conversation prefixed with their source (e.g., `[signal:alice]`, `[webhook:deploy]`, `[cron:daily-check]`). You will see them appear between your tool calls. You don't need to finish what you're doing to read them."

4. **Stop when done.** "When you have nothing left to do -- no pending messages, no ongoing work -- just stop. Don't emit tool calls or text for the sake of staying active. The system will wake you when new input arrives."

---

## 7. Webhooks

### How they fit

Webhooks work exactly as before, but simpler. A POST arrives, the payload becomes a user message tagged with its source, and it gets injected into the thread.

### Before

```
POST /hook/deploy
    -> parse payload
    -> agent.Enqueue(Input{SessionID: "webhook:deploy", Content: "..."})
    -> agent processes in separate session
    -> response extracted from assistant text via event broker
    -> caller polls session history (or doesn't)
```

### After

```
POST /hook/deploy
    -> parse payload
    -> agent.Inject(Input{Source: "webhook:deploy", Content: "..."})
    -> message appears in thread: "[webhook:deploy] { ... }"
    -> agent sees it at next tool boundary (or on wake if idle)
    -> agent decides what to do: message someone, run a command, ignore it
    -> return 202 immediately (unchanged)
```

The key difference: the webhook payload lands in the same thread as everything else. The agent has full context -- it knows what Alice asked five minutes ago, what the last deploy looked like, what cron jobs are scheduled. It can correlate events across channels without any session-hopping.

### No response routing

Today, webhook callers can theoretically poll `sessions_history` to get the agent's response. That mechanism disappears (no sessions). But webhooks were already fire-and-forget by design. If the agent needs to notify someone about a webhook event, it uses the `message` tool to reach them on Signal or any other channel. The webhook caller doesn't get a response -- it gets a 202 and the agent handles the rest.

---

## 8. Heartbeat

### How it works today

`HEARTBEAT.md` in the workspace defines a response pattern. When the agent receives a health-check message (typically from a cron job), it responds with `HEARTBEAT_OK`. External monitoring observes the response (or its absence) to know if the agent is alive.

The heartbeat is a **response pattern**, not a keepalive. There's no timer in the agent. A cron job fires, injects a message, and the agent responds.

### How it works in the new model

The mechanism is the same, but the response path changes.

**Before:** Cron fires -> user message injected -> agent responds with text `HEARTBEAT_OK` -> text extracted from assistant message via event broker -> routed back.

**After:** Cron fires -> user message injected `[cron:heartbeat] health check` -> agent sees it -> agent calls `message` tool to send `HEARTBEAT_OK` to the appropriate channel.

The system prompt heartbeat section needs to be updated to reflect that the agent must use the `message` tool to respond, not just emit text.

### Heartbeat must not wake a busy agent

If the agent is already running (in a tool loop), a heartbeat cron message gets injected at the next tool boundary like any other input. The agent sees it, responds via `message`, and continues what it was doing. No special handling needed -- injection handles it naturally.

But if the agent is idle and the heartbeat fires, it wakes the agent. This is fine and expected -- the whole point is to verify the agent can wake and respond.

**The heartbeat should NOT trigger if the agent is already active.** If the agent is mid-turn, it's provably alive. Injecting a heartbeat message into an active tool loop wastes tokens (the model has to read and respond to it) and clutters the thread. The fix is simple: the heartbeat injection checks `agent.IsActive()` before injecting. If active, skip silently. If idle, inject.

```
Cron fires heartbeat
    |
Is agent active?
    +-- YES: skip, agent is provably alive
    +-- NO: inject "[cron:heartbeat] health check"
              -> agent wakes
              -> agent calls message tool with HEARTBEAT_OK
              -> agent stops (nothing else to do)
```

This keeps the thread clean. Heartbeat messages only appear in the history when the agent was genuinely idle.

### HEARTBEAT.md update

The current HEARTBEAT.md tells the agent to "respond with exactly: HEARTBEAT_OK" and "do not use tools." This must change:

```markdown
When you receive a message containing "heartbeat" or "health check",
use the `message` tool to send exactly: HEARTBEAT_OK
Send it to the same source that sent the health check.
Do not add any other text to the message.
```

---

## 9. Compaction

Compaction works the same as today, but simpler:

- No session-level compaction. There's one thread, one compaction point.
- The compaction middleware checks total context tokens before each LLM call.
- When triggered, it summarizes the history and replaces it, preserving the system prompt and recent context.
- The summary includes awareness of all channels and pending work.

The compaction pointer is stored somewhere simple -- a single row in a key-value table, or a field on a singleton "thread" record. No session ID needed.

---

## 10. Message Format in the Thread

Messages from external sources get a prefix tag so the agent knows who/what sent them:

```
[signal:+15551234567] Hey, can you check the deploy?
[signal:group:abc123] Anyone seen the latest logs?
[webhook:deploy-notify] {"status": "success", "commit": "abc1234"}
[cron:daily-check] Time for the daily status check.
```

The agent sees the source and can route its response to the right target via the `message` tool. The tag format matches the `message` tool's `to` parameter, so the agent can just copy it.

---

## 11. Tool Inventory After the Change

### Removed (7 tools)

| Tool | Why removed |
|------|-------------|
| `sessions_list` | No sessions |
| `sessions_history` | No sessions |
| `sessions_send` | No sessions |
| `sessions_spawn` | No sub-agents |
| `sessions_status` | No sessions |
| `subagents` | No sub-agents |
| `agents_list` | One agent, it knows itself |

### Kept (13 tools)

`read`, `write`, `edit`, `apply_patch`, `grep`, `glob`, `ls`, `exec`, `process`, `cron`, `message`, `memory_search`, `memory_get`.

Net: 20 -> 13. Seven tools deleted. The `message` tool becomes more central but its interface doesn't change.

---

## 12. Code Changes

### `agent/agent.go`

- Remove `sessions` field and `SessionStore` dependency.
- Remove `cancel` / `active` / `runID` concurrency machinery for queue processing.
- Replace `InputQueue` usage with a `pending` slice (thread-safe) that gets drained at the top of the loop.
- The agent is not a long-lived goroutine. It runs when there's input, processes until there's nothing left to do, and stops. External callers wake it by injecting input.

### `agent/generation.go`

- Remove `getOrCreateSession`, `createSession`, `updateSessionUsage`.
- Remove `SessionID` from all message creation.
- The main loop (`processGeneration`) becomes the agent's lifecycle, not a per-input function:

```go
// Inject adds input to the pending queue. If the agent is idle, starts processing.
func (a *Agent) Inject(input Input) {
    a.pending.Push(input)
    if a.active.CompareAndSwap(false, true) {
        go a.run(context.Background())
    }
}

// run processes until there's nothing left to do, then stops.
func (a *Agent) run(ctx context.Context) {
    defer a.active.Store(false)

    for {
        pending := a.pending.Drain()
        if len(pending) == 0 {
            return // nothing to do, stop
        }
        a.injectInputs(pending)

        for {
            hasToolCalls, err := a.streamAndHandle(ctx)
            if err != nil {
                a.publishError(err)
                return
            }
            if !hasToolCalls {
                break // agent decided it's done, check for more input
            }
            // Before next LLM call, drain any new inputs that arrived during tool execution
            if more := a.pending.Drain(); len(more) > 0 {
                a.injectInputs(more)
            }
        }
    }
}
```

### `agent/generation.go` -- `streamAndHandle`

- Remove `session` parameter.
- Remove event publishing for `EventResponse` (the assistant text is internal).
- History building reads all messages from the single thread.

### `agent/queue.go`

- Simplify to just `Push` and `Drain`. No `Pop`. No sequential processing.
- Or replace entirely with a channel that the main loop selects on.

### `agent/events.go`

- `EventResponse` can be removed or repurposed for observability.
- The event broker is no longer the delivery mechanism for user-facing messages.

### `store/` -- Session store

- Delete `SessionStore` interface and its SQLite implementation.
- Delete `Session` type.
- `MessageStore` drops `SessionID` from messages and the `ListBySession` method becomes `List` (there's only one thread).

### `cmd/miclaw/main.go`

- Signal pipeline no longer subscribes to `EventResponse` to extract replies. The `message` tool handles delivery directly.
- Signal inbound still parses envelopes, but instead of calling `agent.Enqueue(Input{SessionID: "signal:dm:..."})`, it calls `agent.Inject(Input{Source: "signal:+1555...", Content: "..."})`.
- Webhook handler does the same: `agent.Inject(Input{Source: "webhook:deploy", Content: "..."})`.

### `tools/message.go`

- Already exists. May need to gain direct access to Signal/webhook send functions instead of going through the event system.
- The tool becomes the sole outbound path for all user communication.

### Tools to delete

- `tools/sessions_list.go`, `tools/sessions_history.go`, `tools/sessions_send.go`, `tools/sessions_spawn.go`, `tools/sessions_status.go`, `tools/subagents.go`, `tools/agents_list.go` -- all deleted.

---

## 13. What This Gains

| Before | After |
|--------|-------|
| Agent is deaf during tool chains | Agent sees new messages at every tool boundary |
| Agent must finish to speak | Agent speaks via tool call, keeps working |
| N sessions, fragmented context | One thread, full context |
| Sub-agents for parallel research | Agent does its own research, or uses background `exec` |
| 20 tools | 13 tools |
| Event broker as delivery mechanism | `message` tool delivers directly |
| Assistant text = user-facing response | Assistant text = private internal monologue |
| Complex session routing | No routing. One thread. |

---

## 14. What This Costs

**Token usage increases.** One thread means the full history is in every LLM call. Cross-channel context is included even when irrelevant. Compaction mitigates this, but the steady-state context will be larger than per-session contexts were.

**The model must be disciplined.** It must remember to use `message` to communicate. If it forgets and just emits text, nobody sees it. The system prompt must be clear and the model must be capable enough to follow the instruction reliably.

**No parallelism.** Sub-agents allowed the main agent to dispatch research in parallel. With one thread and one model call at a time, everything is sequential. This is acceptable because: (a) sub-agents were expensive (full re-prompts), (b) background `exec` still allows parallel shell commands, (c) the simplicity gain outweighs the parallelism loss.

**Injection latency.** New inputs are only seen at tool boundaries. If the agent is mid-stream (LLM is generating), the input waits. Worst case is one full LLM response time (a few seconds). This is much better than the current model (waits for the entire turn, possibly minutes) but not instant.
