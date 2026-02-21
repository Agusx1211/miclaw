package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadExampleConfigs(t *testing.T) {
	tmp := t.TempDir()
	workspace := filepath.Join(tmp, "workspace")
	state := filepath.Join(tmp, "state")

	localPath, err := writeExampleConfig(t, tmp, "config-local.json", workspace, state)
	if err != nil {
		t.Fatalf("write local example config: %v", err)
	}
	localCfg, err := Load(localPath)
	if err != nil {
		t.Fatalf("Load local example failed: %v", err)
	}
	if localCfg.Provider.Backend != "lmstudio" {
		t.Fatalf("unexpected local backend: %q", localCfg.Provider.Backend)
	}
	if localCfg.Provider.BaseURL != defaultLMStudioURL {
		t.Fatalf("unexpected local base_url: %q", localCfg.Provider.BaseURL)
	}
	if localCfg.Provider.APIKey != "lmstudio" {
		t.Fatalf("unexpected local api_key: %q", localCfg.Provider.APIKey)
	}
	if localCfg.Workspace != workspace {
		t.Fatalf("unexpected local workspace: %q", localCfg.Workspace)
	}
	if localCfg.StatePath != state {
		t.Fatalf("unexpected local state_path: %q", localCfg.StatePath)
	}

	cloudPath, err := writeExampleConfig(t, tmp, "config-cloud.json", workspace, state)
	if err != nil {
		t.Fatalf("write cloud example config: %v", err)
	}
	cloudCfg, err := Load(cloudPath)
	if err != nil {
		t.Fatalf("Load cloud example failed: %v", err)
	}
	if cloudCfg.Provider.Backend != "openrouter" {
		t.Fatalf("unexpected cloud backend: %q", cloudCfg.Provider.Backend)
	}
	if cloudCfg.Provider.BaseURL != defaultOpenRouterURL {
		t.Fatalf("unexpected cloud base_url: %q", cloudCfg.Provider.BaseURL)
	}
	if cloudCfg.Signal.Account != "+1234567890" {
		t.Fatalf("unexpected cloud signal account: %q", cloudCfg.Signal.Account)
	}
	if cloudCfg.Webhook.Listen != defaultWebhookListen {
		t.Fatalf("unexpected cloud webhook listen default: %q", cloudCfg.Webhook.Listen)
	}
	if cloudCfg.Memory.Citations != defaultCitations {
		t.Fatalf("unexpected cloud memory citations default: %q", cloudCfg.Memory.Citations)
	}
}

func writeExampleConfig(t *testing.T, dir, name, workspace, state string) (string, error) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "examples", name))
	if err != nil {
		return "", err
	}
	cfg := strings.ReplaceAll(string(body), "~/.miclaw/workspace", workspace)
	cfg = strings.ReplaceAll(cfg, "~/.miclaw/state", state)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		return "", err
	}
	return path, nil
}
