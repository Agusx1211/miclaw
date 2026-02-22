# Sandboxing

Miclaw sandboxing is **host runtime + containerized tool execution**.

- The `miclaw` process runs on the host.
- A Docker container is started and kept alive as a sandbox bridge.
- Filesystem/exec tool calls are dispatched into that container.
- Signal/webhook/provider/memory runtime services stay on the host.

Miclaw does **not** run a full nested runtime loop inside Docker.

## 1. Lifecycle

When `sandbox.enabled=true`:

1. Host miclaw starts a detached Docker container.
2. Tool calls are proxied with `docker exec` into that container.
3. On shutdown, miclaw stops the container.

One container per miclaw process.

## 2. Network

`"sandbox.network"` maps directly to Docker `--network`:

| Value | Effect |
|-------|--------|
| `none` | No container egress network. Default. |
| `host` | Host network namespace. |
| `bridge` | Default Docker bridge networking. |
| Custom name | Attach to a user-defined Docker network. |

## 3. Filesystem Mounts

Miclaw always mounts:

- `workspace` path as `rw`
- miclaw executable as `ro` (for internal tool-call dispatch)

Optional mounts come from `sandbox.mounts`:

```json
{
  "sandbox": {
    "enabled": true,
    "mounts": [
      {"host": "/home/user/ref", "container": "/ref", "mode": "ro"}
    ]
  }
}
```

Only mounted paths are visible to sandboxed tools.

## 4. Tool Routing

When sandboxing is enabled, these tools run in Docker:

- `read`
- `write`
- `edit`
- `apply_patch`
- `grep`
- `glob`
- `ls`
- `exec`

`exec` background mode is disabled in sandbox bridge mode.

## 5. Host Command Proxy

You can expose selected host commands from inside the container.

- Set `sandbox.host_commands` (command names)
- Optionally set `sandbox.host_user` for host-side audit labeling

Miclaw starts a host-side executor server on a Unix socket.
Inside the sandbox, Miclaw writes a tiny client launcher and command symlinks into PATH.
Each allowlisted command call is proxied to the host server over the mounted socket.

Example:

```json
{
  "sandbox": {
    "enabled": true,
    "host_user": "pipo-runner",
    "host_commands": ["git", "docker"]
  }
}
```

## 6. Security Model Notes

- No Docker socket mount.
- No implicit host filesystem access outside configured mounts.
- Host command access is explicit and allowlisted by command name.
