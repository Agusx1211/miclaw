# Signal Integration

## 1. Architecture

Miclaw connects to Signal via `signal-cli` running as an HTTP daemon. This is not embedded Signal protocol -- it's a client/server architecture.

```
+------------------+     HTTP JSON-RPC      +------------------+
|                  | <--------------------> |                  |
|    Miclaw        |     SSE Events         |   signal-cli     |
|    (Agent)       | <--------------------  |   (HTTP daemon)  |
|                  |                        |                  |
+------------------+                        +------------------+
                                                    |
                                            Signal Protocol
                                                    |
                                            Signal Servers
```

- **Inbound:** SSE stream from `/api/v1/events`
- **Outbound:** JSON-RPC POST to `/api/v1/rpc`
- **Protocol:** JSON-RPC 2.0 over HTTP

---

## 2. Configuration

```go
type SignalConfig struct {
    Enabled    bool   // default: true
    Account    string // E.164 format: "+15551234567"
    HTTPHost   string // default: "127.0.0.1"
    HTTPPort   int    // default: 8080
    CLIPath    string // default: "signal-cli"
    AutoStart  bool   // auto-spawn daemon (default: true)

    // Access control
    DMPolicy   string   // "allowlist" | "open" | "disabled"
    AllowFrom  []string // E.164 numbers or "uuid:<id>"

    GroupPolicy    string   // "open" | "allowlist" | "disabled"
    GroupAllowFrom []string

    // Formatting
    TextChunkLimit int // default: 4000
    MediaMaxMB     int // default: 8
}
```

Single account only. No multi-account support.

---

## 3. Daemon Management

### Auto-Start

```
If AutoStart == true:
    Spawn: signal-cli -a <account> daemon --http --send-read-receipts
    |
    Wait for readiness:
      POST /api/v1/check every 150ms
      Timeout: 30 seconds
    |
    Start SSE listener
Else:
    Connect to HTTPHost:HTTPPort directly
```

---

## 4. Inbound Message Flow

### SSE Reception

Connect to `GET /api/v1/events?account=<account>`. Parse Server-Sent Events in real-time. Reconnect with exponential backoff (1s -> 2s -> 4s -> max 10s, +/- 20% jitter).

### Processing Pipeline

```
SSE Event
    |
Parse SignalEnvelope
+-- sourceNumber/sourceUuid -> sender identity
+-- dataMessage -> text, attachments, mentions, group info
+-- timestamp
    |
Validation
+-- Self-message loop detection (ignore if sender == own account)
+-- Access control (DMPolicy / GroupPolicy check)
    |
Processing
+-- Render mentions (replace mention markers with readable names)
+-- Fetch attachments (if any, up to MediaMaxMB)
    |
Inject into Agent Thread
+-- Format as user message
+-- Include sender info, group context if applicable
+-- Queue for agent processing
    |
Agent Processes Turn
    |
Format Response
+-- Markdown -> Signal text styles
+-- Chunk at TextChunkLimit (4000 chars)
+-- Send each chunk via JSON-RPC
```

### Envelope Structure

```go
type SignalEnvelope struct {
    SourceNumber string
    SourceUUID   string
    SourceName   string
    Timestamp    int64
    DataMessage  *DataMessage
}

type DataMessage struct {
    Message     string
    Attachments []Attachment
    Mentions    []Mention
    GroupInfo   *GroupInfo
    Reaction    *Reaction
}
```

---

## 5. Access Control

### DM Policies

| Policy | Behavior |
|--------|----------|
| `allowlist` | Only senders in AllowFrom can DM |
| `open` | Anyone can DM |
| `disabled` | DMs completely disabled |

### Group Policies

| Policy | Behavior |
|--------|----------|
| `open` | Respond in any group |
| `allowlist` | Only groups in GroupAllowFrom |
| `disabled` | Groups completely disabled |

No pairing system. Use `allowlist` or `open`.

---

## 6. Outbound Message Flow

### Sending

```
Agent produces response text
    |
Convert Markdown to Signal
+-- **bold** -> BOLD style
+-- *italic* -> ITALIC style
+-- `code` -> MONOSPACE style
+-- ~~strike~~ -> STRIKETHROUGH style
+-- [label](url) -> "label (url)" plain text
    |
Chunk text at 4000 chars
+-- Preserve text styles across chunk boundaries
    |
For each chunk:
    POST /api/v1/rpc
    method: "send"
    params: {
        message: "text",
        text-style: ["0:5:BOLD", ...],
        recipient: ["+15551234567"],  // or groupId
        account: "+account"
    }
```

### Text Styles

```go
type TextStyle struct {
    Start  int    // character position
    Length int    // style span
    Style  string // BOLD, ITALIC, STRIKETHROUGH, MONOSPACE
}

// Encoded as: "start:length:STYLE"
```

---

## 7. Attachments

### Inbound

```go
type Attachment struct {
    ID          string
    ContentType string
    Filename    string
    Size        int
}
```

- Fetched via RPC: `getAttachment` with base64-encoded response
- Max size: MediaMaxMB (default 8MB)
- Stored locally with timestamp naming
- Included as binary parts in user message

### Outbound

Attachments sent as file paths in the RPC `send` call.

---

## 8. Session Routing

All Signal messages route to the singleton agent. The session key encodes the conversation source:

```
DM:    signal:dm:<sender-uuid>
Group: signal:group:<group-id>
```

Both route to the same agent. The session key is metadata, not a routing decision.

---

## 9. Typing Indicators

- Sent before agent starts processing
- Refreshed periodically during processing
- Stopped after response is sent

```go
// POST /api/v1/rpc, method: "sendTyping"
```

---

## 10. Error Handling

| Scenario | Handling |
|----------|---------|
| SSE connection drop | Exponential backoff reconnection (1s-10s) |
| Daemon not ready | Poll every 150ms, timeout after 30s |
| RPC error | Return error, log, continue |
| Oversized attachment | Skip with placeholder message |
| JSON parse error | Log and skip message |
