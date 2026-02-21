# LLM Backends

## 1. Overview

Three backends. All OpenAI-compatible. No Anthropic, no Google, no Ollama.

| Backend | Type | Auth | Use Case |
|---------|------|------|----------|
| LM Studio | Local | None (dummy key) | Privacy, offline, free |
| OpenRouter | Cloud | API key | Multi-model access, cloud |
| OpenAI Codex | Cloud | API key or OAuth | Codex-specific models |

All three use the OpenAI chat completions API format. The provider implementation is shared; only the base URL, auth, and model list differ.

---

## 2. Provider Interface

```go
type LLMProvider interface {
    Stream(ctx context.Context, messages []Message, tools []Tool) <-chan ProviderEvent
    Model() ModelInfo
    CountTokens(ctx context.Context, messages []Message, tools []Tool) int
}

type ModelInfo struct {
    Provider      string // "lmstudio", "openrouter", "openai-codex"
    ID            string // model identifier
    ContextWindow int    // max context tokens
    MaxTokens     int    // max output tokens
    Reasoning     bool   // supports extended thinking
}
```

Since all backends are OpenAI-compatible, there's one provider implementation parameterized by config:

```go
type OpenAIProvider struct {
    baseURL   string
    apiKey    string
    model     string
    maxTokens int
    client    *http.Client
}
```

---

## 3. LM Studio

### Configuration

```json
{
    "llm": {
        "provider": "lmstudio",
        "lmstudio": {
            "baseURL": "http://127.0.0.1:1234/v1",
            "model": "minimax-m2.1-gs32",
            "contextWindow": 196608,
            "maxTokens": 8192
        }
    }
}
```

### Details

- **Protocol:** OpenAI-compatible REST API at `/v1`
- **Default endpoint:** `http://127.0.0.1:1234/v1`
- **Auth:** Dummy API key (LM Studio doesn't enforce auth). Send `Authorization: Bearer lmstudio`.
- **Streaming:** Standard SSE (Server-Sent Events)
- **Tool calling:** Supported, depends on local model capability
- **Model discovery:** Manual only. User specifies model ID in config.
- **Cost:** Zero. Local inference.

### Streaming

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
        "max_tokens": 8192
    }

Response: text/event-stream
    data: {"choices":[{"delta":{"content":"Hello"}}]}
    data: {"choices":[{"delta":{"tool_calls":[...]}}]}
    data: [DONE]
```

---

## 4. OpenRouter

### Configuration

```json
{
    "llm": {
        "provider": "openrouter",
        "openrouter": {
            "apiKey": "sk-or-...",
            "model": "anthropic/claude-sonnet-4-5",
            "contextWindow": 200000,
            "maxTokens": 8192
        }
    }
}
```

### Details

- **Protocol:** OpenAI-compatible REST API
- **Endpoint:** `https://openrouter.ai/api/v1`
- **Auth:** API key via `Authorization: Bearer <key>`
- **Streaming:** Standard SSE
- **Tool calling:** Supported, depends on model
- **Model discovery:** Manual. User specifies the OpenRouter model ID (e.g., `anthropic/claude-sonnet-4-5`, `google/gemini-2.5-pro`).
- **Cost:** Per-token, varies by model.

### Extra Headers

OpenRouter expects additional headers for app identification:

```
HTTP-Referer: https://github.com/miclaw
X-Title: Miclaw
```

### Model IDs

OpenRouter uses `provider/model` format:
- `anthropic/claude-sonnet-4-5`
- `anthropic/claude-opus-4`
- `google/gemini-2.5-pro`
- `openai/gpt-5.2`
- `meta-llama/llama-4-maverick`
- etc.

The full model catalog is at OpenRouter's API. We don't enumerate or auto-discover models; the user picks one and puts it in config.

---

## 5. OpenAI Codex

### Configuration (API Key)

```json
{
    "llm": {
        "provider": "openai-codex",
        "openai-codex": {
            "apiKey": "sk-...",
            "model": "gpt-5.1-codex",
            "contextWindow": 200000,
            "maxTokens": 16384
        }
    }
}
```

### Configuration (OAuth)

```json
{
    "llm": {
        "provider": "openai-codex",
        "openai-codex": {
            "auth": "oauth",
            "model": "gpt-5.3-codex",
            "contextWindow": 200000,
            "maxTokens": 16384
        }
    }
}
```

OAuth flow opens a browser, authenticates via ChatGPT subscription, returns tokens.

### Details

- **Protocol:** OpenAI Responses API
- **Endpoints:**
  - API key: `https://api.openai.com/v1`
  - Codex OAuth: `https://chatgpt.com/backend-api/v1` (or similar)
- **Auth:** API key or OAuth bearer token
- **Streaming:** SSE with OpenAI-specific event types
- **Tool calling:** Full support
- **Extended thinking:** Supports `off` through `xhigh` thinking levels

### Thinking Levels

```go
type ThinkLevel string

const (
    ThinkOff     ThinkLevel = "off"
    ThinkMinimal ThinkLevel = "minimal"
    ThinkLow     ThinkLevel = "low"
    ThinkMedium  ThinkLevel = "medium"
    ThinkHigh    ThinkLevel = "high"
    ThinkXHigh   ThinkLevel = "xhigh"
)
```

Configured per-model:
```json
{
    "llm": {
        "openai-codex": {
            "model": "gpt-5.2",
            "thinking": "high"
        }
    }
}
```

### Apply Patch Tool

The `apply_patch` tool is enabled only when using OpenAI Codex models, since those models naturally generate the `*** Begin Patch / *** End Patch` format.

### Tool Call ID Format

OpenAI tool calls use dual IDs: `call_id|fc_id`. These must be preserved exactly as returned by the API.

---

## 6. Streaming Protocol

All three backends use the same streaming format (OpenAI SSE):

```
data: {"id":"...","choices":[{"delta":{"content":"text"}}],"model":"..."}
data: {"id":"...","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_...","function":{"name":"read","arguments":"{..."}}]}}]}
data: {"id":"...","choices":[{"finish_reason":"stop","usage":{"prompt_tokens":1234,"completion_tokens":567}}]}
data: [DONE]
```

### Event Processing

```go
for event := range sseStream {
    switch {
    case event.Delta.Content != "":
        emit(ProviderEvent{Type: ContentDelta, Content: event.Delta.Content})

    case event.Delta.ToolCalls != nil:
        for _, tc := range event.Delta.ToolCalls {
            if tc.ID != "" {
                emit(ProviderEvent{Type: ToolUseStart, ToolCall: &tc})
            } else {
                emit(ProviderEvent{Type: ToolUseDelta, ToolCall: &tc})
            }
        }

    case event.FinishReason != "":
        emit(ProviderEvent{Type: Complete, Response: &response})
    }
}
```

---

## 7. Retry Logic

Shared across all backends:

```
maxRetries = 8
baseBackoff = 2 seconds

For each attempt:
    1. Send request
    2. If success: return
    3. If error:
       - Status 429 (rate limit) or 529 (overloaded):
         backoff = baseBackoff * 2^(attempt-1)
         jitter = backoff * 0.2 * random()
         wait(backoff + jitter)
         Respect Retry-After header if present
         Continue
       - Any other error: return immediately
    4. If max retries exceeded: return error
```

---

## 8. Configuration Reference

```json
{
    "llm": {
        "provider": "lmstudio | openrouter | openai-codex",

        "lmstudio": {
            "baseURL": "http://127.0.0.1:1234/v1",
            "model": "minimax-m2.1-gs32",
            "contextWindow": 196608,
            "maxTokens": 8192
        },

        "openrouter": {
            "apiKey": "sk-or-...",
            "model": "anthropic/claude-sonnet-4-5",
            "contextWindow": 200000,
            "maxTokens": 8192
        },

        "openai-codex": {
            "apiKey": "sk-...",
            "auth": "api-key | oauth",
            "model": "gpt-5.1-codex",
            "contextWindow": 200000,
            "maxTokens": 16384,
            "thinking": "off | minimal | low | medium | high | xhigh"
        }
    }
}
```

---

## 9. Fallback Strategy

The config specifies one active provider. There is no automatic fallback chain. If LM Studio is down, the agent fails. If OpenRouter returns 503, retry logic handles transient errors.

If you want to switch providers, change the config. No runtime provider switching. No `mode: "merge"`. One provider at a time.

This is a deliberate simplification. Fallback chains add complexity and hide failures. If your local model is down, you should know about it.
