# Tools & Filesystem Access

## 1. Tool System Overview

### Tool Interface

Every tool implements this interface:

```typescript
interface AgentTool<TParams, TDetails> {
  name: string;              // Canonical lowercase name (e.g., "read", "exec")
  label?: string;            // Display name
  description: string;       // Description sent to the model
  parameters: JSONSchema;    // JSON Schema for input validation
  execute(
    toolCallId: string,
    params: TParams,
    signal?: AbortSignal,
    onUpdate?: (update: unknown) => void,
  ): Promise<AgentToolResult<TDetails>>;
  ownerOnly?: boolean;       // Restrict to owner senders
}
```

### Complete Tool Inventory

| Tool | Group | Description | Main Agent | Sub-agent |
|------|-------|-------------|-----------|-----------|
| `read` | fs | Read files with adaptive paging | Yes | Yes |
| `write` | fs | Write/create files | Yes | No |
| `edit` | fs | Edit specific file sections | Yes | No |
| `apply_patch` | fs | Apply unified diffs (OpenAI only) | Yes | No |
| `grep` | fs | Search file contents | Yes | Yes |
| `find` / `glob` | fs | Find files by pattern | Yes | Yes |
| `ls` | fs | List directories | Yes | Yes |
| `exec` | runtime | Execute shell commands | Yes | No |
| `process` | runtime | Monitor background processes | Yes | No |
| `web_search` | web | Search the internet | Yes | No |
| `web_fetch` | web | Fetch and parse web pages | Yes | No |
| `browser` | ui | Control headless browser | Yes | No |
| `canvas` | ui | Render visualizations | Yes | No |
| `nodes` | infra | List/manage node hosts | Yes | No |
| `cron` | automation | Schedule recurring tasks | Yes (owner) | No |
| `message` | messaging | Send cross-channel messages | Yes | No |
| `gateway` | admin | System operations | Yes (owner) | No |
| `agents_list` | introspection | List agents | Yes | No |
| `sessions_list` | sessions | List active sessions | Yes | Leaf: No |
| `sessions_history` | sessions | Get session history | Yes | Leaf: No |
| `sessions_send` | sessions | Send to sessions | Yes | No |
| `sessions_spawn` | sessions | Spawn sub-agents | Yes | Leaf: No |
| `subagents` | sessions | List sub-agents | Yes | No |
| `session_status` | sessions | Current session status | Yes | No |
| `memory_search` | memory | Semantic memory search | Yes | No |
| `memory_get` | memory | Read memory snippets | Yes | No |
| `image` | media | Process/analyze images | Yes | No |

---

## 2. Tool Assembly Pipeline

### Assembly Function

```typescript
function createOpenClawCodingTools(options: {
  exec?: ExecToolDefaults & ProcessToolDefaults;
  messageProvider?: string;
  agentAccountId?: string;
  sandbox?: SandboxContext | null;
  sessionKey?: string;
  config?: OpenClawConfig;
  modelProvider?: string;
  modelId?: string;
  groupId?: string | null;
  senderIsOwner?: boolean;
  // ... 20+ additional options
}): AgentTool[]
```

### Assembly Steps

```
Step 1: Load Base Coding Tools
├── Import from pi-coding-agent library
├── Filter out bash/exec (replaced by OpenClaw exec)
├── Conditionally include write/edit (disabled in sandbox read-only)
├── Wrap read tool with image sanitization and adaptive paging
└── Result: [read, write?, edit?, grep, find, ls]

Step 2: Create Execution Tools
├── Create exec tool (with sandbox, host, security config)
├── Create process tool (background process management)
├── Create apply_patch tool (if OpenAI provider)
└── Result: [exec, process, apply_patch?]

Step 3: Create OpenClaw Native Tools
├── Invoke createOpenClawTools() for all native tools
├── Pass configuration, allowlists, context
└── Result: [browser, canvas, nodes, cron, message, gateway,
             agents_list, sessions_*, subagents, session_status,
             memory_search, memory_get, web_search, web_fetch, image]

Step 4: Apply Permissions
├── Apply owner-only tool filtering
├── Apply tool policy pipeline (multi-step)
├── Filter by explicit allowlist
└── Result: filtered tool set

Step 5: Apply Hooks and Normalization
├── Normalize parameter schemas for provider (Gemini, OpenAI, Anthropic)
├── Wrap with before-tool-call hooks (loop detection, plugins)
├── Wrap with abort signal support
└── Result: final tool set ready for agent
```

---

## 3. Filesystem Tools Detail

### Read Tool

**Adaptive paging** based on context window:

```typescript
const DEFAULT_READ_PAGE_MAX_BYTES = 50 * 1024;        // 50KB default page
const MAX_ADAPTIVE_READ_MAX_BYTES = 512 * 1024;       // 512KB max page
const ADAPTIVE_READ_CONTEXT_SHARE = 0.2;               // 20% of context window
const CHARS_PER_TOKEN_ESTIMATE = 4;
const MAX_ADAPTIVE_READ_PAGES = 8;
```

**Read variants:**

| Variant | When Used | Implementation |
|---------|-----------|----------------|
| Standard Read | pi-coding-agent default | Direct file read |
| OpenClaw Read | Wrapped with sanitization | Image sanitization + adaptive paging |
| Sandboxed Read | Inside sandbox | Uses FsBridge across container boundary |

**Result structure:**
```typescript
type ReadResult = {
  content: TextBlock[];          // File content
  details: {
    truncation: {
      truncated: boolean;
      outputLines: number;
      firstLineExceedsLimit: boolean;
    }
  };
  // If truncated: continuation notice with offset instructions
};
```

**Image sanitization:**
- Resolves limits from config
- Sanitizes oversized images in results
- Prevents excessive token usage from large images

### Write Tool

- Creates or overwrites files
- **Disabled** in sandbox read-only mode
- Wrapped with workspace root guard (if `workspaceOnly` enabled)
- Parameter normalization for provider compatibility

### Edit Tool

- Edits specific sections of files (find-and-replace style)
- **Disabled** in sandbox read-only mode
- LSP integration for diagnostics after edit
- Wrapped with workspace root guard

### Apply Patch Tool

- OpenAI-specific (uses their unified diff format)
- Controlled by `applyPatchConfig.enabled` and `applyPatchConfig.allowModels`
- Workspace-contained by default
- Model allowlist: e.g., `["gpt-5.2", "openai/gpt-5.2"]`

### Path Resolution

```
Non-sandboxed:
  resolveWorkspaceRoot(workspaceDir) → absolute path
  If workspaceOnly: wrapToolWorkspaceRootGuard(tool, root)

Sandboxed:
  sandbox.workspaceDir → container-local path
  All paths validated through assertSandboxPath():
    - Prevents directory traversal (..)
    - Validates symlink chains stay within root
    - Rejects absolute paths outside root
```

---

## 4. Exec Tool (Shell Execution)

### Schema

```typescript
type ExecParams = {
  command: string;          // Required: shell command to execute
  workdir?: string;         // Working directory
  env?: Record<string, string>;  // Environment variables
  yieldMs?: number;         // Yield after N ms (for background)
  background?: boolean;     // Immediate yield
  timeout?: number;         // Timeout in seconds
  pty?: boolean;            // TTY mode (non-sandbox only)
  elevated?: boolean;       // Elevated permissions
  host?: string;            // Execution host
  security?: string;        // Security mode override
  ask?: string;             // Approval mode override
  node?: string;            // Node host target
};
```

### Execution Hosts

```typescript
type ExecHost = "sandbox" | "gateway" | "node";
```

| Host | Description | Default |
|------|-------------|---------|
| `sandbox` | Isolated Docker container | Yes (when sandbox enabled) |
| `gateway` | Host machine with approval system | Fallback |
| `node` | Remote node host | Explicit only |

### Security Modes

```typescript
type ExecSecurity = "deny" | "allowlist" | "full";
type ExecAsk = "off" | "on-miss" | "always";
```

| Security | Behavior |
|----------|----------|
| `deny` | All commands blocked |
| `allowlist` | Only pre-approved commands allowed |
| `full` | All commands allowed |

| Ask Mode | Behavior |
|----------|----------|
| `off` | Never ask for approval |
| `on-miss` | Ask when command not in allowlist |
| `always` | Always ask for approval |

**Security resolution:** `minSecurity(configured, requested)` = most restrictive. `maxAsk(configured, requested)` = most permissive (most asking).

### Execution Flow

```
1. Validate parameters (command required)
2. Resolve background/yield behavior
3. Check elevated permissions (if requested)
4. Determine execution host (sandbox/gateway/node)
5. Resolve security mode (min of configured and requested)
6. Resolve environment variables
7. Apply PATH prepending (from config)
8. Preflight validation:
   - validateScriptFileForShellBleed()
   - Detects shell syntax in Python/JS files
9. Execute via runExecProcess()
10. Handle background/yield:
    - background=true → immediate yield
    - yieldMs set → yield after N ms
    - Stored in process registry
11. Return result
```

### Output Handling

```typescript
const DEFAULT_MAX_OUTPUT = 100_000;         // 100K chars for completed runs
const DEFAULT_PENDING_MAX_OUTPUT = 10_000;  // 10K chars for background runs
```

### Timeout

- Default: 1800 seconds (30 minutes)
- Minimum: 10 seconds
- Configurable via params.timeout or config

### Approval System

```
File: ~/.openclaw/exec-approvals.json    (allowlist storage)
Socket: ~/.openclaw/exec-approvals.sock  (approval server)
Timeout: 120,000ms (2 minutes)

Flow:
1. Command submitted
2. Check allowlist → if match, execute
3. If security == "allowlist" and ask == "on-miss":
   - Send approval request via socket
   - Wait for user response (up to 2 min)
   - If approved: add to allowlist, execute
   - If denied: return error
```

### Process Tool (Background Management)

For monitoring background exec processes:

```typescript
type ProcessParams = {
  action: "status" | "input" | "signal" | "poll";
  processId: string;
  input?: string;       // For stdin
  signal?: string;      // For signals (SIGTERM, etc.)
};
```

---

## 5. Tool Permission System

### Tool Profiles

```typescript
type ToolProfileId = "minimal" | "coding" | "messaging" | "full";
```

| Profile | Tools | Use Case |
|---------|-------|----------|
| `minimal` | session_status only | Least privileged |
| `coding` | fs, runtime, sessions, memory, image | Development |
| `messaging` | message, sessions (list/history/send), status | Communication |
| `full` | All tools | Full access |

### Tool Groups

```typescript
const TOOL_GROUPS = {
  "group:memory":     ["memory_search", "memory_get"],
  "group:web":        ["web_search", "web_fetch"],
  "group:fs":         ["read", "write", "edit", "apply_patch"],
  "group:runtime":    ["exec", "process"],
  "group:sessions":   ["sessions_list", "sessions_history", "sessions_send",
                        "sessions_spawn", "subagents", "session_status"],
  "group:ui":         ["browser", "canvas"],
  "group:automation": ["cron", "gateway"],
  "group:messaging":  ["message"],
  "group:nodes":      ["nodes"],
  "group:openclaw":   [/* all 18+ core tools */],
};
```

### Policy Pipeline

Tools pass through a multi-step policy pipeline in order:

```
Step 1: tools.profile                    (global profile)
Step 2: tools.byProvider.profile         (provider-specific profile)
Step 3: tools.allow                      (global allowlist)
Step 4: tools.byProvider.allow           (provider-specific allowlist)
Step 5: agents.{agentId}.tools.allow     (agent-specific allowlist)
Step 6: agents.{agentId}.tools.byProvider.allow  (agent+provider)
Step 7: group tools.allow                (group-specific allowlist)
Step 8: sandbox tools.allow              (sandbox-specific)
Step 9: subagent depth-based tools.allow (depth restrictions)
```

Each step can add or remove tools. The pipeline function:

```typescript
function applyToolPolicyPipeline(params: {
  tools: AgentTool[];
  toolMeta: (tool) => { pluginId: string } | undefined;
  warn: (message: string) => void;
  steps: ToolPolicyPipelineStep[];
}): AgentTool[]
```

### Policy Configuration

```typescript
type ToolPolicyConfig = {
  allow?: string[];      // Explicit allowlist (replaces default)
  alsoAllow?: string[];  // Additional allowed tools (appends)
  deny?: string[];       // Explicit denylist
  profile?: ToolProfileId;  // Profile preset
};
```

### Owner-Only Tools

```typescript
const OWNER_ONLY_TOOLS = ["whatsapp_login", "cron", "gateway"];

function applyOwnerOnlyToolPolicy(tools, senderIsOwner) {
  if (!senderIsOwner) {
    return tools.filter(t => !OWNER_ONLY_TOOLS.has(t.name));
  }
  return tools;
}
```

---

## 6. Tool Schema Normalization

Different providers require different schema formats:

```typescript
function normalizeToolParameters(tool, options?: { modelProvider?: string }): AgentTool
```

| Provider | Normalization |
|----------|--------------|
| **Anthropic** | Full JSON Schema draft 2020-12 compliance |
| **OpenAI** | Adds `type: "object"` at top-level |
| **Gemini** | Cleans constraint keywords, flattens anyOf/oneOf unions |

**Flattening logic** (Gemini):
- Merges `anyOf`/`oneOf` schemas into single object schema
- Preserves useful enums (e.g., `action` field)
- Keeps required fields from all variants

---

## 7. Tool Loop Detection

### Before-Tool-Call Hook

```typescript
async function runBeforeToolCallHook(args: {
  toolName: string;
  params: unknown;
  toolCallId?: string;
  ctx?: { agentId?, sessionKey?, loopDetection? };
}): Promise<{ blocked: boolean; params: unknown }>
```

### Loop Detection Configuration

```typescript
type ToolLoopDetectionConfig = {
  enabled?: boolean;
  historySize?: number;              // default: 30
  warningThreshold?: number;          // default: 10
  criticalThreshold?: number;         // default: 20
  globalCircuitBreakerThreshold?: number;  // default: 30
  detectors?: {
    genericRepeat?: boolean;          // Same call repeated
    knownPollNoProgress?: boolean;    // Polling without progress
    pingPong?: boolean;               // Alternating patterns
  };
};
```

### Detection Patterns

1. **Generic Repeat:** Same tool with same parameters called repeatedly
2. **Known Poll No-Progress:** Polling commands (e.g., process status) returning same result
3. **Ping-Pong:** Alternating between two tool calls without progress

### Action on Detection

- **Warning threshold (10):** Inject warning message to agent
- **Critical threshold (20):** Block tool call, return error
- **Circuit breaker (30):** Hard stop, prevent all further tool calls

---

## 8. Sandbox System

### Sandbox Context

```typescript
type SandboxContext = {
  enabled: boolean;
  containerName: string;
  workspaceDir: string;              // Host path
  containerWorkdir: string;          // Container path
  docker: { env: Record<string, string> };
  workspaceAccess: "ro" | "rw";     // Read-only or read-write
  fsBridge: SandboxFsBridge;         // Cross-container file I/O
  browserAllowHostControl: boolean;
  browser?: { bridgeUrl: string };
  tools?: SandboxToolPolicy;
};
```

### Filesystem Bridge

Bridges host and container filesystems for read/write/edit tools:

```typescript
interface SandboxFsBridge {
  readFile(path: string): Promise<Buffer>;
  writeFile(path: string, content: Buffer): Promise<void>;
  stat(path: string): Promise<FileStat>;
  readdir(path: string): Promise<string[]>;
}
```

### Path Validation

```typescript
async function assertSandboxPath(params: {
  filePath: string;
  cwd: string;
  root: string;
  allowFinalSymlink?: boolean;
}): Promise<{ resolved: string; relative: string }>
```

Prevents:
- Directory escape via `..` sequences
- Symlink escape outside sandbox root
- Absolute paths outside sandbox root

### Sandbox Impact on Tools

| Tool | Sandbox Behavior |
|------|-----------------|
| `read` | Uses FsBridge (host paths) |
| `write` | Disabled if `workspaceAccess == "ro"` |
| `edit` | Disabled if `workspaceAccess == "ro"` |
| `exec` | Runs inside container (container paths) |
| `browser` | Uses sandbox browser bridge |

---

## 9. Tool Result Formatting

### Result Structure

```typescript
type AgentToolResult<TDetails> = {
  content: ContentBlock[];  // Text blocks, image blocks, etc.
  details?: TDetails;       // Structured metadata
};
```

### Image Sanitization on Results

```typescript
function sanitizeToolResultImages(
  result: AgentToolResult,
  limits?: ImageSanitizationLimits,
): AgentToolResult
```

Applied to read tool results to prevent oversized images from consuming tokens.

### Output Truncation

- Read tool: configurable page size (default 50KB, max 512KB)
- Exec tool: 100K chars (completed), 10K chars (background)
- All tools: content blocks may include truncation notices

---

## 10. Plugin Tools

### Plugin Tool Loading

```typescript
function resolvePluginTools(params: {
  context: OpenClawPluginToolContext;
  existingToolNames?: Set<string>;
  toolAllowlist?: string[];
}): AgentTool[]
```

Dynamic tools loaded from plugins based on:
- Plugin configuration
- Tool allowlist settings
- Optional/required flags
- Naming conflict resolution (existing tools take precedence)

---

## 11. Configuration Reference

### Global Tool Configuration

```typescript
type ToolsConfig = {
  profile?: ToolProfileId;
  allow?: string[];
  alsoAllow?: string[];
  deny?: string[];
  byProvider?: Record<string, ToolPolicyConfig>;
};
```

### Exec Tool Configuration

```typescript
type ExecToolConfig = {
  host?: "sandbox" | "gateway" | "node";
  security?: "deny" | "allowlist" | "full";
  ask?: "off" | "on-miss" | "always";
  pathPrepend?: string[];           // Prepend to PATH
  safeBins?: string[];              // Pre-approved binaries
  backgroundMs?: number;            // Default yield time
  timeoutSec?: number;              // Default timeout
  applyPatch?: {
    enabled?: boolean;
    workspaceOnly?: boolean;
    allowModels?: string[];
  };
};
```

### Filesystem Tool Configuration

```typescript
type FsToolsConfig = {
  workspaceOnly?: boolean;  // Restrict to workspace directory
};
```

### Agent-Specific Tool Configuration

```typescript
type AgentToolsConfig = {
  profile?: ToolProfileId;
  allow?: string[];
  alsoAllow?: string[];
  deny?: string[];
  byProvider?: Record<string, ToolPolicyConfig>;
  elevated?: { enabled?: boolean; allowFrom?: AgentElevatedAllowFromConfig };
  exec?: ExecToolConfig;
  fs?: FsToolsConfig;
};
```
