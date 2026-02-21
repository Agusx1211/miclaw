# Miclaw Architecture Overview

Miclaw is a lean Go reimplementation of openclaw. One agent. One thread. Minimal surface area.

## Design Principles

1. **Singleton agent.** There is exactly one agent process. All input channels (Signal, webhooks) feed into one conversation thread. The agent decides when to spawn sub-agents; nothing else does.
2. **No plugin system.** No marketplace. No dynamic loading. Skills are directories with markdown files checked into the workspace.
3. **Crash on bad state.** No defensive nil guards, no fallback paths. If something is wrong, panic. Tests catch it.
4. **Fewer tools, same power.** 20 tools, no browser, no canvas, no gateway admin. If the agent needs something, it uses `exec`.

## System Map

```
                    +-----------------+
                    |   Signal        |
                    |   (SSE + RPC)   |
                    +--------+--------+
                             |
                             v
+--------------+    +--------+--------+    +--------------+
|  Webhooks    |--->|   Singleton     |<-->|   Config     |
|  (HTTP POST) |    |   Agent Loop    |    |   (JSON)     |
+--------------+    +--------+--------+    +--------------+
                             |
              +--------------+--------------+
              |              |              |
       +------+-----+ +-----+------+ +-----+------+
       | System      | | Tool       | | Sub-agent  |
       | Prompt      | | Execution  | | Spawning   |
       | Builder     | | Pipeline   | | (read-only)|
       +------+-----+ +-----+------+ +-----+------+
              |              |
       +------+-----+ +-----+------+
       | Workspace   | | LLM        |
       | Bootstrap   | | Backend    |
       | (SOUL.md,   | | (LM Studio |
       |  MEMORY.md, | |  OpenRouter |
       |  skills/)   | |  Codex)    |
       +------+-----+ +------------+
              |
       +------+-----+
       | Memory      |
       | (SQLite +   |
       |  vectors)   |
       +-------------+
```

## Core Subsystems

### 1. System Prompt, Memory & Skills
**Document:** [01-system-prompt-memory-skills.md](./01-system-prompt-memory-skills.md)

The system prompt is built dynamically at runtime from workspace files (SOUL.md, AGENTS.md, MEMORY.md), skills (SKILL.md files), and runtime info. Memory uses SQLite with hybrid vector+FTS search. Skills are discovered from the workspace `skills/` directory only.

### 2. Agent Loop & Sub-agents
**Document:** [02-agent-loop-and-subagents.md](./02-agent-loop-and-subagents.md)

One agent loop processes all input. Messages arrive from Signal or webhooks, get appended to the single conversation thread, and drive the agent loop: build prompt, call LLM, stream response, execute tool calls, repeat. Sub-agents are read-only child sessions spawned by the main agent.

### 3. Tools
**Document:** [03-tools.md](./03-tools.md)

20 tools in total: `read`, `write`, `edit`, `apply_patch`, `grep`, `glob`, `ls`, `exec`, `process`, `cron`, `message`, `agents_list`, `sessions_list`, `sessions_history`, `sessions_send`, `sessions_spawn`, `subagents`, `sessions_status`, `memory_search`, `memory_get`. No browser, no canvas, no web_search, no web_fetch, no gateway admin.

### 4. Signal Integration
**Document:** [04-signal-integration.md](./04-signal-integration.md)

Signal via `signal-cli` HTTP daemon. SSE for inbound, JSON-RPC for outbound. DMs and groups route into the singleton agent. Markdown converted to Signal text styles. Message chunking at 4000 chars.

### 5. Webhooks
**Document:** [05-webhooks.md](./05-webhooks.md)

HTTP endpoints that inject content into the agent thread. When a webhook fires, its payload becomes a user message in the agent's conversation. The agent wakes up and processes it like any other input. No new threads, no new sub-agents (unless the agent decides to spawn one).

### 6. Context Compaction
**Document:** [06-context-compaction.md](./06-context-compaction.md)

When context tokens exceed the threshold, the conversation is summarized by the LLM and replaced with a compressed version. The system prompt is preserved. The last user intent is preserved. Everything else becomes a structured summary.

### 7. LLM Backends
**Document:** [07-llm-backends.md](./07-llm-backends.md)

Three backends only: LM Studio (local, OpenAI-compatible), OpenRouter (cloud, multi-model), OpenAI Codex (OAuth or API key). All use streaming. All support tool calling.

## Data Flow: Complete Request Lifecycle

```
1. Input arrives (Signal SSE event OR webhook POST)
   |
2. Parse and validate sender/payload
   |
3. Append as user message to singleton agent thread
   |
4. Agent Loop begins:
   a. Middleware: check compaction threshold
   b. If compaction needed: summarize, compress, reset
   c. Build system prompt (workspace files, skills, memory, runtime)
   d. Send messages + tools to LLM backend
   e. Stream response, process events
   f. Execute tool calls sequentially
   g. If more tool calls: loop back to (a)
   h. Else: return final response
   |
5. Route response back to origin channel
   - Signal: markdown -> text styles, chunk at 4000 chars, send via RPC
   - Webhook: response stored, caller can poll or receive callback
```

## Configuration

```
~/.miclaw/
+-- config.json               # Main configuration
+-- workspace/                 # Agent workspace
|   +-- SOUL.md                # Agent personality/tone
|   +-- AGENTS.md              # Agent instructions
|   +-- MEMORY.md              # Semantic memory
|   +-- IDENTITY.md            # User identity info
|   +-- USER.md                # User preferences
|   +-- HEARTBEAT.md           # Heartbeat response pattern
|   +-- skills/                # Workspace skills
|   |   +-- {name}/
|   |       +-- SKILL.md
|   +-- memory/                # Memory file fragments
|       +-- *.md
+-- state/
|   +-- memory/
|       +-- agent.sqlite       # Memory database
+-- sessions/                  # Session storage
```

## What We Don't Have (And Won't)

- No multi-agent orchestration. One agent.
- No plugin system. No hook marketplace. No npm packages.
- No browser tool. No canvas tool.
- No web_search or web_fetch tools (use exec + curl if needed).
- No gateway admin tool.
- No sandbox/container system.
- No permission profiles or policy pipelines. Tool access is all-or-nothing per agent type (main vs sub-agent).
- No Anthropic, Google, or Ollama backends.
- No pairing system for DMs (use allowlist or open).
