# Example Configurations

## Provider
- `backend`: `lmstudio`, `openrouter`, or `codex`.
- `base_url`: Optional. If omitted, defaults by backend.
- `api_key`: Required for `openrouter` and `codex`.
- `model`: Required model name/path.
- `max_tokens`: Optional, defaults to `8192`.

## Signal
- `enabled`: Turn Signal integration on/off.
- `account`: E.164 phone number when enabled.
- `http_host`, `http_port`, `cli_path`, `auto_start`: Signal daemon settings.
- `dm_policy`, `group_policy`: `allowlist`, `open`, or `disabled`.
- `allowlist`: Required when an allowlist policy is used.

## Webhook
- `enabled`: Turn webhook support on/off.
- `listen`: Address for webhook server.
- `hooks`: Array of webhook routes (`id`, `path`, `secret`, `format`).

## Memory
- `enabled`: Turn memory retrieval on/off.
- `embedding_url`: Embedding service endpoint.
- `embedding_model`: Embedding model.
- `embedding_api_key`: API key for the embedding service.
- `min_score`, `default_results`, `citations`: Scoring and output options.

## Sandbox
- `enabled`: Turn sandbox execution on/off.
- `network`: Sandbox network mode.
- `mounts`: Optional mount list (`host`, `container`, `mode`).
- `ssh_key_path`: Optional SSH key path.
- `host_user`: Host account used by sandbox jobs.

## Core
- `workspace`: Directory for workspace files.
- `state_path`: Directory for persisted state.
