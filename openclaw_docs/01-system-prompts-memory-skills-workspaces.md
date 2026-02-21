# System Prompts, Memory, Skills & Workspaces

## 1. System Prompt Construction

### Overview

The system prompt is the agent's complete operating manual, dynamically assembled at runtime. The core function `buildAgentSystemPrompt()` lives in `src/agents/system-prompt.ts` (679 lines) and takes 24+ parameters to produce a 20-150KB prompt.

### Prompt Modes

Three prompt modes control verbosity:

| Mode | Usage | Size |
|------|-------|------|
| `"full"` | Main agent (default) | ~60+ KB, all sections |
| `"minimal"` | Sub-agents | Reduced: only Tooling, Workspace, Runtime |
| `"none"` | Bare minimum | Just identity line |

### Function Signature

```typescript
export function buildAgentSystemPrompt(params: {
  workspaceDir: string;
  defaultThinkLevel?: ThinkLevel;
  reasoningLevel?: ReasoningLevel;
  extraSystemPrompt?: string;
  ownerNumbers?: string[];
  reasoningTagHint?: boolean;
  toolNames?: string[];
  toolSummaries?: Record<string, string>;
  modelAliasLines?: string[];
  userTimezone?: string;
  userTime?: string;
  userTimeFormat?: ResolvedTimeFormat;
  contextFiles?: EmbeddedContextFile[];
  skillsPrompt?: string;
  heartbeatPrompt?: string;
  docsPath?: string;
  workspaceNotes?: string[];
  ttsHint?: string;
  promptMode?: PromptMode;  // "full" | "minimal" | "none"
  runtimeInfo?: { agentId, host, os, arch, node, model, ... };
  messageToolHints?: string[];
  sandboxInfo?: { enabled, workspaceDir, containerWorkspaceDir, ... };
  reactionGuidance?: { level: "minimal" | "extensive", channel: string };
  memoryCitationsMode?: MemoryCitationsMode;
}): string
```

### Prompt Sections (in order)

The system prompt is assembled from these conditional sections:

1. **Identity Line**
   ```
   You are a personal assistant running inside OpenClaw.
   ```

2. **Tooling Section** - Lists all available tools with descriptions
   - Tool availability determined by `params.toolNames`
   - Tool descriptions from `params.toolSummaries`
   - Tools listed in predefined order: read, write, edit, apply_patch, grep, find, ls, exec, process, web_search, web_fetch, browser, canvas, nodes, cron, message, gateway, agents_list, sessions_list, sessions_history, sessions_send, subagents, session_status, image

3. **Tool Call Style** - Guidance on narration vs silent execution

4. **Safety Section** - Hardcoded safety principles

5. **CLI Quick Reference** - Standard commands (gateway, help, etc.)

6. **Skills Section** (conditional, full mode only)
   ```
   ## Skills (mandatory)
   Before replying: scan <available_skills> <description> entries.
   - If exactly one skill clearly applies: read its SKILL.md at <location> with `read`, then follow it.
   - If multiple could apply: choose the most specific one, then read/follow it.
   - If none clearly apply: do not read any SKILL.md.
   Constraints: never read more than one skill up front; only read after selecting.
   <skills listing>
   ```

7. **Memory Recall Section** (conditional, if memory_search/memory_get tools available)
   - Instructions about MEMORY.md and memory/*.md
   - Citation guidance based on `memoryCitationsMode` ("on"/"off"/"auto")

8. **Self-Update Section** (conditional, full mode with gateway tool)
   - Restricts config.apply and update.run to explicit user requests

9. **Model Aliases Section** (conditional, if modelAliasLines provided)

10. **Workspace Section**
    ```
    ## Workspace
    Working directory: <workspaceDir>
    <workspace guidance for file operations vs exec commands>
    <workspace notes>
    ```
    - For sandboxed: different path guidance (host vs container paths)

11. **Documentation Section** (conditional) - Docs path, links

12. **Sandbox Section** (conditional, if sandbox enabled) - Container info, browser access

13. **Authorized Senders Section** (conditional) - Owner phone numbers

14. **Current Date & Time Section** (conditional, if userTimezone provided)

15. **Workspace Files Section** - Notes about injected bootstrap files

16. **Reply Tags Section** (conditional) - `[[reply_to_current]]` syntax for native replies

17. **Messaging Section** (conditional) - Session routing, cross-session messaging, sub-agent orchestration, inline buttons support

18. **Voice (TTS) Section** (conditional, if ttsHint provided)

19. **Project Context Section** (conditional)
    - Each bootstrap file embedded with `## <filepath>` header
    - This is where SOUL.md, AGENTS.md, MEMORY.md content appears

20. **Silent Replies Section** (conditional, not minimal mode)
    - SILENT_REPLY_TOKEN guidance

21. **Heartbeats Section** (conditional)
    - HEARTBEAT_OK response pattern

22. **Reactions Section** (conditional, if reactionGuidance provided)
    - "minimal" vs "extensive" reaction frequency guidance

23. **Reasoning Format Section** (conditional, if reasoningTagHint is true)
    - Requires `<think>...</think>` and `<final>...</final>` wrapper format

24. **Runtime Section** (always last)
    - Agent ID, host, repo root, OS, arch, Node version
    - Model provider and ID
    - Shell type, channel, capabilities
    - Reasoning level toggle guidance

### System Prompt Injection Point

The prompt is injected in `src/agents/pi-embedded-runner/run/attempt.ts`:

```typescript
const appendPrompt = buildEmbeddedSystemPrompt({
  workspaceDir: effectiveWorkspace,
  defaultThinkLevel: params.thinkLevel,
  reasoningLevel: params.reasoningLevel ?? "off",
  extraSystemPrompt: params.extraSystemPrompt,
  ownerNumbers: params.ownerNumbers,
  reasoningTagHint,
  heartbeatPrompt: isDefaultAgent
    ? resolveHeartbeatPrompt(params.config?.agents?.defaults?.heartbeat?.prompt)
    : undefined,
  skillsPrompt,
  docsPath: docsPath ?? undefined,
  ttsHint,
  workspaceNotes,
  reactionGuidance,
  promptMode,  // "full" for main agent, "minimal" for subagents
  runtimeInfo,
  messageToolHints,
  sandboxInfo,
  tools,
  modelAliasLines: buildModelAliasLines(params.config),
  userTimezone,
  userTime,
  userTimeFormat,
  contextFiles,  // Bootstrap files injected here
  memoryCitationsMode: params.config?.memory?.citations,
});
```

### Context Information Gathering

Before building the prompt, the system gathers runtime context (`buildSystemPromptParams`):

1. **Machine info:** hostname, OS, arch, Node version
2. **Model info:** provider name, model ID
3. **Shell type** (bash/zsh/etc.)
4. **Channel capabilities** (what the current channel supports)
5. **Git repository root** (if available)
6. **Timezone & time** from config

---

## 2. Workspace System

### Workspace Directory Resolution

Workspaces are per-agent directories. Resolution order in `resolveAgentWorkspaceDir()`:

```typescript
function resolveAgentWorkspaceDir(cfg: OpenClawConfig, agentId: string) {
  // 1. Agent-specific workspace from config
  const configured = resolveAgentConfig(cfg, id)?.workspace?.trim();
  if (configured) return resolveUserPath(configured);

  // 2. For default agent, use global defaults
  const defaultAgentId = resolveDefaultAgentId(cfg);
  if (id === defaultAgentId) {
    const fallback = cfg.agents?.defaults?.workspace?.trim();
    if (fallback) return resolveUserPath(fallback);
    return resolveDefaultAgentWorkspaceDir(process.env);
  }

  // 3. For non-default agents, use state dir with agent-id suffix
  const stateDir = resolveStateDir(process.env);
  return path.join(stateDir, `workspace-${id}`);
}
```

Default workspace: `~/.openclaw/workspace` (or `~/.openclaw/workspace-{profile}` if OPENCLAW_PROFILE is set).

### Bootstrap Files

These files are discovered from the workspace root:

| File | Purpose |
|------|---------|
| `AGENTS.md` | Agent definitions and configurations |
| `SOUL.md` | Personality, tone, communication style |
| `TOOLS.md` | User-defined tool guides and instructions |
| `IDENTITY.md` | User identity information |
| `USER.md` | User preferences |
| `MEMORY.md` | Semantic memory (primary) |
| `memory.md` | Semantic memory (alternative) |
| `HEARTBEAT.md` | Heartbeat response pattern |
| `BOOTSTRAP.md` | Git/workspace notes |
| `BOOT.md` | Gateway startup script (run by boot-md hook) |

### Bootstrap File Loading

`loadWorkspaceBootstrapFiles()` in `src/agents/workspace.ts`:

1. Attempts to read all bootstrap files from workspace
2. Caches content with mtime-based invalidation
3. Handles symlinks and missing files gracefully
4. Returns `WorkspaceBootstrapFile[]`:
   ```typescript
   type WorkspaceBootstrapFile = {
     name: WorkspaceBootstrapFileName;
     path: string;
     content?: string;
     missing: boolean;
   };
   ```

### Session-Based Filtering

Different session types get different bootstrap files:

```typescript
const MINIMAL_BOOTSTRAP_ALLOWLIST = new Set([
  "AGENTS.md",
  "TOOLS.md"
]);

// Main agents: get ALL bootstrap files
// Sub-agents and cron sessions: only AGENTS.md + TOOLS.md
```

### Character Limits and Truncation

- **Per-file max:** 20,000 characters (configurable via `bootstrapMaxChars`)
- **Total max:** 150,000 characters (configurable via `bootstrapTotalMaxChars`)
- **Truncation strategy:** Keep 70% head, 20% tail, 10% for truncation marker

### Workspace Initialization

When creating a new workspace (`ensureAgentWorkspace()`):

1. Creates directory if missing
2. Seeds bootstrap files from templates (stripped of YAML frontmatter)
3. Tracks onboarding state in `.openclaw/workspace-state.json`
4. Initializes git repo if available

### Multi-Workspace Support

Each agent gets its own workspace directory:

```typescript
function listAgentWorkspaceDirs(cfg: OpenClawConfig): string[] {
  const dirs = new Set<string>();
  // Add workspace for each configured agent
  for (const entry of cfg.agents?.list ?? []) {
    dirs.add(resolveAgentWorkspaceDir(cfg, entry.id));
  }
  // Always include default agent workspace
  dirs.add(resolveAgentWorkspaceDir(cfg, resolveDefaultAgentId(cfg)));
  return [...dirs];
}
```

---

## 3. Memory System

### Overview

The memory system provides semantic search over workspace memory files and session transcripts, backed by SQLite with vector embeddings.

### Memory File Discovery

In `src/agents/workspace.ts`, the system looks for:

```typescript
const candidates = ["MEMORY.md", "memory.md"];
```

Both are included if they point to different files (symlink-aware deduplication).

### Memory Search Configuration

Full configuration schema (`ResolvedMemorySearchConfig`):

```typescript
type ResolvedMemorySearchConfig = {
  enabled: boolean;
  sources: Array<"memory" | "sessions">;
  extraPaths: string[];
  provider: "openai" | "local" | "gemini" | "voyage" | "auto";
  remote?: { baseUrl?, apiKey?, headers?, batch?: {...} };
  experimental: { sessionMemory: boolean };
  fallback: "openai" | "gemini" | "local" | "voyage" | "none";
  model: string;
  local: { modelPath?, modelCacheDir? };
  store: {
    driver: "sqlite";
    path: string;         // ~/.openclaw/state/memory/{agentId}.sqlite
    vector: { enabled: boolean; extensionPath? };
  };
  chunking: { tokens: number; overlap: number };
  sync: {
    onSessionStart: boolean;
    onSearch: boolean;
    watch: boolean;
    watchDebounceMs: number;
    intervalMinutes: number;
    sessions: { deltaBytes: number; deltaMessages: number };
  };
  query: {
    maxResults: number;   // default: 6
    minScore: number;     // default: 0.35
    hybrid: {
      enabled: boolean;
      vectorWeight: number;   // default: 0.7
      textWeight: number;     // default: 0.3
      candidateMultiplier: number;
      mmr: { enabled: boolean; lambda: number };
      temporalDecay: { enabled: boolean; halfLifeDays: number };
    };
  };
  cache: { enabled: boolean; maxEntries?: number };
};
```

### Memory Tools

Two tools are created when memory is enabled:

#### memory_search
```typescript
{
  name: "memory_search",
  label: "Memory Search",
  description: "Mandatory recall step: semantically search MEMORY.md + memory/*.md (and optional session transcripts) before answering questions about prior work...",
  parameters: {
    query: string;          // required
    maxResults?: number;
    minScore?: number;
  }
}
```

#### memory_get
```typescript
{
  name: "memory_get",
  label: "Memory Get",
  description: "Safe snippet read from MEMORY.md or memory/*.md with optional from/lines...",
  parameters: {
    path: string;           // required
    from?: number;          // start line
    lines?: number;         // number of lines
  }
}
```

### Memory Storage Backend

SQLite database with optional vector extensions:

```
Storage path: ~/.openclaw/state/memory/{agentId}.sqlite

Tables:
  meta          { key, value }                        -- Metadata key-value store
  files         { path, source, hash, mtime, size }   -- File index
  chunks        { id, path, source, start_line,       -- Content chunks
                  end_line, hash, model, text,
                  embedding, updated_at }
  embedding_cache { provider, model, provider_key,    -- Embedding cache
                    hash, embedding, dims, updated_at }
  fts5_table    (virtual FTS5 table)                  -- Full-text search
```

### Memory Citations

Configurable via `config.memory.citations`:

| Mode | Behavior |
|------|----------|
| `"on"` | Always include `Source: <path#line>` citations |
| `"off"` | Never mention file paths or line numbers unless user asks |
| `"auto"` | Contextual decision (default) |

### Memory Injection into System Prompt

Memory content is injected in two ways:

1. **Bootstrap files:** MEMORY.md content included in "Project Context" section (subject to character limits)
2. **Memory tools:** Agent can query the vector store at runtime via memory_search/memory_get

---

## 4. Skills System

### Overview

Skills are markdown files with structured frontmatter that define agent capabilities. They're discovered from multiple locations, filtered by policy, and injected into the system prompt.

### Skill File Structure

Each skill is a directory containing a `SKILL.md` file:

```
skills/
└── github/
    └── SKILL.md      # Frontmatter + instructions
```

### Skill Discovery Sources (in order)

1. **Workspace skills:** `{workspace}/skills/**/**/SKILL.md`
2. **User bundled skills:** `~/.bun/install/global/node_modules/.bin/../{package}/skills/**/**/SKILL.md`
3. **Plugin skills:** From installed plugins
4. **Managed skills:** Via skills manager if configured
5. **Bundled openclaw skills:** Built-in skills directory

### Discovery Limits

```typescript
const DEFAULT_MAX_CANDIDATES_PER_ROOT = 300;
const DEFAULT_MAX_SKILLS_LOADED_PER_SOURCE = 200;
const DEFAULT_MAX_SKILLS_IN_PROMPT = 150;
const DEFAULT_MAX_SKILLS_PROMPT_CHARS = 30_000;    // 30KB
const DEFAULT_MAX_SKILL_FILE_BYTES = 256_000;      // 256KB per SKILL.md
```

### Skill Frontmatter

```yaml
---
name: github
description: "Interact with GitHub..."
metadata:
  openclaw:
    primaryEnv: node
    requires:
      env: ["GITHUB_TOKEN"]
    invocation:
      disableModelInvocation: false
      allowRemote: true
---
```

Parsed type:
```typescript
type ParsedSkillFrontmatter = {
  raw?: Record<string, unknown>;
  metadata?: {
    primaryEnv?: string;
    requires?: { env?: string[] };
  };
  invocation?: {
    disableModelInvocation?: boolean;
    allowRemote?: boolean;
  };
};
```

### Skill Path Optimization

Skill paths are compacted to reduce token usage:
- Replaces home directory with `~`
- Saves ~5-6 tokens per skill x 150 skills = 400-600 tokens

### Skills in System Prompt

The skills section instructs the agent:

```
## Skills (mandatory)
Before replying: scan <available_skills> <description> entries.
- If exactly one skill clearly applies: read its SKILL.md at <location> with `read`, then follow it.
- If multiple could apply: choose the most specific one, then read/follow it.
- If none clearly apply: do not read any SKILL.md.
Constraints: never read more than one skill up front; only read after selecting.

<available_skills>
  <skill location="~/skills/github/SKILL.md">
    <description>Interact with GitHub repositories...</description>
  </skill>
  ...
</available_skills>
```

### Skill Invocation Flow

```
1. System prompt includes skills listing
2. Agent scans <available_skills> descriptions
3. Decision:
   - One skill matches → read its SKILL.md with read tool
   - Multiple match → choose most specific, read it
   - None match → skip
4. Agent reads SKILL.md content
5. Agent follows skill instructions
```

---

## 5. Complete Data Flow: Prompt Assembly

```
Agent Run Initiated
    ↓
Resolve Agent Workspace
├── From config.agents[id].workspace
├── From config.agents.defaults.workspace
└── Default: ~/.openclaw/workspace
    ↓
Load Bootstrap Context
├── loadWorkspaceBootstrapFiles(workspace)
│   ├── Discover AGENTS.md, SOUL.md, TOOLS.md, etc.
│   ├── Load memory files (MEMORY.md, memory.md)
│   └── Cache with mtime invalidation
├── filterBootstrapFilesForSession(files, sessionKey)
│   └── Reduce to AGENTS.md + TOOLS.md for sub-agents
└── buildBootstrapContextFiles(files, limits)
    └── Apply per-file (20KB) and total (150KB) limits
    ↓
Build Skills Prompt
├── loadWorkspaceSkillEntries(workspace)
│   ├── Discover SKILL.md from all sources
│   └── Parse frontmatter for metadata
├── Apply limits: 150 skills, 30KB
└── buildWorkspaceSkillsPrompt() → skillsPrompt
    ↓
Build Memory Configuration
└── resolveMemorySearchConfig(config, agentId)
    ├── Store path: ~/.openclaw/state/memory/{agentId}.sqlite
    ├── Query: 6 results, 0.35 min score
    └── Hybrid: 70% vector, 30% text
    ↓
Gather Runtime Info
├── hostname, OS, arch, Node version
├── model provider + ID
├── shell type, channel capabilities
└── git repo root, timezone
    ↓
Build System Prompt
└── buildAgentSystemPrompt({
      workspaceDir, skillsPrompt, contextFiles,
      runtimeInfo, memoryCitationsMode, ...24+ params
    })
    ├── 24 conditional sections assembled
    ├── SOUL.md guides personality (in Project Context)
    ├── Bootstrap files embedded in Project Context
    └── Returns ~20-150KB prompt text
    ↓
Inject into Session
└── Set as active system prompt for agent conversation
```
