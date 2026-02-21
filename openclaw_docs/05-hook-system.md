# Hook System

## 1. Overview

OpenClaw has two distinct hook systems:

1. **Internal Hooks** - Event-driven automation scripts that respond to agent commands, lifecycle events, and system events
2. **Plugin Hooks** - Lifecycle callbacks in the tool/agent pipeline that can intercept and modify behavior

Both systems are complementary and serve different purposes.

---

## 2. Internal Hooks

### Hook Definition

A hook is a directory containing:

```
my-hook/
├── HOOK.md          # Metadata (YAML frontmatter) + documentation
└── handler.ts       # Handler implementation (or handler.js/index.ts/index.js)
```

### HOOK.md Format

```yaml
---
name: session-memory
description: "Save session context to memory when /new command is issued"
homepage: https://docs.openclaw.ai/automation/hooks#session-memory
metadata:
  openclaw:
    emoji: "💾"
    events: ["command:new"]
    export: "default"
    homepage: "https://..."
    requires:
      bins: ["git"]           # All required
      anyBins: ["npm", "yarn"] # At least one
      env: ["MY_API_KEY"]
      config: ["workspace.dir"]
    os: ["linux", "darwin"]
    always: false
    install:
      - id: "bundled"
        kind: "bundled"
        label: "Bundled with OpenClaw"
---

# Hook Name

Detailed markdown documentation...
```

### Hook Metadata Schema

```typescript
type OpenClawHookMetadata = {
  always?: boolean;            // Bypass eligibility checks
  hookKey?: string;            // Override config key
  emoji?: string;              // Display emoji
  homepage?: string;           // Documentation URL
  events: string[];            // Events this hook handles
  export?: string;             // Export name (default: "default")
  os?: string[];               // Required OS (linux, darwin, win32)
  requires?: {
    bins?: string[];           // All binaries must exist on PATH
    anyBins?: string[];        // At least one binary must exist
    env?: string[];            // Required environment variables
    config?: string[];         // Required config paths (must be truthy)
  };
  install?: HookInstallSpec[];
};
```

### Configuration

```typescript
type InternalHooksConfig = {
  enabled?: boolean;                     // Master switch
  handlers?: InternalHookHandlerConfig[];  // Legacy handler list
  entries?: Record<string, HookConfig>;  // Per-hook overrides
  load?: {
    extraDirs?: string[];                // Additional hook directories
  };
  installs?: Record<string, HookInstallRecord>;
};

type HookConfig = {
  enabled?: boolean;                     // Enable/disable specific hook
  env?: Record<string, string>;          // Environment for hook
  [key: string]: unknown;                // Hook-specific config keys
};
```

Example:
```json
{
  "hooks": {
    "internal": {
      "enabled": true,
      "entries": {
        "session-memory": {
          "enabled": true,
          "messages": 25
        },
        "command-logger": {
          "enabled": true
        }
      },
      "load": {
        "extraDirs": ["./custom-hooks"]
      }
    }
  }
}
```

---

## 3. Hook Events

### Event Types

```typescript
type InternalHookEventType = "command" | "session" | "agent" | "gateway" | "message";
```

### Complete Event List

| Event | Trigger | Can Modify |
|-------|---------|------------|
| `command` | Any command issued | Messages only |
| `command:new` | `/new` command | Messages only |
| `command:reset` | `/reset` command | Messages only |
| `command:stop` | `/stop` command | Messages only |
| `agent:bootstrap` | Before workspace bootstrap injection | **bootstrapFiles (mutable)** |
| `gateway:startup` | 250ms after gateway starts | Messages only |
| `message:received` | Inbound message from any channel | Messages only |
| `message:sent` | Outbound message sent | Messages only |

### Event Trigger Locations

| Event | File |
|-------|------|
| `command:new/reset/stop` | `src/auto-reply/reply/commands-core.ts`, `commands-session.ts` |
| `gateway:startup` | `src/gateway/server-startup.ts` (line 141-148, 250ms delay) |
| `message:received/sent` | `src/auto-reply/reply/dispatch-from-config.ts` |
| `agent:bootstrap` | `src/agents/bootstrap-hooks.ts` |

---

## 4. Hook Execution

### Hook Event Object

```typescript
interface InternalHookEvent {
  type: InternalHookEventType;     // "command", "agent", etc.
  action: string;                  // "new", "bootstrap", "startup", etc.
  sessionKey: string;              // Current session key
  context: Record<string, unknown>; // Event-specific data
  timestamp: Date;                 // When event occurred
  messages: string[];              // Hooks can push messages here
}
```

### Execution Engine

```typescript
async function triggerInternalHook(event: InternalHookEvent): Promise<void> {
  // Get handlers for the event type (e.g., "command")
  const typeHandlers = handlers.get(event.type) ?? [];
  // Get handlers for specific event (e.g., "command:new")
  const specificHandlers = handlers.get(`${event.type}:${event.action}`) ?? [];

  const allHandlers = [...typeHandlers, ...specificHandlers];

  // Execute handlers sequentially in registration order
  for (const handler of allHandlers) {
    try {
      await handler(event);
    } catch (err) {
      console.error(`Hook error [${event.type}:${event.action}]: ${err.message}`);
      // Error does NOT prevent other hooks from running
    }
  }
}
```

### Event Context by Type

**Command Events:**
```typescript
{
  sessionEntry?: SessionEntry,
  sessionId?: string,
  sessionFile?: string,
  commandSource?: string,    // "whatsapp", "telegram", "signal"
  senderId?: string,
  workspaceDir?: string,
  cfg?: OpenClawConfig,
}
```

**Message Received:**
```typescript
{
  from: string,
  content: string,
  timestamp?: number,
  channelId: string,         // "whatsapp", "telegram", "signal"
  accountId?: string,
  conversationId?: string,
  messageId?: string,
  metadata?: {
    to?: string,
    provider?: string,
    surface?: string,
    threadId?: string,
    senderId?: string,
    senderName?: string,
    senderUsername?: string,
    senderE164?: string,
  }
}
```

**Agent Bootstrap:**
```typescript
{
  workspaceDir: string,
  bootstrapFiles: WorkspaceBootstrapFile[],  // MUTABLE!
  cfg?: OpenClawConfig,
  sessionKey?: string,
  sessionId?: string,
  agentId?: string,
}
```

---

## 5. Hook Behavior Modification

### Message Injection

All hooks can push messages to the user:

```typescript
// Inside hook handler:
event.messages.push("Session saved to memory.");
```

Messages are sent to the user immediately after all hooks execute (for command events).

### Bootstrap File Mutation

Only `agent:bootstrap` hooks can mutate the context:

```typescript
// Inside agent:bootstrap hook handler:
const context = event.context as AgentBootstrapHookContext;
context.bootstrapFiles.push({
  name: "CUSTOM.md",
  content: "Custom context to inject into system prompt",
  type: "custom",
});
```

The mutated `bootstrapFiles` array is used for actual system prompt construction.

---

## 6. Hook Discovery & Loading

### Discovery Sources (in precedence order)

1. **Workspace hooks:** `<workspace>/hooks/`
2. **Managed hooks:** `~/.openclaw/hooks/`
3. **Bundled hooks:** `<openclaw>/dist/hooks/bundled/`

### Loading Process

```
Gateway Startup
    ↓
clearInternalHooks()          // Clear previously registered
    ↓
loadInternalHooks(config, workspaceDir)
    ↓
For each directory (workspace → managed → bundled):
  1. Find subdirectories containing HOOK.md
  2. Parse HOOK.md frontmatter for metadata
  3. Check eligibility:
     a. OS requirement matches?
     b. Required binaries on PATH?
     c. Required env vars set?
     d. Required config paths truthy?
     e. Respect "always" flag (bypass checks)
  4. Check if disabled in config (entries.<name>.enabled == false)
  5. Verify path safety (realpath, no traversal)
  6. Import handler module (cache-busted):
     const url = pathToFileURL(handlerPath).href;
     const cacheBustedUrl = `${url}?t=${Date.now()}`;
     const mod = await import(cacheBustedUrl);
  7. Get handler function (default or named export)
  8. Register handler for all events listed in metadata
    ↓
Also load legacy config handlers (handlers[] array)
    ↓
Return count of loaded handlers
    ↓
After channels start (250ms delay):
  Trigger gateway:startup event
```

### Handler Implementation Pattern

```typescript
import type { HookHandler } from "../../src/hooks/hooks.js";

const handler: HookHandler = async (event) => {
  if (event.type !== "command" || event.action !== "new") {
    return;  // Only handle command:new
  }

  // Access event data
  const { sessionKey, context, timestamp } = event;

  // Do work...

  // Optionally send message to user
  event.messages.push("Hook executed successfully.");
};

export default handler;
```

---

## 7. Built-in Hooks

### session-memory

| Property | Value |
|----------|-------|
| **Events** | `command:new` |
| **Purpose** | Save session context to memory when `/new` is issued |
| **Output** | `~/.openclaw/workspace/memory/YYYY-MM-DD-slug.md` |
| **Requires** | `workspace.dir` config |
| **Config** | `messages` (number, default 15): messages to include |
| **LLM Usage** | Calls configured LLM to generate filename slug |

### bootstrap-extra-files

| Property | Value |
|----------|-------|
| **Events** | `agent:bootstrap` |
| **Purpose** | Inject additional workspace files via glob patterns |
| **Requires** | `workspace.dir` config |
| **Config** | `paths` / `patterns` / `files` (string[]): glob patterns |
| **Mutation** | Adds matching files to `context.bootstrapFiles` |

### command-logger

| Property | Value |
|----------|-------|
| **Events** | `command` (all command events) |
| **Purpose** | Audit log of all commands |
| **Output** | `~/.openclaw/logs/commands.log` (JSONL) |
| **Requires** | None |
| **Format** | `{"timestamp":"...","action":"new","sessionKey":"...","senderId":"...","source":"telegram"}` |

### boot-md

| Property | Value |
|----------|-------|
| **Events** | `gateway:startup` |
| **Purpose** | Run `BOOT.md` when gateway starts |
| **Requires** | `workspace.dir` config |
| **Behavior** | Executes BOOT.md in each agent's workspace if it exists |

---

## 8. Plugin Hooks (Lifecycle Callbacks)

### Plugin Hook Events

| Event | When | Can Modify |
|-------|------|------------|
| `before_tool_call` | Before any tool executes | Block call, modify params |
| `after_tool_call` | After tool executes | Observe results |
| `tool_result_persist` | Before persisting results | Modify results |
| `before_agent_start` | Before agent run | Override model/provider |
| `before_prompt_build` | Before system prompt | Modify prompt/context |
| `llm_input` | Before LLM API call | Observe request |
| `llm_output` | After LLM response | Observe response |
| `agent_end` | After agent completes | Observe results |
| `before_compaction` | Before context compaction | Observe |
| `after_compaction` | After compaction | Observe |
| `before_reset` | Before session reset | Observe |
| `before_message_write` | Before message persistence | Block/modify |
| `gateway_start` | Gateway startup | Observe |
| `gateway_stop` | Gateway shutdown | Observe |
| `message_received` | Inbound message | Observe |
| `message_sending` | Before outbound message | Cancel/modify |
| `message_sent` | After outbound message | Observe |

### Global Hook Runner

```typescript
// File: src/plugins/hook-runner-global.ts

const hookRunner = getGlobalHookRunner();

// Check if hooks exist for event
if (hookRunner?.hasHooks("before_tool_call")) {
  const result = await hookRunner.runBeforeToolCall({
    toolName, params, toolCallId, ctx
  });
  if (result.blocked) {
    return { error: "Blocked by hook" };
  }
  params = result.params;  // May be modified
}
```

### Plugin Hook vs Internal Hook

| Aspect | Internal Hooks | Plugin Hooks |
|--------|---------------|-------------|
| **Definition** | HOOK.md + handler.ts directories | Plugin code |
| **Events** | command, agent, gateway, message | Tool calls, LLM lifecycle |
| **Can block** | No (messages only) | Yes (tool calls, messages) |
| **Can modify** | Bootstrap files only | Params, prompts, models |
| **Discovery** | Directory scanning | Plugin registry |
| **Execution** | Sequential, error-isolated | Priority-sorted, error-isolated |

---

## 9. Hook CLI Management

### List Hooks

```bash
openclaw hooks list [--eligible] [--json] [--verbose]
```

### Get Hook Info

```bash
openclaw hooks info <name> [--json]
```

### Enable/Disable

```bash
openclaw hooks enable <name>
# Sets config: hooks.internal.entries.<name>.enabled = true

openclaw hooks disable <name>
# Sets config: hooks.internal.entries.<name>.enabled = false
# Requires gateway restart
```

### Install Hooks

```bash
# From local directory
openclaw hooks install ./path/to/hook

# From npm package
openclaw hooks install @acme/my-hooks [--pin]

# From archive
openclaw hooks install ./hooks.tar.gz

# Link instead of copy
openclaw hooks install ./path --link
# Adds to hooks.internal.load.extraDirs
```

### Update Hooks

```bash
openclaw hooks update <id|--all> [--dry-run]
# Updates installed hook packs (npm only)
# Checks integrity hash before updating
```

### Hook Pack Format (npm)

```json
{
  "name": "@acme/my-hooks",
  "version": "0.1.0",
  "openclaw": {
    "hooks": ["./hooks/my-hook", "./hooks/other-hook"]
  }
}
```

Each entry must contain HOOK.md + handler file and stay within package boundary.

---

## 10. Error Handling

### Load-Time Failures

| Failure | Result |
|---------|--------|
| Missing HOOK.md | Hook not loaded |
| Missing handler file | Hook not loaded |
| Handler not a function | Hook not loaded |
| Missing events in metadata | Hook skipped with warning |
| Import error | Hook not loaded, error logged |

### Runtime Failures

| Failure | Result |
|---------|--------|
| Hook handler throws | Error logged, other hooks still execute |
| Hook handler rejects | Error logged, other hooks still execute |
| Timeout | No built-in timeout (hooks should be fast) |

**Critical:** Hook errors never prevent other hooks from running and never crash the gateway.

---

## 11. Complete Data Flow Examples

### Command Event Flow

```
User sends /new command
    ↓
handleNewCommand() [commands-session.ts]
    ↓
Create hook event:
  type: "command"
  action: "new"
  sessionKey: "agent:main:main"
  context: { sessionEntry, sessionId, ... }
    ↓
triggerInternalHook(event)
├── Get handlers for "command" (type-level)
├── Get handlers for "command:new" (specific)
├── Execute all in order:
│   ├── session-memory hook
│   │   └── Extract recent messages
│   │   └── Generate slug via LLM
│   │   └── Write memory/YYYY-MM-DD-slug.md
│   │   └── Push message: "Session saved"
│   │
│   └── command-logger hook
│       └── Write JSONL to commands.log
│
└── If event.messages has content:
    └── Send messages to user
```

### Bootstrap Event Flow

```
Agent session starting
    ↓
resolveBootstrapFilesForRun()
    ↓
applyBootstrapHookOverrides()
    ↓
Create hook event:
  type: "agent"
  action: "bootstrap"
  context: {
    workspaceDir: "~/.openclaw/workspace",
    bootstrapFiles: [SOUL.md, AGENTS.md, ...],  // MUTABLE
    sessionKey: "agent:main:main"
  }
    ↓
triggerInternalHook(event)
├── bootstrap-extra-files hook
│   └── Read glob patterns from config
│   └── Load matching files
│   └── MUTATE context.bootstrapFiles (add files)
│
└── Return mutated context.bootstrapFiles
    ↓
Build system prompt with augmented bootstrap files
```
