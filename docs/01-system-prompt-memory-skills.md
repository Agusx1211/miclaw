# System Prompt, Memory & Skills

## 1. System Prompt Construction

The system prompt is the agent's operating manual, assembled at runtime from workspace files and runtime context.

### Prompt Modes

| Mode | Usage | Content |
|------|-------|---------|
| `full` | Singleton agent | All sections |
| `minimal` | Sub-agents | Tooling + workspace + runtime only |

### Prompt Sections (in order)

1. **Identity** -- "You are a personal assistant running inside Miclaw."
2. **Tooling** -- Available tools with descriptions, listed in fixed order.
3. **Tool Call Style** -- When to narrate vs execute silently.
4. **Safety** -- Hardcoded safety principles.
5. **Skills** -- Discovered skills with scan-then-read instructions (full mode only).
6. **Memory Recall** -- Instructions for using `memory_search` / `memory_get` (if enabled).
7. **Workspace** -- Working directory, file operation guidance.
8. **Current Date & Time** -- Timezone-aware timestamp.
9. **Workspace Files** -- Injected bootstrap file contents (SOUL.md, AGENTS.md, MEMORY.md, etc.).
10. **Heartbeat** -- If HEARTBEAT.md exists, instructions for responding to health-check messages with `HEARTBEAT_OK` (full mode only).
11. **Runtime** -- OS, arch, model provider, model ID, Go version.

Sub-agents get sections 1, 2, 7, 9 (AGENTS.md only), and 10.

### Inputs

```go
type SystemPromptParams struct {
    WorkspaceDir    string
    ToolNames       []string
    ToolSummaries   map[string]string
    SkillsPrompt    string            // pre-formatted skills block
    UserTimezone    string
    UserTime        string
    ContextFiles    []BootstrapFile   // SOUL.md, MEMORY.md, etc.
    RuntimeInfo     RuntimeInfo       // os, arch, model, etc.
    PromptMode      PromptMode        // "full" or "minimal"
    CitationsMode   string            // "on", "off", "auto"
}
```

The function returns a single string. No side effects.

---

## 2. Workspace System

### Workspace Path

Single workspace at `~/.miclaw/workspace`. No per-agent workspaces (there is only one agent).

### Bootstrap Files

Discovered from workspace root:

| File | Purpose | Sub-agent gets it |
|------|---------|-------------------|
| `SOUL.md` | Personality, tone, communication style | No |
| `AGENTS.md` | Agent instructions, behavioral rules | Yes |
| `IDENTITY.md` | User identity info | No |
| `USER.md` | User preferences | No |
| `MEMORY.md` | Semantic memory (primary) | No |
| `HEARTBEAT.md` | Heartbeat response pattern | No |

### Loading

1. Read each bootstrap file from workspace.
2. Cache content with mtime-based invalidation.
3. Filter by prompt mode (sub-agents only get AGENTS.md).
4. Truncate: per-file max 20,000 chars, total max 150,000 chars.
5. Truncation strategy: keep 70% head, 20% tail, 10% for marker.

---

## 3. Memory System

### Overview

SQLite-backed vector store with hybrid search. Two tools (`memory_search`, `memory_get`) let the agent query MEMORY.md and memory fragment files at runtime.

### Memory File Discovery

The system looks for:
- `{workspace}/MEMORY.md` -- primary memory file
- `{workspace}/memory/*.md` -- memory fragments

### Storage

```
Path: ~/.miclaw/state/memory/agent.sqlite

Tables:
  meta              { key, value }
  files             { path, hash, mtime, size }
  chunks            { id, path, start_line, end_line, hash, text, embedding, updated_at }
  fts               (virtual FTS5 table on chunks.text)
```

### Hybrid Search

Queries combine vector similarity and full-text search:
- **Vector weight:** 0.7 (semantic similarity via embeddings)
- **Text weight:** 0.3 (FTS5 keyword match)
- **Default max results:** 6
- **Default min score:** 0.35

### Embedding Provider

Embeddings are generated using the configured LLM backend. The embedding model is a configuration choice. For local (LM Studio), embeddings can run locally. For cloud (OpenRouter), use their embedding endpoints.

### Memory Sync

Memory files are re-indexed:
- On startup
- On search (if files changed since last sync)
- Periodically (configurable interval)

Change detection uses file hash comparison.

### Citations

Configurable via `config.memory.citations`:

| Mode | Behavior |
|------|----------|
| `on` | Always cite source path and line numbers |
| `off` | Never mention file paths unless user asks |
| `auto` | Contextual decision (default) |

---

## 4. Skills System

### Overview

Skills are markdown files that define agent capabilities for specific tasks. They live in the workspace and are discovered at startup.

### No Import, No Marketplace

Skills must be manually created as directories in `{workspace}/skills/`. There is no `skills install`, no npm packages, no remote skill sources, no managed skills directory.

### Skill Structure

```
workspace/
  skills/
    github/
      SKILL.md
    deploy/
      SKILL.md
```

Each skill is a directory containing exactly one `SKILL.md` file.

### SKILL.md Format

```yaml
---
name: github
description: "Interact with GitHub repositories, PRs, and issues"
---

# GitHub Skill

When the user asks about GitHub operations...

## Available Commands
...
```

The frontmatter requires `name` and `description`. Everything after the frontmatter is the skill's instructions, read by the agent when the skill is selected.

### Discovery

At startup (and on workspace file change):
1. Scan `{workspace}/skills/*/SKILL.md`
2. Parse frontmatter for name and description
3. Build skills prompt block

### Limits

- **Max skills in prompt:** 150
- **Max skills prompt size:** 30KB
- **Max SKILL.md file size:** 256KB

### Injection into System Prompt

```
## Skills (mandatory)
Before replying: scan <available_skills> <description> entries.
- If exactly one skill clearly applies: read its SKILL.md with `read`, then follow it.
- If multiple could apply: choose the most specific one, then read/follow it.
- If none clearly apply: do not read any SKILL.md.
Constraints: never read more than one skill up front; only read after selecting.

<available_skills>
  <skill location="~/.miclaw/workspace/skills/github/SKILL.md">
    <description>Interact with GitHub repositories, PRs, and issues</description>
  </skill>
  ...
</available_skills>
```

### Invocation Flow

```
1. System prompt includes skills listing with descriptions
2. Agent scans descriptions for relevance to current request
3. If match: agent reads SKILL.md using the `read` tool
4. Agent follows the skill's instructions
```

The agent uses the `read` tool to load the skill content on demand. Skills are NOT pre-loaded into the prompt (only their names and descriptions are).

---

## 5. Heartbeat System

### Overview

The heartbeat is a **response pattern**, not a background keepalive. There is no timer, no periodic pinging, no background process.

### How It Works

1. `HEARTBEAT.md` in the workspace defines how the agent should respond to health-check messages.
2. If HEARTBEAT.md exists, a heartbeat section is included in the system prompt instructing the agent to respond with `HEARTBEAT_OK` when it receives a health-check message.
3. External monitoring (cron jobs, webhook callers, Signal messages) can send a health-check message. The agent recognizes it and responds with `HEARTBEAT_OK`.

### Integration with Cron

A typical setup: the agent uses the `cron` tool to schedule a periodic self-check. When the cron fires, it injects a health-check message. The agent responds `HEARTBEAT_OK`. If no response comes, the monitoring system knows the agent is down.

```
Cron fires "heartbeat check"
    |
Injected as user message into agent thread
    |
Agent sees health-check, responds HEARTBEAT_OK
    |
Monitoring system observes response (or absence of it)
```

### HEARTBEAT.md

The file content defines the heartbeat pattern. Example:

```markdown
When you receive a message containing "heartbeat" or "health check",
respond with exactly: HEARTBEAT_OK

Do not add any other text. Do not use tools. Just respond.
```

The agent reads this via the bootstrap file injection in the system prompt.

---

## 6. Prompt Assembly Flow

```
Agent Run
    |
Load Bootstrap Files
+-- Read SOUL.md, AGENTS.md, MEMORY.md, etc.
+-- Cache with mtime invalidation
+-- Filter for prompt mode (full vs minimal)
+-- Apply character limits (20K per file, 150K total)
    |
Build Skills Prompt
+-- Scan workspace/skills/*/SKILL.md
+-- Parse frontmatter for name + description
+-- Apply limits (150 skills, 30KB)
    |
Gather Runtime Info
+-- hostname, OS, arch, Go version
+-- model provider + ID
+-- timezone
    |
Build System Prompt
+-- Assemble sections in order
+-- Inject bootstrap file contents
+-- Return string
    |
Inject into Conversation
+-- Set as system message (messages[0])
```
