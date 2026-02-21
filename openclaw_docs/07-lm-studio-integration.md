# LM Studio Integration

## 1. Overview

OpenClaw treats LM Studio as an **OpenAI-compatible HTTP API provider**. There is no special LM Studio client -- it connects to LM Studio's local server using the standard OpenAI chat completions API.

```
+------------------+     HTTP POST /v1/chat/completions     +------------------+
|                  | ─────────────────────────────────────→ |                  |
|    OpenClaw      |     SSE streaming response              |   LM Studio      |
|    (Agent)       | ←───────────────────────────────────── |   (localhost)    |
|                  |                                        |                  |
+------------------+                                        +------------------+
        ↑                                                          ↑
  Uses pi-ai library                                    Runs local model
  (OpenAI provider)                                     (e.g., MiniMax M2.1)
```

**Key facts:**
- Protocol: OpenAI-compatible REST API at `/v1`
- Default endpoint: `http://127.0.0.1:1234/v1`
- Auth: Dummy API key (LM Studio doesn't enforce auth)
- Streaming: Standard SSE (Server-Sent Events)
- Tool calling: Supported (depends on model capability)
- Model discovery: **Manual only** (no auto-discovery)

---

## 2. Configuration

### Minimal Configuration

```json5
{
  "models": {
    "providers": {
      "lmstudio": {
        "baseUrl": "http://127.0.0.1:1234/v1",
        "apiKey": "lmstudio",
        "api": "openai-responses",
        "models": [
          {
            "id": "minimax-m2.1-gs32",
            "name": "MiniMax M2.1 GS32",
            "reasoning": false,
            "input": ["text"],
            "cost": { "input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0 },
            "contextWindow": 196608,
            "maxTokens": 8192
          }
        ]
      }
    }
  }
}
```

### With Primary Model Assignment

```json5
{
  "agents": {
    "defaults": {
      "model": {
        "primary": "lmstudio/minimax-m2.1-gs32"
      }
    }
  },
  "models": {
    "mode": "merge",
    "providers": {
      "lmstudio": {
        "baseUrl": "http://127.0.0.1:1234/v1",
        "apiKey": "lmstudio",
        "api": "openai-responses",
        "models": [/* as above */]
      }
    }
  }
}
```

### Hybrid (Local + Cloud Fallback)

```json5
{
  "agents": {
    "defaults": {
      "model": {
        "primary": "anthropic/claude-sonnet-4-5",
        "fallbacks": ["lmstudio/minimax-m2.1-gs32", "anthropic/claude-opus-4-6"]
      }
    }
  },
  "models": {
    "mode": "merge",
    "providers": {
      "lmstudio": { /* ... */ }
    }
  }
}
```

The `mode: "merge"` setting keeps both cloud and local providers available.

### Configuration Schema

```typescript
type ModelProviderConfig = {
  baseUrl: string;           // e.g., "http://127.0.0.1:1234/v1"
  apiKey?: string;           // Any dummy value (e.g., "lmstudio")
  auth?: "api-key" | "oauth" | "token" | "aws-sdk";
  api?: ModelApi;            // "openai-responses" or "openai-completions"
  headers?: Record<string, string>;
  authHeader?: boolean;
  models: ModelDefinitionConfig[];
};

type ModelDefinitionConfig = {
  id: string;                // Model ID as loaded in LM Studio
  name?: string;             // Display name
  reasoning?: boolean;       // Extended thinking support
  input?: string[];          // Input types: ["text"], ["text", "image"]
  cost?: { input: number; output: number; cacheRead: number; cacheWrite: number };
  contextWindow?: number;    // Max context tokens
  maxTokens?: number;        // Max output tokens
};

type ModelApi =
  | "openai-completions"        // Standard chat completions
  | "openai-responses"          // Responses API (reasoning separation)
  | "anthropic-messages"
  | "google-generative-ai"
  | "github-copilot"
  | "bedrock-converse-stream"
  | "ollama";                   // Native Ollama API
```

---

## 3. Provider Implementation

### Provider ID

LM Studio registers as provider `"lmstudio"`. It does NOT have a dedicated implementation -- it reuses the OpenAI-compatible provider from the `pi-ai` library.

### Model Resolution Flow

```
User config: agents.defaults.model.primary = "lmstudio/minimax-m2.1-gs32"
    ↓
resolveModel(provider="lmstudio", modelId="minimax-m2.1-gs32")
    ↓
discoverModels() → reads config.models.providers.lmstudio.models[]
    ↓
Returns Model object:
  { api: "openai-responses",
    provider: "lmstudio",
    id: "minimax-m2.1-gs32",
    baseUrl: "http://127.0.0.1:1234/v1",
    contextWindow: 196608,
    maxTokens: 8192 }
```

### Setup Function

```typescript
// src/commands/onboard-auth.config-minimax.ts
async function applyMinimaxProviderConfig(params: {
  config: OpenClawConfig;
  baseUrl?: string;
  apiKey?: string;
}): Promise<void>
```

This function creates the `lmstudio` provider entry with MiniMax M2.1 defaults.

---

## 4. Model Discovery

**LM Studio has NO auto-discovery in openclaw.** Users must manually define models.

Contrast with other local providers:

| Provider | Auto-Discovery | Mechanism |
|----------|---------------|-----------|
| **LM Studio** | No | Manual config only |
| **Ollama** | Yes | Native API `/api/tags` |
| **vLLM** | Yes | OpenAI-compatible `GET /v1/models` |
| **Huggingface** | Yes | `GET /v1/models` |

LM Studio does expose `/v1/models` (OpenAI-compatible), but openclaw doesn't use it for auto-discovery. This could be added but isn't implemented.

---

## 5. Streaming

Streaming uses standard OpenAI SSE format, handled by the `pi-ai` library:

```
POST http://127.0.0.1:1234/v1/chat/completions
Headers:
  Authorization: Bearer lmstudio
  Content-Type: application/json
Body:
  {
    "model": "minimax-m2.1-gs32",
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

The API format (`"openai-responses"` vs `"openai-completions"`) determines how reasoning is separated from final output:

| API | Reasoning | Use Case |
|-----|-----------|----------|
| `openai-responses` | Separates thinking from answer | Models with reasoning capability |
| `openai-completions` | No separation | Standard completion models |

---

## 6. Tool Calling

Tool calling works the same as any OpenAI-compatible provider:

1. Tools serialized to OpenAI JSON Schema format
2. Sent as `tools` array in chat completions request
3. Model responds with `tool_calls` in the response
4. OpenClaw executes tools and feeds results back

**Limitations:**
- Depends on the local model's tool-calling capability
- MiniMax M2.1 GS32 (recommended) supports tool calling
- Smaller/heavily quantized models may not support tools reliably
- From docs: "Use the largest/full-size model variant you can run" and "aggressively quantized or 'small' checkpoints raise prompt-injection risk"

---

## 7. Transcript Sanitization

OpenAI-compatible providers get lighter transcript sanitization:

```typescript
// For "openai-responses" / "openai-completions" APIs:
{
  sanitizeMode: "images-only",           // Only sanitize images (not full)
  sanitizeToolCallIds: false,            // Preserve tool call IDs
  repairToolUseResultPairing: false,     // Don't repair pairing
  allowSyntheticToolResults: false,
}
```

This is lighter than Anthropic/Google providers which get full sanitization.

---

## 8. Comparison: LM Studio vs Other Local Providers

| Feature | LM Studio | Ollama | vLLM |
|---------|-----------|--------|------|
| Protocol | OpenAI-compatible | Native + OpenAI-compat | OpenAI-compatible |
| Default Port | 1234 | 11434 | 8000 |
| Auto-Discovery | No | Yes (`/api/tags`) | Yes (`/v1/models`) |
| Streaming | SSE | NDJSON (native) / SSE | SSE |
| API Key | Dummy value | Dummy value | Optional |
| Tool Calling | Model-dependent | Model-dependent | Model-dependent |
| Dedicated Streaming Code | No | Yes (`ollama-stream.ts`) | No |
| Config API | `openai-responses` | `ollama` | `openai-completions` |

---

## 9. Data Flow

```
1. CONFIG RESOLUTION
   agents.defaults.model.primary = "lmstudio/minimax-m2.1-gs32"
       ↓
2. PROVIDER LOOKUP
   models.providers.lmstudio → { baseUrl, apiKey, api, models[] }
       ↓
3. MODEL MATCH
   Find "minimax-m2.1-gs32" in provider's model list
       ↓
4. CONTEXT ASSEMBLY
   - Message history (user/assistant/tool)
   - System prompt (from buildAgentSystemPrompt)
   - Available tools (serialized to JSON schema)
   - Temperature, max_tokens, streaming flags
       ↓
5. API CALL (via pi-ai OpenAI provider)
   POST http://127.0.0.1:1234/v1/chat/completions
       ↓
6. STREAMING RESPONSE (SSE)
   Process content deltas, tool calls, finish events
       ↓
7. TRANSCRIPT SANITIZATION
   Images-only mode (lighter than Anthropic)
       ↓
8. TOOL EXECUTION (if tool calls present)
   Execute tools → feed results back → loop
       ↓
9. DELIVERY
   Format response for channel (Signal, CLI, etc.)
```
