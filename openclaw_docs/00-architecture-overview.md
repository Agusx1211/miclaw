# Miclaw Architecture Overview

This document provides a high-level overview of the entire system architecture, describing how all subsystems interact. Each subsystem has its own detailed document.

## System Map

```
                         +-----------------------+
                         |    Signal Channel     |
                         |  (SSE + JSON-RPC)     |
                         +----------+------------+
                                    |
                                    | inbound messages
                                    v
+----------------+       +----------+------------+       +------------------+
|   Hook System  |<----->|     Gateway / Main    |<----->|  Config System   |
| (event-driven) |       |     Event Router      |       | (JSON + agents)  |
+----------------+       +----------+------------+       +------------------+
                                    |
                         +----------+------------+
                         |     Agent Loop        |
                         |  (conversation core)  |
                         +----------+------------+
                                    |
                    +---------------+---------------+
                    |               |               |
            +-------+----+  +------+------+  +-----+-------+
            | System     |  | Tool        |  | Sub-agent   |
            | Prompt     |  | Execution   |  | Spawning    |
            | Builder    |  | Pipeline    |  | (Task)      |
            +-------+----+  +------+------+  +-----+-------+
                    |               |               |
            +-------+----+  +------+------+         |
            | Workspace  |  | Permission  |         |
            | Bootstrap  |  | System      |    (child sessions
            | (SOUL.md,  |  | (policies,  |     with limited
            |  MEMORY.md |  |  profiles,  |     tool sets)
            |  AGENTS.md |  |  approvals) |
            |  SKILLS/)  |  +-------------+
            +------------+
                    |
            +-------+----+
            | Memory     |
            | Search     |
            | (SQLite +  |
            |  vectors)  |
            +------------+
```

## Core Subsystems

### 1. System Prompts, Memory, Skills & Workspaces
**Document:** [01-system-prompts-memory-skills-workspaces.md](./01-system-prompts-memory-skills-workspaces.md)

The system prompt is the agent's operating manual. It is dynamically constructed at runtime by `buildAgentSystemPrompt()` with 24+ parameters, generating a 20-150KB prompt. Key inputs:

- **Workspace bootstrap files** (SOUL.md, AGENTS.md, TOOLS.md, MEMORY.md, etc.) are loaded from the workspace directory, filtered by session type (subagents only get AGENTS.md + TOOLS.md), and injected into the prompt.
- **Skills** are discovered from workspace, managed, and bundled directories as SKILL.md files. Up to 150 skills / 30KB are formatted into the prompt. The agent is instructed to scan skill descriptions and read the matching SKILL.md before responding.
- **Memory** uses a SQLite-backed vector store with hybrid search (70% vector, 30% FTS5). Two tools (`memory_search`, `memory_get`) allow the agent to query MEMORY.md and session transcripts.
- **Workspaces** are per-agent directories (default `~/.openclaw/workspace`) containing bootstrap files. Resolution order: agent config > defaults config > default path.

### 2. Agent Loop & Sub-agents
**Document:** [02-agent-loop-and-subagents.md](./02-agent-loop-and-subagents.md)

The agent loop is the core conversation engine:

1. User message arrives (from Signal, CLI, etc.)
2. Message appended to conversation history
3. **Middleware pipeline** runs `before_turn` checks (e.g., auto-compact if tokens >= threshold)
4. Full history + system prompt + tools sent to LLM provider via streaming API
5. Response streamed back, events processed (text deltas, thinking, tool calls)
6. If response contains tool calls: execute each tool, append results to history, loop back to step 3
7. If no tool calls: return final response

**Sub-agents** are spawned via the `sessions_spawn` / `agent` tool. They get:
- A new child session (linked to parent via `ParentSessionID`)
- A limited tool set (read-only: Glob, Grep, LS, View - no write/exec/spawn)
- Their own conversation loop running autonomously
- Costs propagated back to parent session

### 3. Tools & Filesystem Access
**Document:** [03-tools-and-filesystem.md](./03-tools-and-filesystem.md)

The tool system provides 20+ tools organized into groups:
- **Filesystem:** read, write, edit, apply_patch (with workspace containment and sandbox bridging)
- **Runtime:** exec (shell commands with sandbox/gateway/node routing), process (background management)
- **Web:** web_search, web_fetch
- **Memory:** memory_search, memory_get
- **Sessions:** sessions_list, sessions_history, sessions_send, sessions_spawn, subagents
- **Messaging:** message (cross-channel)
- **UI:** browser, canvas
- **Automation:** cron, gateway

Tools pass through a **policy pipeline** (profile > provider > agent > group > sandbox > subagent depth), then are wrapped with hooks, abort signal support, and schema normalization for the target provider (Anthropic/OpenAI/Gemini).

### 4. Signal Integration
**Document:** [04-signal-integration.md](./04-signal-integration.md)

Signal integration uses `signal-cli` via HTTP JSON-RPC + Server-Sent Events:

- **Inbound:** SSE stream from `/api/v1/events` with exponential backoff reconnection
- **Outbound:** JSON-RPC POST to `/api/v1/rpc` with method="send"
- **Message flow:** SSE event > parse envelope > validate sender > access control > mention rendering > history tracking > debounce > agent dispatch > format response > chunk (4000 chars) > send with text styles
- **Groups vs DMs:** Isolated sessions per group, shared sessions per DM sender
- **Attachments:** Fetched via RPC, max 8MB, stored locally with timestamp naming
- **Reactions:** Full support for send/receive/remove with configurable notification levels
- **Multi-account:** Multiple Signal accounts with per-account configuration

### 5. Hook System
**Document:** [05-hook-system.md](./05-hook-system.md)

Two hook systems:

**Internal Hooks** - Event-driven automation scripts:
- Events: `command:new/reset/stop`, `agent:bootstrap`, `gateway:startup`, `message:received/sent`
- Hooks are directories with `HOOK.md` (metadata) + `handler.ts` (implementation)
- Discovered from: workspace/hooks > ~/.openclaw/hooks > bundled
- Can inject messages and mutate bootstrap files
- Built-in hooks: session-memory, bootstrap-extra-files, command-logger, boot-md

**Plugin Hooks** - Lifecycle hooks in the tool/agent pipeline:
- Events: `before_tool_call`, `after_tool_call`, `before_agent_start`, `llm_input/output`, etc.
- Can block tool calls, modify parameters, override model selection

### 6. Context Compaction
**Document:** [06-context-compaction.md](./06-context-compaction.md)

When context tokens reach the threshold (default 200,000), compaction runs **before the next LLM turn**:

1. Clean message history (fill missing tool responses, ensure valid message order)
2. Find last user message to preserve intent
3. Send entire conversation + 7-section summarization prompt to LLM
4. Replace all messages with: [system prompt] + [summary + last user request as user message]
5. Recount tokens via API probe (16 max_tokens request)
6. Reset session ID, save interaction, reset middleware

The summarization prompt requests 7 sections: Goals, Timeline, Technical Decisions, Files/Changes, Active Work (critical), Unresolved Issues, Next Step.

**Note:** No separate heartbeat system exists. The only keepalive mechanism is HTTP client timeouts (720s default).

## Data Flow: Complete Request Lifecycle

```
1. Signal SSE event received
   ↓
2. Parse envelope (sender, message, group, attachments)
   ↓
3. Validate: self-loop check, access control (dm/group policy)
   ↓
4. Process: render mentions, record group history, fetch attachments
   ↓
5. Debounce (group rapid messages from same sender)
   ↓
6. Send typing indicator
   ↓
7. Route to agent session (DM: signal:<phone>, Group: agent:<id>:signal:group:<gid>)
   ↓
8. Agent Loop begins:
   a. Middleware: check compaction threshold
   b. If compaction needed: summarize → compress → reset
   c. Build/update system prompt (workspace files, skills, memory config, runtime info)
   d. Send messages + tools to LLM
   e. Stream response, process events
   f. Execute tool calls (with permission checks, hooks, sandboxing)
   g. If more tool calls: loop; else: return response
   ↓
9. Format response: markdown → Signal text styles
   ↓
10. Chunk response (4000 char limit)
    ↓
11. Send each chunk via JSON-RPC
    ↓
12. Stop typing indicator
    ↓
13. Hook: message:sent fires
```

## Key Architectural Principles

1. **Dynamic prompt construction** - The system prompt is rebuilt contextually for each session type, incorporating workspace files, skills, runtime info, and capabilities.

2. **Layered tool permissions** - Tools pass through a multi-step policy pipeline that considers profiles, providers, agents, groups, sandbox state, and subagent depth.

3. **Session hierarchy** - Main sessions spawn child sessions for sub-agents and title generation, with cost propagation upward.

4. **Channel abstraction** - Signal (and other channels) are abstracted behind a common inbound/outbound adapter pattern, allowing the agent loop to be channel-agnostic.

5. **Event-driven hooks** - Both internal hooks (HOOK.md scripts) and plugin hooks (lifecycle callbacks) allow customization without modifying core code.

6. **Graceful degradation** - Context compaction prevents context window overflow, exponential backoff handles rate limits, and hook failures don't block the main loop.

## Configuration Hierarchy

```
~/.openclaw/config.json          (global config)
├── agents.list[]                (per-agent definitions)
│   ├── id, name, model
│   ├── workspace               (agent-specific workspace path)
│   ├── tools.profile/allow/deny (tool permissions)
│   ├── memorySearch.*           (memory search config)
│   └── sandbox.*               (sandbox config)
├── agents.defaults              (fallback agent config)
│   ├── workspace, bootstrapMaxChars, bootstrapTotalMaxChars
│   ├── userTimezone, timeFormat
│   └── heartbeat.prompt
├── channels.signal.*            (Signal config)
│   ├── account, httpUrl/Host/Port
│   ├── dmPolicy, groupPolicy, allowFrom
│   └── historyLimit, textChunkLimit, mediaMaxMb
├── hooks.internal.*             (hook config)
│   ├── enabled, entries.{hookName}.enabled
│   └── load.extraDirs
├── memory.*                     (memory config)
│   └── citations: "on" | "off" | "auto"
└── tools.*                      (global tool policies)
    ├── profile, allow, deny
    └── exec.{security, ask, host, timeoutSec}
```

## File Structure Reference

```
~/.openclaw/
├── config.json                  # Main configuration
├── workspace/                   # Default agent workspace
│   ├── SOUL.md                  # Agent personality/tone
│   ├── AGENTS.md                # Agent definitions (symlinked as CLAUDE.md)
│   ├── TOOLS.md                 # User tool guides
│   ├── IDENTITY.md              # User identity info
│   ├── USER.md                  # User preferences
│   ├── MEMORY.md                # Semantic memory
│   ├── BOOTSTRAP.md             # Git/workspace notes
│   ├── HEARTBEAT.md             # Heartbeat pattern
│   ├── BOOT.md                  # Gateway startup script
│   ├── skills/                  # Workspace-level skills
│   │   └── {skill-name}/
│   │       └── SKILL.md
│   └── hooks/                   # Workspace-level hooks
│       └── {hook-name}/
│           ├── HOOK.md
│           └── handler.ts
├── hooks/                       # Managed (user-installed) hooks
├── state/
│   └── memory/
│       └── {agentId}.sqlite     # Per-agent memory database
├── exec-approvals.json          # Shell execution allowlist
└── exec-approvals.sock          # Approval server socket
```
