package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfigFile(t *testing.T, body string) string {
	t.Helper()
	d := t.TempDir()
	p := filepath.Join(d, "config.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return p
}

func TestLoadAcceptsValidFullConfig(t *testing.T) {
	p := writeConfigFile(t, `{
		"provider": {
			"backend": "openrouter",
			"base_url": "https://openrouter.ai/api/v1",
			"api_key": "sk-or-test",
			"model": "anthropic/claude-sonnet-4-5",
			"max_tokens": 4096
		},
		"signal": {
			"enabled": true,
			"account": "+15551234567",
			"http_host": "127.0.0.1",
			"http_port": 8080,
			"cli_path": "signal-cli",
			"auto_start": true,
			"dm_policy": "allowlist",
			"group_policy": "open",
			"allowlist": ["+15557654321"],
			"text_chunk_limit": 3000,
			"media_max_mb": 10
		},
		"webhook": {
			"enabled": true,
			"listen": "127.0.0.1:9191",
			"hooks": [
				{
					"id": "deploy",
					"path": "/hook/deploy",
					"secret": "whsec_test",
					"format": "json"
				}
			]
		},
		"sandbox": {
			"enabled": true,
			"network": "bridge",
			"mounts": [
				{"host": "/tmp", "container": "/workspace", "mode": "rw"}
			],
			"host_user": "runner"
		},
		"memory": {
			"enabled": true,
			"embedding_url": "https://embed.local/v1",
			"embedding_model": "text-embed-v1",
			"embedding_api_key": "emb-key",
			"min_score": 0.5,
			"default_results": 7,
			"citations": "on"
		},
		"workspace": "/tmp/workspace",
		"state_path": "/tmp/state"
	}`)

	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if c.Provider.Backend != "openrouter" {
		t.Fatalf("unexpected backend: %q", c.Provider.Backend)
	}
	if c.Signal.Account != "+15551234567" {
		t.Fatalf("unexpected account: %q", c.Signal.Account)
	}
	if c.Webhook.Hooks[0].Path != "/hook/deploy" {
		t.Fatalf("unexpected hook path: %q", c.Webhook.Hooks[0].Path)
	}
	if c.Sandbox.Mounts[0].Mode != "rw" {
		t.Fatalf("unexpected mount mode: %q", c.Sandbox.Mounts[0].Mode)
	}
	if c.Memory.Citations != "on" {
		t.Fatalf("unexpected citations mode: %q", c.Memory.Citations)
	}
}

func TestLoadRejectsMissingFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "missing.json")
	_, err := Load(p)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "read config") {
		t.Fatalf("expected read config error, got: %v", err)
	}
}

func TestLoadRejectsInvalidJSON(t *testing.T) {
	p := writeConfigFile(t, `{"provider":`) // invalid JSON
	_, err := Load(p)
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "parse config") {
		t.Fatalf("expected parse config error, got: %v", err)
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	p := writeConfigFile(t, `{
		"provider": {
			"backend": "lmstudio",
			"model": "local-model"
		}
	}`)

	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	h, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("resolve home: %v", err)
	}
	if c.Workspace != filepath.Join(h, ".miclaw/workspace") {
		t.Fatalf("unexpected workspace default: %q", c.Workspace)
	}
	if c.StatePath != filepath.Join(h, ".miclaw/state") {
		t.Fatalf("unexpected state path default: %q", c.StatePath)
	}
	if c.Provider.BaseURL != defaultLMStudioURL {
		t.Fatalf("unexpected provider base url: %q", c.Provider.BaseURL)
	}
	if c.Signal.TextChunkLimit != defaultTextChunkLimit || c.Signal.MediaMaxMB != defaultMediaMaxMB {
		t.Fatalf("unexpected signal defaults: %d %d", c.Signal.TextChunkLimit, c.Signal.MediaMaxMB)
	}
	if c.Webhook.Listen != defaultWebhookListen {
		t.Fatalf("unexpected webhook listen default: %q", c.Webhook.Listen)
	}
	if c.Sandbox.HostUser != defaultHostUser {
		t.Fatalf("unexpected sandbox host user default: %q", c.Sandbox.HostUser)
	}
	if c.Memory.MinScore != defaultMinScore || c.Memory.DefaultResults != defaultResults || c.Memory.Citations != defaultCitations {
		t.Fatalf("unexpected memory defaults: %v %d %q", c.Memory.MinScore, c.Memory.DefaultResults, c.Memory.Citations)
	}
}

func TestLoadRejectsInvalidBackend(t *testing.T) {
	p := writeConfigFile(t, `{
		"provider": {
			"backend": "invalid",
			"model": "m"
		}
	}`)

	_, err := Load(p)
	if err == nil {
		t.Fatal("expected invalid backend error")
	}
	if !strings.Contains(err.Error(), "provider.backend") {
		t.Fatalf("expected provider.backend error, got: %v", err)
	}
}

func TestLoadRejectsInvalidSignalE164(t *testing.T) {
	p := writeConfigFile(t, `{
		"provider": {
			"backend": "lmstudio",
			"model": "m"
		},
		"signal": {
			"enabled": true,
			"account": "+12abc"
		}
	}`)

	_, err := Load(p)
	if err == nil {
		t.Fatal("expected invalid E.164 error")
	}
	if !strings.Contains(err.Error(), "signal.account") {
		t.Fatalf("expected signal.account error, got: %v", err)
	}
}

func TestLoadRejectsWebhookPathWithoutLeadingSlash(t *testing.T) {
	p := writeConfigFile(t, `{
		"provider": {
			"backend": "lmstudio",
			"model": "m"
		},
		"webhook": {
			"enabled": true,
			"hooks": [
				{"id": "x", "path": "hook/deploy", "format": "text"}
			]
		}
	}`)

	_, err := Load(p)
	if err == nil {
		t.Fatal("expected webhook path validation error")
	}
	if !strings.Contains(err.Error(), "webhook.hooks[0].path") {
		t.Fatalf("expected webhook path error, got: %v", err)
	}
}

func TestLoadRejectsMissingRequiredFieldsWhenEnabled(t *testing.T) {
	p := writeConfigFile(t, `{
		"provider": {
			"backend": "lmstudio",
			"model": "m"
		},
		"memory": {
			"enabled": true,
			"embedding_url": "http://127.0.0.1:1234/v1"
		}
	}`)

	_, err := Load(p)
	if err == nil {
		t.Fatal("expected missing required field error")
	}
	if !strings.Contains(err.Error(), "memory.embedding_model") {
		t.Fatalf("expected embedding_model error, got: %v", err)
	}
}

func TestLoadExpandsTildePaths(t *testing.T) {
	p := writeConfigFile(t, `{
		"provider": {
			"backend": "lmstudio",
			"model": "m"
		},
		"workspace": "~/.miclaw/custom-workspace",
		"state_path": "~/.miclaw/custom-state"
	}`)

	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	h, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("resolve home: %v", err)
	}
	if c.Workspace != filepath.Join(h, ".miclaw/custom-workspace") {
		t.Fatalf("unexpected workspace expansion: %q", c.Workspace)
	}
	if c.StatePath != filepath.Join(h, ".miclaw/custom-state") {
		t.Fatalf("unexpected state_path expansion: %q", c.StatePath)
	}
}

func TestLoadAcceptsValidThinkingEffort(t *testing.T) {
	p := writeConfigFile(t, `{
		"provider": {
			"backend": "codex",
			"api_key": "sk-test",
			"model": "codex-mini-latest",
			"thinking_effort": "medium"
		}
	}`)

	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if c.Provider.ThinkingEffort != "medium" {
		t.Fatalf("unexpected thinking_effort: %q", c.Provider.ThinkingEffort)
	}
}

func TestLoadRejectsInvalidThinkingEffort(t *testing.T) {
	p := writeConfigFile(t, `{
		"provider": {
			"backend": "codex",
			"api_key": "sk-test",
			"model": "codex-mini-latest",
			"thinking_effort": "extreme"
		}
	}`)

	_, err := Load(p)
	if err == nil {
		t.Fatal("expected invalid thinking_effort error")
	}
	if !strings.Contains(err.Error(), "provider.thinking_effort") {
		t.Fatalf("expected provider.thinking_effort error, got: %v", err)
	}
}

func TestLoadAcceptsSandboxHostCommandsWithoutKeyPath(t *testing.T) {
	p := writeConfigFile(t, `{
		"provider": {
			"backend": "lmstudio",
			"model": "m"
		},
		"sandbox": {
			"enabled": true,
			"host_commands": ["git"]
		}
	}`)

	c, err := Load(p)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(c.Sandbox.HostCommands) != 1 || c.Sandbox.HostCommands[0] != "git" {
		t.Fatalf("unexpected host commands: %#v", c.Sandbox.HostCommands)
	}
}

func TestLoadRejectsSandboxHostCommandsWithSpaces(t *testing.T) {
	p := writeConfigFile(t, `{
		"provider": {
			"backend": "lmstudio",
			"model": "m"
		},
		"sandbox": {
			"enabled": true,
			"host_commands": ["git status"]
		}
	}`)

	_, err := Load(p)
	if err == nil {
		t.Fatal("expected sandbox host command name validation error")
	}
	if !strings.Contains(err.Error(), "sandbox.host_commands[0]") {
		t.Fatalf("expected sandbox.host_commands[0] error, got: %v", err)
	}
}
