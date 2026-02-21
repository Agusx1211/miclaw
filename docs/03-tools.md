# Tools

## 1. Tool Interface

Every tool implements:

```go
type Tool interface {
    Name() string
    Description() string
    Parameters() JSONSchema
    Run(ctx context.Context, call ToolCall) (ToolResult, error)
}

type ToolCall struct {
    ID    string
    Name  string
    Input json.RawMessage
}

type ToolResult struct {
    Content string
    IsError bool
}
```

No labels. No metadata. No hooks wrapping. A tool is a function with a schema.

---

## 2. Complete Tool Inventory

| Tool | Group | Description | Main Agent | Sub-agent |
|------|-------|-------------|-----------|-----------|
| `read` | fs | Read file contents | Yes | Yes |
| `write` | fs | Create or overwrite files | Yes | No |
| `edit` | fs | Edit sections of files | Yes | No |
| `apply_patch` | fs | Apply unified diffs | Yes | No |
| `grep` | fs | Search file contents by pattern | Yes | Yes |
| `glob` | fs | Find files by glob pattern | Yes | Yes |
| `ls` | fs | List directory contents | Yes | Yes |
| `exec` | runtime | Execute shell commands | Yes | No |
| `process` | runtime | Monitor background processes | Yes | No |
| `cron` | automation | Schedule recurring tasks | Yes | No |
| `message` | messaging | Send cross-channel messages | Yes | No |
| `agents_list` | introspection | List agent info | Yes | No |
| `sessions_list` | sessions | List sessions | Yes | No |
| `sessions_history` | sessions | Get session message history | Yes | No |
| `sessions_send` | sessions | Send message to a session | Yes | No |
| `sessions_spawn` | sessions | Spawn a sub-agent | Yes | No |
| `subagents` | sessions | List active sub-agents | Yes | No |
| `sessions_status` | sessions | Current session status | Yes | No |
| `memory_search` | memory | Semantic memory search | Yes | Yes |
| `memory_get` | memory | Read memory file snippets | Yes | Yes |

**Sub-agent tool set:** `read`, `grep`, `glob`, `ls`, `memory_search`, `memory_get`. Six tools. All read-only.

---

## 3. Filesystem Tools

### read

Read file contents with pagination for large files.

```go
type ReadParams struct {
    Path   string `json:"path"`            // required
    Offset int    `json:"offset,omitempty"` // start line (0-based)
    Limit  int    `json:"limit,omitempty"`  // max lines
}
```

- Default page size: 50KB
- Max page size: 512KB
- If file exceeds page size: return truncated content with continuation notice
- Returns line-numbered content

### write

Create or overwrite files.

```go
type WriteParams struct {
    Path    string `json:"path"`    // required
    Content string `json:"content"` // required
}
```

- Creates parent directories if needed
- Overwrites existing file entirely

### edit

Edit specific sections of a file using find-and-replace.

```go
type EditParams struct {
    Path      string `json:"path"`       // required
    OldText   string `json:"old_text"`   // required, must be unique in file
    NewText   string `json:"new_text"`   // required
}
```

- `old_text` must appear exactly once in the file (error if not found or ambiguous)
- Preserves rest of file unchanged

### apply_patch

Apply unified diffs for multi-file changes.

```go
type ApplyPatchParams struct {
    Patch string `json:"patch"` // required
}
```

Patch format:
```
*** Begin Patch
*** Add File: path/to/new.go
+package main
+...

*** Update File: path/to/existing.go
@@ context line @@
 unchanged
-old line
+new line

*** Delete File: path/to/remove.go
*** End Patch
```

### grep

Search file contents by regex pattern.

```go
type GrepParams struct {
    Pattern string `json:"pattern"`          // required, regex
    Path    string `json:"path,omitempty"`   // directory to search (default: workspace)
    Include string `json:"include,omitempty"` // glob filter (e.g., "*.go")
}
```

Returns matching lines with file paths and line numbers.

### glob

Find files matching a glob pattern.

```go
type GlobParams struct {
    Pattern string `json:"pattern"` // required (e.g., "**/*.go")
    Path    string `json:"path,omitempty"` // root directory (default: workspace)
}
```

Returns list of matching file paths.

### ls

List directory contents.

```go
type LSParams struct {
    Path string `json:"path"` // required
}
```

Returns entries with type (file/dir) and size.

---

## 4. Runtime Tools

### exec

Execute shell commands.

```go
type ExecParams struct {
    Command    string            `json:"command"`              // required
    Workdir    string            `json:"workdir,omitempty"`
    Env        map[string]string `json:"env,omitempty"`
    Background bool              `json:"background,omitempty"` // yield immediately
    Timeout    int               `json:"timeout,omitempty"`    // seconds
}
```

- Default timeout: 1800 seconds (30 min)
- Minimum timeout: 10 seconds
- Output limit: 100K chars (completed), 10K chars (background)
- Background processes stored in process registry

When running inside the sandbox, `exec` checks if the command matches a configured host command and routes it through SSH automatically. The agent doesn't need to know about SSH — it just calls `exec`. See [08-sandboxing.md](./08-sandboxing.md).

### process

Monitor and control background processes started by `exec`.

```go
type ProcessParams struct {
    Action    string `json:"action"`     // "status", "input", "signal", "poll"
    ProcessID string `json:"process_id"` // required
    Input     string `json:"input,omitempty"`  // for stdin
    Signal    string `json:"signal,omitempty"` // for signals (SIGTERM, etc.)
}
```

---

## 5. Automation Tools

### cron

Schedule recurring tasks.

```go
type CronParams struct {
    Action   string `json:"action"`             // "list", "add", "remove"
    Schedule string `json:"schedule,omitempty"`  // cron expression
    Prompt   string `json:"prompt,omitempty"`    // message to inject
    ID       string `json:"id,omitempty"`        // for remove
}
```

When a cron job fires, it injects its prompt as a user message into the agent thread. The agent wakes up and processes it like any other input.

### message

Send messages to external channels.

```go
type MessageParams struct {
    To      string `json:"to"`      // target (e.g., "signal:+15551234567")
    Content string `json:"content"` // message text
}
```

---

## 6. Session Tools

### agents_list

Returns information about the agent (singular, since there's only one).

### sessions_list

List active sessions.

```go
type SessionsListParams struct {
    Limit int `json:"limit,omitempty"` // default: 20
}
```

### sessions_history

Get message history for a session.

```go
type SessionsHistoryParams struct {
    SessionID string `json:"session_id"` // required
    Limit     int    `json:"limit,omitempty"`
}
```

### sessions_send

Send a message to an existing session.

```go
type SessionsSendParams struct {
    SessionID string `json:"session_id"` // required
    Content   string `json:"content"`    // required
}
```

### sessions_spawn

Spawn a read-only sub-agent for research tasks.

```go
type SessionsSpawnParams struct {
    Prompt string `json:"prompt"` // required
}
```

Creates a child session with read-only tools, runs the sub-agent, returns its text response.

### subagents

List currently active sub-agents.

### sessions_status

Return current session info (ID, message count, token usage, cost).

---

## 7. Memory Tools

### memory_search

Semantic search over MEMORY.md and memory fragments.

```go
type MemorySearchParams struct {
    Query      string  `json:"query"`                  // required
    MaxResults int     `json:"max_results,omitempty"`  // default: 6
    MinScore   float64 `json:"min_score,omitempty"`    // default: 0.35
}
```

Returns ranked results with source path, line range, score, and text snippet.

### memory_get

Read a specific snippet from a memory file.

```go
type MemoryGetParams struct {
    Path  string `json:"path"`            // required
    From  int    `json:"from,omitempty"`  // start line
    Lines int    `json:"lines,omitempty"` // number of lines
}
```

---

## 8. Tool Assembly

No policy pipeline. No permission profiles. No hook wrapping.

### Main Agent

Gets all 20 tools.

### Sub-agent

Gets: `read`, `grep`, `glob`, `ls`, `memory_search`, `memory_get`.

Assembly is a function that returns a slice:

```go
func MainAgentTools(/* deps */) []Tool { ... }
func SubAgentTools(/* deps */) []Tool { ... }
```

### Schema Normalization

Different LLM providers need different JSON Schema formats:

| Provider | Requirement |
|----------|-------------|
| OpenAI-compatible (LM Studio, OpenRouter, Codex) | `type: "object"` at root level |

Since all three backends are OpenAI-compatible, there's one normalization path.
