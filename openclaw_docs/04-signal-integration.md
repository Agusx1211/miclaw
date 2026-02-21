# Signal Integration

## 1. Architecture Overview

OpenClaw connects to Signal via `signal-cli` (external CLI tool by AsamK) using:

- **JSON-RPC 2.0 API** for commands (send, receive, reactions, etc.)
- **HTTP daemon mode** with optional auto-spawn
- **Server-Sent Events (SSE)** for incoming message streaming

This is NOT embedded Signal protocol -- it's a client/server architecture where signal-cli runs as an HTTP daemon.

```
+------------------+     HTTP JSON-RPC      +------------------+
|                  | ←────────────────────→ |                  |
|    OpenClaw      |     SSE Events         |   signal-cli     |
|    (Agent)       | ←────────────────────  |   (HTTP daemon)  |
|                  |                        |                  |
+------------------+                        +------------------+
                                                    |
                                            Signal Protocol
                                                    |
                                            Signal Servers
```

### Key Files

| File | Purpose |
|------|---------|
| `src/signal/client.ts` | RPC client, SSE streaming, HTTP communication |
| `src/signal/daemon.ts` | signal-cli process spawning & lifecycle |
| `src/signal/accounts.ts` | Account config resolution, multi-account |
| `src/signal/monitor.ts` | Main event loop orchestrator |
| `src/signal/monitor/event-handler.ts` | Core message processing (678 lines) |
| `src/signal/monitor/event-handler.types.ts` | Type definitions |
| `src/signal/monitor/mentions.ts` | Mention rendering from metadata |
| `src/signal/send.ts` | Outbound message/typing/receipts |
| `src/signal/send-reactions.ts` | Reaction add/remove via RPC |
| `src/signal/format.ts` | Markdown → Signal text conversion & chunking |
| `src/signal/identity.ts` | Sender parsing & access control |
| `src/signal/sse-reconnect.ts` | Exponential backoff reconnection |
| `src/signal/reaction-level.ts` | Reaction permission resolution |
| `src/signal/rpc-context.ts` | RPC config resolution helper |
| `src/signal/probe.ts` | Health check & version query |
| `src/channels/plugins/normalize/signal.ts` | Target normalization |
| `src/channels/plugins/outbound/signal.ts` | Outbound adapter (chunker) |
| `src/channels/plugins/actions/signal.ts` | Message actions (reactions) |
| `src/channels/plugins/onboarding/signal.ts` | Interactive setup wizard |
| `src/commands/signal-install.ts` | Auto-install signal-cli binary |

---

## 2. Configuration

### Full Configuration Schema

```typescript
type SignalAccountConfig = {
  // Identity
  name?: string;                     // Display name for CLI
  account?: string;                  // E.164 format: +15551234567
  enabled?: boolean;                 // Default true

  // Connection
  httpUrl?: string;                  // Full URL overrides host:port
  httpHost?: string;                 // Default 127.0.0.1
  httpPort?: number;                 // Default 8080
  cliPath?: string;                  // Default "signal-cli"
  autoStart?: boolean;               // Auto-spawn daemon (default: true if no httpUrl)
  startupTimeoutMs?: number;         // Max 120000ms, default 30000

  // Receive Settings
  receiveMode?: "on-start" | "manual";
  ignoreAttachments?: boolean;
  ignoreStories?: boolean;
  sendReadReceipts?: boolean;

  // Access Control (DMs)
  dmPolicy?: "pairing" | "allowlist" | "open" | "disabled";
  allowFrom?: Array<string | number>; // E.164 or uuid:<id>

  // Group Settings
  groupPolicy?: "open" | "allowlist" | "disabled";
  groupAllowFrom?: Array<string | number>;

  // History & Formatting
  historyLimit?: number;              // Group message history (default 50)
  dmHistoryLimit?: number;            // DM turn limit
  dms?: Record<string, DmConfig>;     // Per-DM config overrides
  textChunkLimit?: number;            // Default 4000
  chunkMode?: "length" | "newline";   // Default "length"
  mediaMaxMb?: number;                // Default 8

  // Reactions
  reactionNotifications?: "off" | "own" | "all" | "allowlist";
  reactionAllowlist?: Array<string | number>;
  reactionLevel?: "off" | "ack" | "minimal" | "extensive";
  actions?: { reactions?: boolean };

  // Advanced
  blockStreaming?: boolean;
  blockStreamingCoalesce?: BlockStreamingCoalesceConfig;
  markdown?: MarkdownConfig;
  configWrites?: boolean;             // Default true
  responsePrefix?: string;
  capabilities?: string[];
};

// Multi-account support
type SignalConfig = {
  accounts?: Record<string, SignalAccountConfig>;
} & SignalAccountConfig;  // Top-level acts as default/single-account
```

### Example Configuration

```json
{
  "channels": {
    "signal": {
      "enabled": true,
      "cliPath": "/usr/local/bin/signal-cli",
      "accounts": {
        "main": {
          "account": "+15551234567",
          "dmPolicy": "pairing",
          "allowFrom": ["+15559876543"]
        },
        "support": {
          "account": "+15558888888",
          "dmPolicy": "open",
          "reactionLevel": "extensive"
        }
      }
    }
  }
}
```

---

## 3. Daemon Management

### Auto-Start Flow

```
monitorSignalProvider()
    ↓
If autoStart=true:
    Spawn signal-cli daemon:
      signal-cli -a <account> daemon --http --send-read-receipts
    ↓
    Wait for readiness:
      POST /api/v1/check every 150ms
      Timeout: startupTimeoutMs (default 30s, max 120s)
      Log progress after 10s
    ↓
    Handle receiveMode:
      "on-start" → receive pending messages
      "manual" → skip
    ↓
Else (external daemon):
    Connect to httpUrl or httpHost:httpPort
```

---

## 4. Inbound Message Flow

### SSE Event Reception

```typescript
// Connects to HTTP GET /api/v1/events?account=<account>
// Parses Server-Sent Events in real-time
// Handles reconnection with exponential backoff (1s→2s→4s→...→10s max)
// ±20% jitter to avoid thundering herd
// Each successful event resets attempt counter
```

### Complete Inbound Processing Pipeline

```
SSE Event (receive)
    ↓
Parse SignalEnvelope
├── sourceNumber/sourceUuid → SignalSender
├── dataMessage/reaction → routing
└── timestamp, mentions, attachments
    ↓
Validation Checks
├── Self-message loop detection (ignore if account match)
├── Reaction-only messages → system event + exit
└── Sync messages → exit
    ↓
[For Data Messages]
├── Mention rendering (￼ → @uuid/@phone)
├── Access control (dmPolicy/groupPolicy)
├── Group history recording
├── Attachment fetching (base64 decode → file store)
├── Command authorization checking
├── Mention gating (requireMention bypass)
└── Read receipt sending (if sendReadReceipts=true)
    ↓
Debounce Enqueue
├── Key: signal:<accountId>:<conversationId>:<senderPeerId>
└── Flush after debounceMs OR on control command
    ↓
Agent Dispatch
├── Typing indicator started
├── Message processed by agent
├── Tool results captured
└── Typing stopped
    ↓
Reply Delivery
├── Text chunked to textChunkLimit
├── Markdown → Signal text styles
├── Attachments sent separately
├── Each chunk sent via sendMessageSignal()
└── Return timestamps
```

### SignalEnvelope (Incoming Data Structure)

```typescript
type SignalEnvelope = {
  sourceNumber?: string;    // "+15551234567"
  sourceUuid?: string;      // "123e4567-e89b-..."
  sourceName?: string;      // "Alice"
  timestamp?: number;       // Unix ms
  dataMessage?: {
    timestamp?: number;
    message?: string;       // Message text
    attachments?: Array<{
      id?: string;
      contentType?: string;  // "image/jpeg"
      filename?: string;
      size?: number;
    }>;
    mentions?: Array<{
      uuid?: string;
      number?: string;
      start?: number;
      length?: number;
    }>;
    groupInfo?: {
      groupId?: string;
      groupName?: string;
    };
    quote?: { text?: string };
    reaction?: { emoji: string; ... };
  };
  editMessage?: { dataMessage?: {...} };
  syncMessage?: unknown;   // Ignored
  reactionMessage?: {
    emoji?: string;
    targetAuthor?: string;
    targetAuthorUuid?: string;
    targetSentTimestamp?: number;
    isRemove?: boolean;
    groupInfo?: { groupId?: string; groupName?: string };
  };
};
```

---

## 5. Access Control

### DM Policies

| Policy | Behavior |
|--------|----------|
| `pairing` | New senders must submit pairing code (1h TTL). Approved senders added to allowFrom. |
| `allowlist` | Only senders in `allowFrom[]` can DM |
| `open` | Anyone can DM |
| `disabled` | DMs completely disabled |

### Group Policies

| Policy | Behavior |
|--------|----------|
| `open` | Respond in any group |
| `allowlist` | Only groups in `groupAllowFrom[]` |
| `disabled` | Groups completely disabled |

### Pairing System

```bash
# List pending pairing codes
openclaw pairing list signal

# Approve a pairing request (adds sender to allowFrom)
openclaw pairing approve signal <CODE>
```

Pairing data stored in `/tmp/openclaw/pairing/<channel>.json` with 1-hour TTL.

---

## 6. Session Routing

### Session Key Structure

```
Main DM:    <agent>:<account-id>:signal:dm:<sender-uuid-or-e164>
Group:      <agent>:<account-id>:signal:group:<group-id>
```

- **DMs:** Share agent's main session per sender
- **Groups:** Isolated sessions per group (separate conversation history)

---

## 7. Group Message History

### History Tracking

```typescript
type HistoryEntry = {
  sender: string;       // Display name
  body: string;         // Message text
  timestamp?: number;   // Unix ms
  messageId?: string;   // Timestamp string
};

// Per-group history
groupHistories: Map<string, HistoryEntry[]>
```

- Messages from all group members recorded (up to `historyLimit`, default 50)
- History formatted with sender info and message IDs
- Included as context when agent processes a group message
- **Cleared after agent reply** (unless history disabled)

---

## 8. Attachment Handling

### Inbound Attachments

```typescript
async function fetchAttachment(params: {
  baseUrl: string;
  account?: string;
  attachment: SignalAttachment;     // {id, contentType, filename, size}
  sender?: string;
  groupId?: string;
  maxBytes: number;                 // Default 8MB
}): Promise<{ path: string; contentType?: string } | null>
```

- RPC call: `getAttachment` with base64-encoded response
- Saved to media store with timestamp naming
- Only first attachment processed per message (placeholder for rest)
- Media types detected: `<media:image>`, `<media:video>`, etc.
- Configurable: `ignoreAttachments?: boolean`, `mediaMaxMb?: number`

### Outbound Attachments

```typescript
// Can send with caption on first message
// Multiple media URLs chunked separately
// RPC param: attachments: ["/path/to/file"]
```

---

## 9. Outbound Message Flow

### Send Process

```
sendMessageSignal(target, text, opts)
    ↓
Parse Target
├── signal:+15551234567 → recipient
├── signal:group:<id> → groupId
├── signal:username:<name> → username
└── uuid:<id> → recipient (raw UUID)
    ↓
Resolve Config
├── Account settings (baseUrl, account param)
└── Media limits (maxBytes)
    ↓
[If mediaUrl provided]
├── Fetch from URL
├── Validate size
└── Store locally
    ↓
Format Text
├── Markdown → Signal IR (linkify, spoilers, etc.)
├── Map to text styles (BOLD, ITALIC, etc.)
└── Encode positions
    ↓
Build RPC Params
{
  message: "formatted text",
  "text-style": ["0:5:BOLD", "5:10:ITALIC"],
  recipient: ["+15551234567"],  // OR groupIds/username
  attachments: ["/path/to/file"],
  account: "+15559876543"
}
    ↓
POST /api/v1/rpc (method: "send")
    ↓
Returns: { timestamp: <unix-ms> }
```

### Text Formatting

```typescript
type SignalTextStyle = "BOLD" | "ITALIC" | "STRIKETHROUGH" | "MONOSPACE" | "SPOILER";

type SignalTextStyleRange = {
  start: number;      // Character position
  length: number;     // Style span length
  style: SignalTextStyle;
};
```

**Markdown → Signal conversion:**
- `**bold**` → BOLD
- `*italic*` → ITALIC
- `` `code` `` → MONOSPACE
- `~~strike~~` → STRIKETHROUGH
- `||spoiler||` → SPOILER
- `[label](url)` → "label (url)" in plain text

### Chunking Strategy

- Default chunk limit: 4000 characters
- Optional newline-aware chunking (`chunkMode: "newline"`)
- Each chunk sent as separate message
- **Text styles preserved across chunks:**
  ```
  Original: "**very long text**" (4000+ chars)
  Chunk 1: "very long te" (start=0, length=12, style=BOLD)
  Chunk 2: "xt" (start=0, length=2, style=BOLD)
  ```
- Trailing whitespace trimmed

---

## 10. Typing Indicators

```typescript
// Sent before reply starts
await sendTypingSignal(target, account, baseUrl);

// Continuously refreshed during streaming
// Timer-based: re-sent every few seconds while agent is processing

// Stopped after reply complete
// Support for both groups and DMs
```

---

## 11. Reactions

### Sending Reactions

```typescript
await sendReactionSignal(recipient, targetTimestamp, emoji, opts);
// RPC method: sendReaction
// Params: {
//   emoji: "👍",
//   targetTimestamp: 1737630212345,
//   recipients: ["+15551234567"],
//   account: "+15559876543"
// }

await removeReactionSignal(recipient, targetTimestamp, emoji, opts);
// Same but adds: remove: true
```

### Reaction Notification Modes

| Mode | Behavior |
|------|----------|
| `"off"` | Ignore reaction events entirely |
| `"own"` | Only notify reactions to bot's own messages (default) |
| `"all"` | Notify all reactions received |
| `"allowlist"` | Notify reactions from senders in reactionAllowlist |

### Reaction Level (Agent Behavior)

| Level | Behavior |
|-------|----------|
| `"off"` | No agent reactions at all |
| `"ack"` | Only automatic 👀 when processing (no agent-initiated) |
| `"minimal"` | Agent can react but sparingly (default) |
| `"extensive"` | Agent can react liberally |

---

## 12. Read Receipts

- **Daemon-based** when `autoStart=true`: via `--send-read-receipts` flag
- **Manual** when external daemon: via `sendReadReceiptSignal()`
- Only for DMs (Signal doesn't expose group read receipts)

---

## 13. SSE Reconnection

```typescript
// Exponential backoff: 1s → 2s → 4s → ... → 10s (max)
// Jitter: ±20% to avoid thundering herd
// Continues indefinitely until abort signal
// Each successful event resets attempt counter
```

---

## 14. Channel Plugin Integration

### Plugin Registration

```typescript
signal: {
  probeSignal,                  // Health check
  sendMessageSignal,            // Send text/media
  monitorSignalProvider,        // Listen for messages
  messageActions: signalMessageActions,  // React, etc.
}
```

### Outbound Adapter

```typescript
signalOutbound: ChannelOutboundAdapter = {
  deliveryMode: "direct",
  chunker: chunkText,
  textChunkLimit: 4000,
  sendText: async ({ cfg, to, text, accountId }) => ...,
  sendMedia: async ({ cfg, to, text, mediaUrl }) => ...
}
```

### Message Action Handler

```typescript
signalMessageActions: ChannelMessageActionAdapter = {
  listActions: ({ cfg }) => ["send", "react"],
  supportsAction: ({ action }) => action !== "send",
  handleAction: async ({ action, params }) => {
    if (action === "react") {
      await sendReactionSignal(...)
    }
  }
}
```

---

## 15. Signal Account Setup

### Linking to Existing Account

```bash
signal-cli link -n "OpenClaw"
# Scan QR code in Signal app → Linked Devices
```

### Registering New Number

```bash
signal-cli -a +<BOT_PHONE> register --captcha '<URL>'
signal-cli -a +<BOT_PHONE> verify <CODE>
```

### Auto-Install

```bash
openclaw signal-install
# Downloads and installs signal-cli binary
```

---

## 16. Error Handling & Resilience

| Scenario | Handling |
|----------|---------|
| SSE connection drop | Exponential backoff reconnection (1s-10s) |
| Daemon not ready | Poll /api/v1/check every 150ms, timeout after 30s |
| RPC error (400/401) | Return error with details |
| HTTP timeout | Default 10s per RPC call |
| JSON parse error | Log and skip message |
| Network error | SSE reconnection handles it |
| Oversized attachment | Skip with placeholder message |
