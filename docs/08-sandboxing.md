# Sandboxing

The agent runs inside a single long-lived Docker container. The main agent and all sub-agents share this container — there is no per-agent or per-request container. The container starts once and stays running. Three things are configurable: network access, filesystem mounts, and host command execution.

## 1. Network Access

Docker network mode is set per-agent in config.

```json
{
  "sandbox": {
    "network": "none"
  }
}
```

| Value | Effect |
|-------|--------|
| `"none"` | No network. Agent cannot reach anything. Default. |
| `"host"` | Full host network. Agent can reach everything the host can. |
| `"bridge"` | Standard Docker bridge. Agent gets outbound internet but no host services unless explicitly exposed. |
| Custom network name | Attach to a user-defined Docker network. Use this for fine-grained control (e.g., allow access to specific services only). |

Maps directly to `docker run --network=<value>`. No abstraction layer.

## 2. Filesystem Mounts

The agent sees only what you mount. Config specifies a list of bind mounts.

```json
{
  "sandbox": {
    "mounts": [
      {"host": "/home/user/projects/foo", "container": "/workspace", "mode": "rw"},
      {"host": "/home/user/reference",    "container": "/ref",       "mode": "ro"}
    ]
  }
}
```

| Field | Description |
|-------|-------------|
| `host` | Absolute path on the host. |
| `container` | Absolute path inside the container. |
| `mode` | `"rw"` (read-write) or `"ro"` (read-only). Default: `"ro"`. |

Each mount becomes a `docker run -v host:container:mode` flag. The workspace directory (`/workspace` by convention) should always be mounted. Everything else is optional.

No volume drivers. No tmpfs. No named volumes. Bind mounts only.

## 3. Host Command Execution via SSH

The agent inside Docker can execute specific commands on the host through SSH with forced commands. This is the only way the container talks back to the host.

### Architecture

```
+---------------------------+          SSH (forced command)          +------------------+
|  Docker Container         | -------- pipo-runner@host ----------> |  Host            |
|  (agent process)          |          only allowed commands        |  (pipo, etc.)    |
|  has private key (secret) |                                       |                  |
+---------------------------+                                       +------------------+
```

### Host Setup

A dedicated user with no shell, no password, no forwarding:

```
# /etc/ssh/sshd_config
Match User pipo-runner
    PasswordAuthentication no
    KbdInteractiveAuthentication no
    PermitTTY no
    X11Forwarding no
    AllowTcpForwarding no
    PermitTunnel no
    GatewayPorts no
```

The user's `~/.ssh/authorized_keys` pins each key to a command wrapper:

```
restrict,command="/usr/local/sbin/miclaw-cmd-wrapper" ssh-ed25519 AAAAC3... agent@container
```

### Command Wrapper

A small script on the host that reads `SSH_ORIGINAL_COMMAND`, validates it against an allowlist, and execs the real binary. The wrapper is the enforcement point.

The ready-to-use script is now checked in as `deploy/miclaw-cmd-wrapper` (install it as
`/usr/local/sbin/miclaw-cmd-wrapper`). It uses first-token matching, which mirrors container-side
`tools/exec.go` behavior.

```bash
# See deploy/miclaw-cmd-wrapper for the full script.
SSH_ORIGINAL_COMMAND='git status'  # allowed
SSH_ORIGINAL_COMMAND='rm -rf /'    # denied
```

The allowlist is configured per deployment. If a command needs root:

```
# /etc/sudoers.d/miclaw
pipo-runner ALL=(root) NOPASSWD: /usr/local/bin/pipo
```

### Container Side

The private key is injected as a Docker secret (never baked into the image). The container reaches the host via `host.docker.internal`:

```bash
docker run \
  --add-host=host.docker.internal:host-gateway \
  --secret id=ssh_key,src=./pipo_key \
  ...
```

From inside the container, the agent runs host commands via:

```bash
ssh -i /run/secrets/ssh_key -o StrictHostKeyChecking=accept-new pipo-runner@host.docker.internal <command>
```

### Config

```json
{
  "sandbox": {
    "host_commands": {
      "enabled": true,
      "ssh_key_secret": "ssh_key",
      "user": "pipo-runner",
      "host": "host.docker.internal",
      "allowed": ["pipo", "git status", "docker ps"]
    }
  }
}
```

The `allowed` list here is documentation/validation on the container side. The real enforcement is the wrapper script on the host. Both should match.

## 4. Full Config Example

```json
{
  "sandbox": {
    "network": "none",
    "mounts": [
      {"host": "/home/user/projects/foo", "container": "/workspace", "mode": "rw"},
      {"host": "/home/user/.miclaw/workspace", "container": "/config", "mode": "ro"}
    ],
    "host_commands": {
      "enabled": true,
      "ssh_key_secret": "ssh_key",
      "user": "pipo-runner",
      "host": "host.docker.internal",
      "allowed": ["pipo"]
    }
  }
}
```

## 5. Container Lifecycle

The container is long-lived. It starts once when miclaw launches and stays running until miclaw shuts down.

- **One container, all agents.** The main agent and every sub-agent it spawns run as goroutines inside the same Go process, inside the same container. Sub-agents are not separate containers.
- **No restart per request.** A Signal message or webhook does not restart the container. The process inside handles all requests sequentially (main agent) or in parallel (sub-agents).
- **Crash = restart.** If the process inside the container dies, Docker restarts it (`--restart=unless-stopped`). State survives via mounted volumes and the SQLite database.

## 6. Implementation Notes

- The `exec` tool detects when a command matches a host command and routes it through SSH automatically. The agent doesn't need to know about SSH.
- Container image is minimal: Go binary + SSH client. Nothing else.
- No Docker socket mounting. Ever.
- The container runs as a non-root user inside Docker.
- If `host_commands.enabled` is false or absent, the container has no way to execute anything on the host.
