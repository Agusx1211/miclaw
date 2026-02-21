# Miclaw

A lean Go port of [openclaw](https://github.com/agusx1211/openclaw). One agent. One thread. Minimal surface area.

The original codebase grew to 450k+ lines. Miclaw delivers the same core functionality in ~20k lines of Go.

## Requirements

- Go 1.25+
- An LLM backend: [LM Studio](https://lmstudio.ai/) (local), [OpenRouter](https://openrouter.ai/) (cloud), or [OpenAI Codex](https://platform.openai.com/) (cloud)
- (Optional) [signal-cli](https://github.com/AsamK/signal-cli) for Signal messaging
- (Optional) Docker for sandboxed execution

## Quick Start

```bash
# Build
go build -o miclaw ./cmd/miclaw

# Cross-compile Linux binaries (Ubuntu x64 + arm64)
make cross

# Run first-time setup TUI
./miclaw --setup
```

If `~/.miclaw/config.json` does not exist and you run `./miclaw` in an interactive terminal, Miclaw now automatically launches the setup TUI.

## Configuration

Config lives at `~/.miclaw/config.json` (override with `-config <path>`).

Use the built-in TUI to create or edit config:

```bash
./miclaw --setup
# or
./miclaw --configure
```

The setup TUI supports:
- Provider selection (`lmstudio`, `openrouter`, `codex`)
- Auto-loading provider models with searchable selection
- OpenAI Codex OAuth flow (open auth URL, paste full redirect URL)
- Signal setup and policy configuration
- Docker sandbox setup (network, mounts, host user, SSH key)
- Memory and webhook configuration

### Provider

Pick one backend:

**LM Studio (local)**
```json
{
  "provider": {
    "backend": "lmstudio",
    "model": "your-model-name"
  }
}
```

**OpenRouter (cloud, multi-model)**
```json
{
  "provider": {
    "backend": "openrouter",
    "api_key": "sk-or-...",
    "model": "anthropic/claude-sonnet-4-5"
  }
}
```

**OpenAI Codex (cloud, extended thinking)**
```json
{
  "provider": {
    "backend": "codex",
    "api_key": "sk-...",
    "model": "gpt-5.2",
    "thinking_effort": "medium"
  }
}
```

Provider fields:

| Field | Default | Description |
|-------|---------|-------------|
| `backend` | *(required)* | `lmstudio`, `openrouter`, or `codex` |
| `base_url` | per-backend | API endpoint (auto-set for each backend) |
| `api_key` | | Required for openrouter and codex |
| `model` | *(required)* | Model name or `provider/model` for OpenRouter |
| `max_tokens` | `8192` | Max output tokens |
| `thinking_effort` | | Codex only: `off`, `minimal`, `low`, `medium`, `high`, `xhigh` |

### Signal Integration

Requires `signal-cli` running as an HTTP daemon.

```json
{
  "signal": {
    "enabled": true,
    "account": "+15551234567",
    "http_host": "127.0.0.1",
    "http_port": 8080,
    "dm_policy": "allowlist",
    "group_policy": "disabled",
    "allowlist": ["+15559876543"]
  }
}
```

| Field | Default | Description |
|-------|---------|-------------|
| `enabled` | `false` | Enable Signal integration |
| `account` | *(required)* | E.164 phone number |
| `http_host` | `127.0.0.1` | signal-cli daemon host |
| `http_port` | `8080` | signal-cli daemon port |
| `cli_path` | `signal-cli` | Path to signal-cli binary |
| `auto_start` | `false` | Auto-start signal-cli daemon |
| `dm_policy` | `open` | `open`, `allowlist`, or `disabled` |
| `group_policy` | `disabled` | `open`, `allowlist`, or `disabled` |
| `allowlist` | `[]` | Allowed phone numbers (E.164) |
| `text_chunk_limit` | `4000` | Max chars per outbound message |
| `media_max_mb` | `8` | Max attachment size in MB |

### Webhooks

HTTP endpoints that inject payloads into the agent's conversation.

```json
{
  "webhook": {
    "enabled": true,
    "listen": "127.0.0.1:9090",
    "hooks": [
      {
        "id": "github",
        "path": "/webhook/github",
        "secret": "whsec_abc123",
        "format": "json"
      }
    ]
  }
}
```

| Field | Default | Description |
|-------|---------|-------------|
| `enabled` | `false` | Enable webhook server |
| `listen` | `127.0.0.1:9090` | Listen address |
| `hooks[].id` | *(required)* | Unique hook identifier |
| `hooks[].path` | *(required)* | URL path (must start with `/`) |
| `hooks[].secret` | | HMAC-SHA256 secret (optional) |
| `hooks[].format` | `text` | `text` or `json` |

Webhooks respond `202 Accepted` immediately. A health check is available at `GET /health`.

### Memory

Hybrid vector + full-text search over workspace markdown files.

```json
{
  "memory": {
    "enabled": true,
    "embedding_url": "https://openrouter.ai/api/v1",
    "embedding_api_key": "sk-or-...",
    "embedding_model": "openai/text-embedding-3-small"
  }
}
```

| Field | Default | Description |
|-------|---------|-------------|
| `enabled` | `false` | Enable memory system |
| `embedding_url` | *(required)* | Embedding API endpoint |
| `embedding_api_key` | *(required)* | Embedding API key |
| `embedding_model` | *(required)* | Embedding model name |
| `min_score` | `0.35` | Minimum relevance score |
| `default_results` | `6` | Default number of results |
| `citations` | `auto` | `on`, `off`, or `auto` |

### Sandbox

Run the agent inside a Docker container with controlled network and filesystem access.

```json
{
  "sandbox": {
    "enabled": true,
    "network": "none",
    "mounts": [
      {"host": "/home/user/projects", "container": "/workspace", "mode": "rw"},
      {"host": "/home/user/ref", "container": "/ref", "mode": "ro"}
    ]
  }
}
```

| Field | Default | Description |
|-------|---------|-------------|
| `enabled` | `false` | Enable sandboxing |
| `network` | `none` | `none`, `host`, `bridge`, or a custom network name |
| `mounts` | `[]` | Bind mounts with `host`, `container`, and `mode` (`ro`/`rw`) |
| `ssh_key_path` | | SSH key for host command execution |
| `host_user` | `pipo-runner` | Host user for SSH commands |

### Full Config Reference

```json
{
  "provider": { "backend": "...", "api_key": "...", "model": "...", "max_tokens": 8192 },
  "signal": { "enabled": false, "account": "", "dm_policy": "open", "..." : "..." },
  "webhook": { "enabled": false, "listen": "127.0.0.1:9090", "hooks": [] },
  "memory": { "enabled": false, "embedding_url": "", "..." : "..." },
  "sandbox": { "enabled": false, "network": "none", "mounts": [] },
  "workspace": "~/.miclaw/workspace",
  "state_path": "~/.miclaw/state"
}
```

See [`examples/`](examples/) for complete config files.

## Workspace

The workspace directory contains the files that shape your agent's behavior.

```
~/.miclaw/workspace/
  SOUL.md            # Personality and tone
  AGENTS.md          # Behavioral instructions and rules
  IDENTITY.md        # User identity info
  USER.md            # User preferences and context
  MEMORY.md          # Primary memory file
  HEARTBEAT.md       # Health check response pattern
  skills/            # Agent skills
    <name>/
      SKILL.md       # Skill definition (YAML frontmatter + markdown)
  memory/            # Memory fragment files
    *.md
```

All files are optional. The agent works without any of them — they just make it smarter.

### Skills

Skills are markdown files the agent loads on demand. Create a directory under `skills/` with a `SKILL.md`:

```markdown
---
name: deploy
description: "Deploy applications to production servers"
---

# Deploy Skill

When the user asks to deploy...
```

The agent sees skill names and descriptions in its system prompt and reads the full file when relevant.

## Docker

### Development

```bash
docker compose up
```

With Signal:
```bash
docker compose --profile with-signal up
```

### Production

```bash
cp examples/docker-compose.prod.yml .
# Edit config and volume paths
docker compose -f docker-compose.prod.yml up -d
```

The Dockerfile builds a minimal Alpine image with the miclaw binary and an openssh-client (for sandbox SSH host commands).

## Architecture

```
                +-----------------+
                |   Signal        |
                |   (SSE + RPC)   |
                +--------+--------+
                         |
                         v
+--------------+   +-----+----------+   +--------------+
|  Webhooks    |-->|   Singleton    |<->|   Config     |
|  (HTTP POST) |   |   Agent Loop   |   |   (JSON)     |
+--------------+   +--------+-------+   +--------------+
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
      | + Memory    | | Backend    |
      +-------------+ +------------+
```

**One agent, one thread.** All input channels (Signal, webhooks, cron) feed into a single conversation. The agent processes requests sequentially.

### Tools (20)

| Category | Tools |
|----------|-------|
| Filesystem | `read`, `write`, `edit`, `apply_patch`, `grep`, `glob`, `ls` |
| Runtime | `exec`, `process` |
| Automation | `cron` |
| Messaging | `message` |
| Sessions | `sessions_list`, `sessions_history`, `sessions_send`, `sessions_spawn`, `sessions_status`, `subagents`, `agents_list` |
| Memory | `memory_search`, `memory_get` |

Sub-agents get read-only access to 6 tools: `read`, `grep`, `glob`, `ls`, `memory_search`, `memory_get`.

### Context Compaction

When conversation tokens exceed the threshold (default 200k), the agent summarizes the history into a structured 7-section summary and resets the conversation. The system prompt and last user request are preserved.

## Development

```bash
make all        # vet + lint + test + build
make test       # go test -race -count=1 ./...
make vet        # go vet ./...
make lint       # staticcheck ./... (requires staticcheck)
make build      # go build ./cmd/miclaw
```

## Design Docs

Detailed design documentation lives in [`docs/`](docs/):

- [Architecture Overview](docs/00-architecture-overview.md)
- [System Prompt, Memory & Skills](docs/01-system-prompt-memory-skills.md)
- [Agent Loop & Sub-agents](docs/02-agent-loop-and-subagents.md)
- [Tools](docs/03-tools.md)
- [Signal Integration](docs/04-signal-integration.md)
- [Webhooks](docs/05-webhooks.md)
- [Context Compaction](docs/06-context-compaction.md)
- [LLM Backends](docs/07-llm-backends.md)
- [Sandboxing](docs/08-sandboxing.md)

## License

See [LICENSE](LICENSE) for details.
