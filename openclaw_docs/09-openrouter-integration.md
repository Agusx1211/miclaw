# OpenRouter Integration

## 1. Overview

OpenRouter is a **built-in provider** that acts as a proxy/router to many LLM providers (Anthropic, OpenAI, Google, Meta, etc.) through a single OpenAI-compatible API. OpenClaw treats it as an OpenAI-compatible endpoint with a few OpenRouter-specific additions.

```
+------------------+     OpenAI-compatible API     +------------------+
|                  | ────────────────────────────→ |                  |
|    OpenClaw      |   + Attribution headers        |   OpenRouter     |
|    (Agent)       | ←──────────────────────────── |   (Router)       |
|                  |     SSE streaming              |                  |
+------------------+                                +--------+---------+
                                                             |
                                                    Routes to actual provider
                                                             |
                                               +-------------+-------------+
                                               |             |             |
                                          Anthropic      OpenAI       Google
                                          (Claude)       (GPT)       (Gemini)
```

**Key facts:**
- Base URL: `https://openrouter.ai/api/v1`
- API: OpenAI-compatible (`openai-completions`)
- Auth: Bearer token with `sk-or-*` prefix API keys
- Model format: `openrouter/<provider>/<model>` (e.g., `openrouter/anthropic/claude-sonnet-4-5`)
- Default model: `openrouter/auto` (intelligent routing)
- Model discovery: Yes, via `/api/v1/models` endpoint
- Tool calling: Fully supported (model-dependent)

---

## 2. Configuration

### Minimal Configuration

```json5
{
  "env": { "OPENROUTER_API_KEY": "sk-or-..." },
  "agents": {
    "defaults": {
      "model": { "primary": "openrouter/anthropic/claude-sonnet-4-5" }
    }
  }
}
```

### Auto-Routing (Default)

```json5
{
  "env": { "OPENROUTER_API_KEY": "sk-or-..." },
  "agents": {
    "defaults": {
      "model": { "primary": "openrouter/auto" }
    }
  }
}
```

`openrouter/auto` uses OpenRouter's intelligent routing to pick the best available model.

### CLI Setup

```bash
openclaw onboard --auth-choice apiKey --token-provider openrouter --token "$OPENROUTER_API_KEY"
```

### Auth Profile Storage

Credentials are stored in an auth profile:

```
Profile ID: openrouter:default
Type: api_key
Mode: Bearer token
Location: ~/.openclaw/agents/<agentId>/auth.json
```

### Environment Variable

- `OPENROUTER_API_KEY` - Primary env var
- Key prefix: `sk-or-*` (auto-detected)
- Blocked from sandbox environments (never leaked to child processes)

---

## 3. Provider Implementation

OpenRouter is a **built-in provider** in the `pi-ai` library. OpenClaw does not have a dedicated OpenRouter provider implementation -- it reuses the OpenAI-compatible infrastructure.

### Registration

- Provider ID: `"openrouter"`
- Default model ref: `"openrouter/auto"`
- API type: `"openai-completions"` (standard OpenAI chat completions)
- No custom `models.providers` config needed (built-in to pi-ai catalog)

### Model Resolution Flow

```
agents.defaults.model.primary = "openrouter/anthropic/claude-sonnet-4-5"
    ↓
pi-ai ModelRegistry resolves provider = "openrouter"
    ↓
AuthStorage looks up "openrouter:default" profile
    ↓
API key resolved from profile or OPENROUTER_API_KEY env var
    ↓
Model object returned with baseUrl = "https://openrouter.ai/api/v1"
```

---

## 4. Model Discovery

### Auto-Discovery

OpenRouter supports auto-discovery via the `/models` API:

```
GET https://openrouter.ai/api/v1/models
```

### Model Scanning Function

```typescript
// src/agents/model-scan.ts
const OPENROUTER_MODELS_URL = "https://openrouter.ai/api/v1/models";

async function fetchOpenRouterModels(apiKey?: string): Promise<OpenRouterModel[]>
async function scanOpenRouterModels(options?: ScanOptions): Promise<ScannedModel[]>
```

### Model Metadata Extracted

From each model in the `/models` response:

| Field | Description |
|-------|-------------|
| `id` | Model identifier (e.g., `anthropic/claude-sonnet-4-5`) |
| `name` | Display name |
| `context_length` | Max context window |
| `max_completion_tokens` | Max output tokens |
| `supported_parameters` | Array including `"tools"` if tool calling supported |
| `modality` | Input types: `"text"`, `"text-image"`, etc. |
| `pricing.prompt` | Cost per input token (dollars) |
| `pricing.completion` | Cost per output token (dollars) |
| `pricing.request` | Fixed per-request fee |
| `pricing.image` | Cost per image |
| `pricing.web_search` | Cost for web search |
| `pricing.internal_reasoning` | Cost for reasoning tokens |
| `created_at` | Model creation timestamp |

### Scanning Filters

```bash
openclaw models scan [options]
```

| Filter | Description |
|--------|-------------|
| Free models only | `:free` suffix or `pricing.prompt == 0 && pricing.completion == 0` |
| `--provider <name>` | Filter by provider prefix (e.g., `anthropic`) |
| `--min-params <n>` | Minimum parameter count |
| `--max-age-days <n>` | Maximum model age |

### Free Model Detection

```typescript
function isFreeOpenRouterModel(model: OpenRouterModel): boolean {
  return model.id.endsWith(":free") ||
    (model.pricing.prompt === 0 && model.pricing.completion === 0);
}
```

---

## 5. Attribution Headers

OpenRouter requires/recommends attribution headers for leaderboard tracking:

```typescript
const OPENROUTER_APP_HEADERS: Record<string, string> = {
  "HTTP-Referer": "https://openclaw.ai",
  "X-Title": "OpenClaw",
};
```

### Header Application

Headers are automatically injected via a stream wrapper:

```typescript
// src/agents/pi-embedded-runner/extra-params.ts
function createOpenRouterHeadersWrapper(baseStreamFn: StreamFn): StreamFn {
  return (model, context, options) => {
    // Merge OpenRouter headers with any user-provided headers
    // User headers take precedence
    return underlying(model, context, {
      ...options,
      headers: { ...OPENROUTER_APP_HEADERS, ...options?.headers },
    });
  };
}
```

Applied **only when** `provider === "openrouter"`, during agent initialization in `applyExtraParamsToAgent()`.

---

## 6. Streaming

Standard OpenAI-compatible SSE streaming, handled by pi-ai's `streamSimple()`:

```
POST https://openrouter.ai/api/v1/chat/completions
Headers:
  Authorization: Bearer sk-or-...
  Content-Type: application/json
  HTTP-Referer: https://openclaw.ai
  X-Title: OpenClaw
Body:
  {
    "model": "anthropic/claude-sonnet-4-5",
    "messages": [...],
    "tools": [...],
    "stream": true,
    "temperature": 0.7,
    "max_tokens": 8192
  }

Response: text/event-stream
  data: {"choices":[{"delta":{"content":"Hello"}}]}
  data: {"choices":[{"delta":{"tool_calls":[...]}}]}
  data: [DONE]
```

No OpenRouter-specific streaming parameters. Standard OpenAI format throughout.

---

## 7. Tool Calling

Fully supported through OpenAI-compatible format:

1. Tools serialized as OpenAI JSON Schema `tools` array
2. Sent in chat completions request
3. OpenRouter proxies to underlying model
4. Model responds with `tool_calls` in response
5. OpenClaw executes and feeds results back

### Tool Support Detection

From model metadata:
```typescript
const supportsTools = model.supported_parameters.includes("tools");
```

Models without tool support are flagged during scanning.

---

## 8. Transcript Sanitization

OpenRouter models get **different sanitization based on the underlying model**:

### OpenRouter → Gemini Models

When the model ID contains "gemini", stricter rules apply:

```typescript
const isOpenRouterGemini =
  (provider === "openrouter") && modelId.toLowerCase().includes("gemini");

// Gemini-specific policy:
{
  sanitizeMode: "full",                          // Full content sanitization
  sanitizeToolCallIds: true,                     // Strict tool ID sanitization
  sanitizeThoughtSignatures: {
    allowBase64Only: true,                       // Only base64 signatures
    includeCamelCase: true                       // Include camelCase variants
  },
  repairToolUseResultPairing: true,
  allowSyntheticToolResults: true,
}
```

### OpenRouter → Other Models

Standard OpenAI-compatible sanitization:

```typescript
{
  sanitizeMode: "images-only",                   // Light sanitization
  sanitizeToolCallIds: false,                    // Preserve tool IDs
  repairToolUseResultPairing: false,
  allowSyntheticToolResults: false,
}
```

This is important because OpenRouter routes to many different providers, and each has different requirements for message format validity.

---

## 9. Web Search Fallback

OpenRouter API keys can be used as a fallback for Perplexity web search:

```typescript
// Key resolution priority for web_search tool:
// 1. tools.web.search.perplexity.apiKey (config)
// 2. PERPLEXITY_API_KEY (env var)
// 3. OPENROUTER_API_KEY (fallback - uses OpenRouter as Perplexity proxy)
```

When using OpenRouter for web search:
- Base URL: `https://openrouter.ai/api/v1` (same endpoint)
- Routes to Perplexity models through OpenRouter

---

## 10. Security

### API Key Protection

`OPENROUTER_API_KEY` is explicitly blocked from leaking into sandboxed environments:

```typescript
// src/agents/sandbox/sanitize-env-vars.ts
// Blocked patterns:
/^OPENROUTER_API_KEY$/i,
/^(AZURE|AZURE_OPENAI|COHERE|AI_GATEWAY|OPENROUTER)_API_KEY$/i,
```

API keys are never passed to child processes, exec commands, or sandbox containers.

### Key Format Detection

```typescript
const OPENROUTER_KEY_PREFIXES = ["sk-or-"];
```

Keys starting with `sk-or-` are automatically recognized as OpenRouter keys.

---

## 11. Complete Data Flow

```
1. MODEL SELECTION
   "openrouter/anthropic/claude-sonnet-4-5"
       ↓
2. AUTH RESOLUTION
   AuthStorage → "openrouter:default" profile → sk-or-* key
   OR OPENROUTER_API_KEY env var
       ↓
3. STREAM OPTIONS
   Inject attribution headers:
     HTTP-Referer: https://openclaw.ai
     X-Title: OpenClaw
       ↓
4. CONTEXT ASSEMBLY
   Messages + system prompt + tools (OpenAI JSON Schema format)
       ↓
5. API CALL (via pi-ai OpenAI-compatible provider)
   POST https://openrouter.ai/api/v1/chat/completions
   Bearer sk-or-...
       ↓
6. OPENROUTER ROUTING
   Routes to underlying provider (Anthropic, OpenAI, Google, etc.)
       ↓
7. STREAMING RESPONSE (SSE)
   Standard OpenAI format
       ↓
8. TRANSCRIPT SANITIZATION
   Based on underlying model:
     Gemini → full sanitization + strict tool IDs
     Others → images-only (light)
       ↓
9. TOOL EXECUTION (if tool calls present)
   Execute → feed results → loop
       ↓
10. DELIVERY
    Format for channel (Signal, CLI, etc.)
```

---

## 12. Comparison with Direct Providers

| Aspect | OpenRouter | Direct Anthropic | Direct OpenAI |
|--------|-----------|-----------------|---------------|
| API | OpenAI-compatible | Anthropic Messages | OpenAI native |
| Auth | Bearer `sk-or-*` | Bearer `sk-ant-*` | Bearer `sk-*` |
| Model format | `openrouter/provider/model` | `anthropic/model` | `openai/model` |
| Headers | Attribution required | Standard | Standard |
| Tool calling | Proxied | Native | Native |
| Streaming | OpenAI SSE | Anthropic SSE | OpenAI SSE |
| Auto-routing | Yes (`/auto`) | No | No |
| Multi-provider | Yes | No | No |
| Pricing | Per-model from `/models` | Fixed | Fixed |
| Transcript policy | Varies by underlying model | Full sanitization | Images-only |
