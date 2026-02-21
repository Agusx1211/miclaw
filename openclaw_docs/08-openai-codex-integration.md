# OpenAI & Codex Integration

## 1. Overview

OpenClaw supports two OpenAI access modes:

1. **OpenAI API Key** (`openai`) - Direct API access with usage-based billing. Uses standard OpenAI endpoints.
2. **OpenAI Codex OAuth** (`openai-codex`) - ChatGPT subscription-based access via OAuth sign-in. Uses Codex-specific endpoints and models.

Both use the OpenAI Responses API for streaming, tool calling, and extended thinking/reasoning.

```
+------------------+                      +------------------+
|                  |  API Key Auth        |                  |
|    OpenClaw      | ──────────────────→  |   api.openai.com |
|    (Agent)       |  OR OAuth Token      |   chatgpt.com    |
|                  | ──────────────────→  |                  |
+------------------+                      +------------------+
```

---

## 2. Authentication

### Option A: API Key

Standard OpenAI API key authentication:

```json5
{
  "env": { "OPENAI_API_KEY": "sk-..." },
  "agents": {
    "defaults": {
      "model": { "primary": "openai/gpt-5.1-codex" }
    }
  }
}
```

### Option B: Codex OAuth (Subscription)

OAuth-based authentication using ChatGPT subscription:

```typescript
// src/commands/openai-codex-oauth.ts
async function loginOpenAICodexOAuth(params: {
  prompter: WizardPrompter;
  runtime: RuntimeEnv;
  isRemote: boolean;
  openUrl: (url: string) => Promise<void>;
  localBrowserMessage?: string;
}): Promise<OAuthCredentials | null>
```

- Uses `loginOpenAICodex()` from the pi-ai library
- Opens browser for OAuth flow
- Localhost callback on port 1455
- Returns credentials: `{ access, refresh, expires, email }`

Configuration:
```json5
{
  "auth": {
    "profiles": {
      "openai-codex:default": {
        "provider": "openai-codex",
        "mode": "oauth"
      }
    }
  },
  "agents": {
    "defaults": {
      "model": { "primary": "openai-codex/gpt-5.3-codex" }
    }
  }
}
```

---

## 3. Supported Models

### OpenAI API Key Models

| Model ID | API | Thinking | Default |
|----------|-----|----------|---------|
| `openai/gpt-5.2` | `openai-responses` | Yes (extended) | |
| `openai/gpt-5.1-codex` | `openai-completions` | No | Yes (`OPENAI_DEFAULT_MODEL`) |
| `openai/gpt-5-mini` | `openai-completions` | No | |

### OpenAI Codex (OAuth) Models

| Model ID | API | Thinking | Default |
|----------|-----|----------|---------|
| `openai-codex/gpt-5.3-codex` | `openai-codex-responses` | Yes | Yes (`OPENAI_CODEX_DEFAULT_MODEL`) |
| `openai-codex/gpt-5.3-codex-spark` | `openai-codex-responses` | Yes | |
| `openai-codex/gpt-5.2-codex` | `openai-codex-responses` | Yes | |
| `openai-codex/gpt-5.1-codex` | `openai-codex-responses` | No | |

### API Types

```typescript
type ModelApi =
  | "openai-completions"         // Standard chat completions
  | "openai-responses"           // Responses API (reasoning separation)
  | "openai-codex-responses"     // Codex-specific Responses API
  | "anthropic-messages"
  | "google-generative-ai"
  // ...
```

---

## 4. Extended Thinking / Reasoning

### Thinking Levels

```typescript
type ThinkLevel = "off" | "minimal" | "low" | "medium" | "high" | "xhigh";
```

OpenAI models (especially Codex) support "xhigh" thinking, which provides the largest reasoning budgets:

```typescript
const XHIGH_MODEL_REFS = [
  "openai/gpt-5.2",
  "openai-codex/gpt-5.3-codex",
  "openai-codex/gpt-5.3-codex-spark",
  // ...
];

function supportsXHighThinking(provider?: string, model?: string): boolean
```

Configuration:
```json5
{
  "agents": {
    "defaults": {
      "model": { "primary": "openai/gpt-5.2" },
      "thinking": "high"  // or "xhigh" for maximum reasoning
    }
  }
}
```

### Reasoning Block Handling

OpenAI's Responses API returns thinking blocks with metadata:

```typescript
type OpenAIThinkingBlock = {
  type?: "thinking";
  thinking?: string;
  thinkingSignature?: string;  // Contains { id: "rs_*", type: "reasoning" }
};
```

Processing:
- Reasoning IDs follow format `rs_*` (reasoning session IDs)
- Metadata stored as: `{ id: "rs_...", type: "reasoning" }` or `{ id: "rs_...", type: "reasoning.simple" }`
- Blocks without proper metadata are dropped for session compatibility
- Reasoning blocks are only valid when followed by non-thinking content

```typescript
// src/agents/pi-embedded-helpers/openai.ts
function downgradeOpenAIReasoningBlocks(messages: AgentMessage[]): AgentMessage[]
```

### Reasoning in Messaging Channels

For Telegram delivery, reasoning is split from the final answer:

```typescript
// src/telegram/reasoning-lane-coordinator.ts
// Extracts <think>/<thinking> tags outside code blocks
// Manages reasoning message buffering
// Sends reasoning and answer as separate messages
```

For Signal/WhatsApp, only the final answer is sent (reasoning stays internal).

---

## 5. Apply Patch Tool (OpenAI-Specific)

### Overview

The `apply_patch` tool is designed to work with OpenAI models that naturally generate patch-format code changes:

```typescript
// src/agents/apply-patch.ts
function createApplyPatchTool(options?: {
  cwd?: string;
  sandbox?: SandboxApplyPatchConfig;
  workspaceOnly?: boolean;
}): AgentTool<typeof applyPatchSchema, ApplyPatchToolDetails>
```

### Patch Format

Uses custom markers (not standard unified diff):

```
*** Begin Patch
*** Add File: src/new-file.ts
+export function hello() {
+  return "world";
+}

*** Update File: src/existing.ts
@@ context line @@
 unchanged line
-old line
+new line
 unchanged line

*** Delete File: src/old-file.ts
*** End Patch
```

Operations:
- `*** Add File: <path>` - Create new files
- `*** Update File: <path>` - Modify existing files
- `*** Delete File: <path>` - Remove files
- `@@ ... @@` - Hunk context markers
- ` ` (space) prefix: context line (unchanged)
- `+` prefix: added line
- `-` prefix: removed line

### Result

```typescript
type ApplyPatchSummary = {
  added: string[];     // New files created
  modified: string[];  // Existing files modified
  deleted: string[];   // Files removed
};
```

### Availability

- Only enabled when using an OpenAI provider (`isOpenAIProvider()` check)
- Controlled by `applyPatchConfig.enabled` and `applyPatchConfig.allowModels`
- Workspace-contained by default (can't write outside workspace)
- Model allowlist example: `["gpt-5.2", "openai/gpt-5.2"]`

---

## 6. Tool Schema Normalization for OpenAI

OpenAI has specific JSON Schema requirements for tool definitions:

```typescript
// src/agents/pi-tools.schema.ts
function normalizeToolParameters(tool, options?: { modelProvider?: string }): AgentTool
```

### OpenAI Requirements

- **Must have `type: "object"` at the top level** (TypeBox root unions compile to `{ anyOf: [...] }` without `type`)
- No special keyword stripping needed (unlike Gemini)
- `anyOf`/`oneOf` schemas are merged into a single object schema with combined properties

### Comparison

| Provider | Schema Handling |
|----------|----------------|
| **OpenAI** | Add `type: "object"` at root, merge unions |
| **Anthropic** | Full JSON Schema draft 2020-12 compliance |
| **Gemini** | Strip constraint keywords, flatten unions |

---

## 7. Transcript Policy (OpenAI-Specific)

OpenAI models get unique transcript handling:

```typescript
// src/agents/transcript-policy.ts
const OPENAI_MODEL_APIS = new Set([
  "openai",
  "openai-completions",
  "openai-responses",
  "openai-codex-responses",
]);
const OPENAI_PROVIDERS = new Set(["openai", "openai-codex"]);

function resolveTranscriptPolicy(params): TranscriptPolicy {
  const isOpenAi = isOpenAiProvider(provider) || isOpenAiApi(params.modelApi);
  return {
    sanitizeMode: isOpenAi ? "images-only" : "full",
    sanitizeToolCallIds: !isOpenAi,         // OpenAI: preserve IDs
    repairToolUseResultPairing: !isOpenAi,  // OpenAI: no repair
    allowSyntheticToolResults: false,
  };
}
```

### Tool Call ID Preservation

OpenAI tool calls have a unique dual-ID format: `call_id|fc_id`

- Each tool call has two IDs: `call_*` and `fc_*`
- These **must be preserved exactly** as returned by the API
- No sanitization or rewriting is performed
- Different from Anthropic/Google which use simpler ID formats

---

## 8. Streaming Differences

### OpenAI Responses API Streaming

```typescript
// Streaming event types from OpenAI Responses API:
| { type: "response.output_item.added"; item: Record<string, unknown> }
| { type: "response.function_call_arguments.delta"; delta: string }
| { type: "response.output_item.done"; item: Record<string, unknown> }
| { type: "response.completed"; response: { usage: { input_tokens, output_tokens, total_tokens } } }
```

### Store Parameter

For direct OpenAI API calls, a `store: true` parameter is injected:

```typescript
// src/agents/pi-embedded-runner/extra-params.ts
function createOpenAIResponsesStoreWrapper(baseStreamFn: StreamFn): StreamFn {
  return (model, context, options) => {
    if (!shouldForceResponsesStore(model)) {
      return underlying(model, context, options);
    }
    // Force store=true for direct OpenAI APIs (api.openai.com, chatgpt.com)
    const originalOnPayload = options?.onPayload;
    return underlying(model, context, {
      ...options,
      onPayload: (payload) => {
        if (payload && typeof payload === "object") {
          (payload as { store?: unknown }).store = true;
        }
        originalOnPayload?.(payload);
      },
    });
  };
}
```

- Direct OpenAI (`api.openai.com`, `chatgpt.com`): forces `store: true`
- Codex responses (non-direct endpoints): uses `store: false`
- This affects conversation state persistence on OpenAI's side

### Comparison with Anthropic Streaming

| Aspect | OpenAI | Anthropic |
|--------|--------|-----------|
| Format | Custom event types | Standard SSE deltas |
| Tool calls | `tool_calls` in delta | `content_block_start/delta/stop` |
| Reasoning | `thinking` blocks with signature | `thinking` content blocks |
| Finish | `response.completed` | `message_stop` |
| Store param | Yes (`store: true/false`) | No |
| Token counting | In `response.completed.usage` | In `message_stop.usage` |

---

## 9. OpenAI-Compatible Gateway Endpoint

OpenClaw exposes its own OpenAI-compatible endpoint so other tools can use it as a proxy:

```
POST /v1/chat/completions
```

```typescript
// src/gateway/openai-http.ts
// Accepts standard OpenAI chat completion format
// Routes to configured agent via model field: "openclaw:<agentId>"
// Supports streaming (SSE) and non-streaming
// Persists sessions via user field

// Response format:
{
  "id": "chatcmpl_...",
  "object": "chat.completion",
  "created": 1234567890,
  "model": "openclaw",
  "choices": [{
    "index": 0,
    "message": { "role": "assistant", "content": "..." },
    "finish_reason": "stop"
  }],
  "usage": { "prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0 }
}
```

This lets external tools (IDEs, scripts, other agents) interact with OpenClaw as if it were an OpenAI API.

---

## 10. Configuration Reference

### OpenAI API Key

```json5
{
  "env": { "OPENAI_API_KEY": "sk-..." },
  "agents": {
    "defaults": {
      "model": { "primary": "openai/gpt-5.1-codex" }
    }
  }
}
```

### OpenAI Codex OAuth

```json5
{
  "auth": {
    "profiles": {
      "openai-codex:default": {
        "provider": "openai-codex",
        "mode": "oauth"
      }
    }
  },
  "agents": {
    "defaults": {
      "model": { "primary": "openai-codex/gpt-5.3-codex" }
    }
  }
}
```

### Extended Thinking

```json5
{
  "agents": {
    "defaults": {
      "model": { "primary": "openai/gpt-5.2" },
      "thinking": "xhigh"
    }
  }
}
```

### Apply Patch Configuration

```json5
{
  "tools": {
    "exec": {
      "applyPatch": {
        "enabled": true,
        "workspaceOnly": true,
        "allowModels": ["gpt-5.2", "openai/gpt-5.2"]
      }
    }
  }
}
```

---

## 11. Complete Data Flow

```
1. AUTH RESOLUTION
   API Key: OPENAI_API_KEY env var
   Codex OAuth: auth.profiles["openai-codex:default"] → OAuth flow → tokens
       ↓
2. MODEL SELECTION
   "openai/gpt-5.2" or "openai-codex/gpt-5.3-codex"
       ↓
3. THINKING LEVEL SETUP
   off / minimal / low / medium / high / xhigh
       ↓
4. TOOL SCHEMA NORMALIZATION
   Ensure top-level type: "object"
   Merge anyOf schemas if present
   Include apply_patch tool (OpenAI only)
       ↓
5. TRANSCRIPT POLICY
   sanitizeMode: "images-only"
   Preserve tool call IDs (call_* and fc_*)
   No pairing repair
       ↓
6. STREAM WRAPPER
   Inject store: true for direct OpenAI API
       ↓
7. API CALL
   POST api.openai.com/v1/responses (or Codex endpoint)
   Stream: SSE with custom event types
       ↓
8. REASONING HANDLING
   Extract thinking blocks with thinkingSignature
   Validate reasoning IDs (rs_*)
   Drop invalid reasoning blocks
       ↓
9. TOOL EXECUTION
   Parse tool_calls (preserving call_id|fc_id format)
   Execute tools (including apply_patch)
   Feed results back with preserved IDs
       ↓
10. DELIVERY
    Telegram: Split reasoning from answer (separate messages)
    Signal/WhatsApp: Final answer only
    CLI: Full output with reasoning
```
