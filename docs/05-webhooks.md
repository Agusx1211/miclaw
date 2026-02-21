# Webhooks

## 1. Overview

Webhooks are HTTP endpoints that inject content into the singleton agent thread. When something is POSTed to a webhook, the payload becomes a user message. The agent wakes up and processes it like any other input.

No new thread is created. No sub-agent is spawned. The webhook payload joins the same queue as Signal messages. If the agent decides it needs a sub-agent, it spawns one through the normal `sessions_spawn` tool.

```
External Service
    |
    | HTTP POST
    v
+---+----+
|Webhook |
|Endpoint|
+---+----+
    |
    | Inject as user message
    v
+---+----+
|Agent   |
|Queue   |
+--------+
```

---

## 2. Configuration

```go
type WebhookConfig struct {
    Enabled bool   // default: false
    Listen  string // default: "127.0.0.1:9090"
    Hooks   []WebhookDef
}

type WebhookDef struct {
    ID     string // unique identifier
    Path   string // URL path (e.g., "/hook/deploy")
    Secret string // optional HMAC secret for verification
    Format string // "text" | "json" (default: "text")
}
```

Example config:
```json
{
    "webhooks": {
        "enabled": true,
        "listen": "127.0.0.1:9090",
        "hooks": [
            {
                "id": "deploy-notify",
                "path": "/hook/deploy",
                "secret": "whsec_abc123",
                "format": "json"
            },
            {
                "id": "alert",
                "path": "/hook/alert",
                "format": "text"
            }
        ]
    }
}
```

---

## 3. HTTP Server

A single HTTP server handles all webhook endpoints. It starts on the configured `listen` address when `webhooks.enabled` is true.

### Endpoint Registration

Each `WebhookDef` registers a POST handler at its `path`:

```
POST /hook/deploy  -> webhook "deploy-notify"
POST /hook/alert   -> webhook "alert"
```

GET requests return 405. Non-matching paths return 404.

### Health Check

```
GET /health -> 200 OK
```

---

## 4. Request Processing

### Authentication

If `secret` is set, the request must include an HMAC signature:

```
Header: X-Webhook-Signature: sha256=<hex-encoded HMAC-SHA256>
```

The HMAC is computed over the raw request body using the webhook's secret. If the signature is missing or invalid, return 401.

If `secret` is empty, no authentication is performed.

### Payload Extraction

**Format "text":**
The raw request body is used as the message content.

**Format "json":**
The request body is parsed as JSON. The message content is built from the JSON structure:

```
Webhook: <webhook-id>
```

Followed by the pretty-printed JSON body.

The agent sees the full payload and decides what to do with it.

### Injection

```
1. Parse and validate request
2. Extract payload text
3. Build user message:
   - Content: "[webhook:<id>] <payload text>"
   - Metadata: { source: "webhook", webhook_id: "<id>" }
4. Enqueue in agent input queue
5. Return 202 Accepted immediately
```

The webhook returns 202 before the agent processes the message. The caller does not wait for the agent's response.

---

## 5. Response Retrieval

Webhooks are fire-and-forget by default. The caller sends a payload and gets 202 back.

If the caller needs the agent's response, they can use `sessions_history` or poll a session. But this is not the primary use case. Webhooks are for injecting events, not for request-response flows.

---

## 6. Agent Message Format

When a webhook payload arrives, the agent sees a user message like:

```
[webhook:deploy-notify] {
  "repository": "myapp",
  "branch": "main",
  "status": "success",
  "commit": "abc1234"
}
```

The agent can then decide what to do: notify via Signal, update memory, spawn a sub-agent to investigate, run a command, etc.

---

## 7. Cron Integration

Cron jobs use the same injection mechanism as webhooks. When a cron job fires, its prompt is injected as a user message into the agent thread. The difference is that cron is internal (scheduled by the agent itself via the `cron` tool) while webhooks are external (triggered by HTTP POST).

Both go through the same input queue and are processed identically by the agent loop.

---

## 8. What Webhooks Replace

In openclaw, the "hook system" has internal hooks (command/lifecycle events), plugin hooks (tool/agent pipeline callbacks), and managed hook packs (npm installable). Miclaw replaces all of that with one concept:

**An HTTP endpoint that puts a message in the agent's inbox.**

The agent's intelligence handles the rest. If you want something to happen when a deploy succeeds, POST to the webhook. The agent reads the payload and acts on it based on its system prompt and skills.

No event types. No lifecycle callbacks. No hook metadata. No handler scripts. Just messages.
