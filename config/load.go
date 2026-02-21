package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	defaultWorkspace      = "~/.miclaw/workspace"
	defaultStatePath      = "~/.miclaw/state"
	defaultLMStudioURL    = "http://127.0.0.1:1234/v1"
	defaultOpenRouterURL  = "https://openrouter.ai/api/v1"
	defaultCodexURL       = "https://api.openai.com/v1"
	defaultMaxTokens      = 8192
	defaultSignalHTTPHost = "127.0.0.1"
	defaultSignalHTTPPort = 8080
	defaultSignalCLIPath  = "signal-cli"
	defaultDMPolicy       = "open"
	defaultGroupPolicy    = "disabled"
	defaultTextChunkLimit = 4000
	defaultMediaMaxMB     = 8
	defaultWebhookListen  = "127.0.0.1:9090"
	defaultSandboxNetwork = "none"
	defaultHostUser       = "pipo-runner"
	defaultMinScore       = 0.35
	defaultResults        = 6
	defaultCitations      = "auto"
)

func Load(path string) (*Config, error) {

	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %v", path, err)
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("parse config %q: %v", path, err)
	}

	applyDefaults(&c)
	if err := expandPaths(&c); err != nil {
		return nil, err
	}
	if err := validate(c); err != nil {
		return nil, err
	}

	return &c, nil
}

func applyDefaults(c *Config) {

	applyCoreDefaults(c)
	applyProviderDefaults(&c.Provider)
	applySignalDefaults(&c.Signal)
	applyWebhookDefaults(&c.Webhook)
	applySandboxDefaults(&c.Sandbox)
	applyMemoryDefaults(&c.Memory)

}

func applyCoreDefaults(c *Config) {

	if c.Workspace == "" {
		c.Workspace = defaultWorkspace
	}
	if c.StatePath == "" {
		c.StatePath = defaultStatePath
	}

}

func applyProviderDefaults(p *ProviderConfig) {

	if p.BaseURL == "" {
		switch p.Backend {
		case "lmstudio":
			p.BaseURL = defaultLMStudioURL
		case "openrouter":
			p.BaseURL = defaultOpenRouterURL
		case "codex":
			p.BaseURL = defaultCodexURL
		}
	}
	if p.MaxTokens == 0 {
		p.MaxTokens = defaultMaxTokens
	}
	if p.Backend == "lmstudio" && p.APIKey == "" {
		p.APIKey = "lmstudio"
	}

}

func applySignalDefaults(s *SignalConfig) {

	if s.HTTPHost == "" {
		s.HTTPHost = defaultSignalHTTPHost
	}
	if s.HTTPPort == 0 {
		s.HTTPPort = defaultSignalHTTPPort
	}
	if s.CLIPath == "" {
		s.CLIPath = defaultSignalCLIPath
	}
	if s.DMPolicy == "" {
		s.DMPolicy = defaultDMPolicy
	}
	if s.GroupPolicy == "" {
		s.GroupPolicy = defaultGroupPolicy
	}
	if s.TextChunkLimit == 0 {
		s.TextChunkLimit = defaultTextChunkLimit
	}
	if s.MediaMaxMB == 0 {
		s.MediaMaxMB = defaultMediaMaxMB
	}

}

func applyWebhookDefaults(w *WebhookConfig) {

	if w.Listen == "" {
		w.Listen = defaultWebhookListen
	}
	for i := range w.Hooks {
		if w.Hooks[i].Format == "" {
			w.Hooks[i].Format = "text"
		}
	}

}

func applySandboxDefaults(s *SandboxConfig) {

	if s.Network == "" {
		s.Network = defaultSandboxNetwork
	}
	if s.HostUser == "" {
		s.HostUser = defaultHostUser
	}
	for i := range s.Mounts {
		if s.Mounts[i].Mode == "" {
			s.Mounts[i].Mode = "ro"
		}
	}

}

func applyMemoryDefaults(m *MemoryConfig) {

	if m.MinScore == 0 {
		m.MinScore = defaultMinScore
	}
	if m.DefaultResults == 0 {
		m.DefaultResults = defaultResults
	}
	if m.Citations == "" {
		m.Citations = defaultCitations
	}

}

func expandPaths(c *Config) error {

	w, err := expandHome(c.Workspace)
	if err != nil {
		return fmt.Errorf("workspace: %v", err)
	}
	s, err := expandHome(c.StatePath)
	if err != nil {
		return fmt.Errorf("state_path: %v", err)
	}
	c.Workspace = w
	c.StatePath = s

	return nil
}

func expandHome(p string) (string, error) {

	if p[0] != '~' {
		return p, nil
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %v", err)
	}
	if p == "~" {

		return h, nil
	}
	if strings.HasPrefix(p, "~/") {
		o := filepath.Join(h, p[2:])

		return o, nil
	}
	return "", fmt.Errorf("unsupported home path %q", p)
}

func validate(c Config) error {

	if err := validateProvider(c.Provider); err != nil {
		return err
	}
	if err := validateSignal(c.Signal); err != nil {
		return err
	}
	if err := validateWebhooks(c.Webhook); err != nil {
		return err
	}
	if err := validateSandbox(c.Sandbox); err != nil {
		return err
	}
	if err := validateMemory(c.Memory); err != nil {
		return err
	}

	return nil
}

func validateProvider(p ProviderConfig) error {
	v := map[string]bool{"lmstudio": true, "openrouter": true, "codex": true}
	e := map[string]bool{"low": true, "medium": true, "high": true}

	if !v[p.Backend] {
		return fmt.Errorf("provider.backend must be one of lmstudio, openrouter, codex")
	}
	if p.BaseURL == "" {
		return fmt.Errorf("provider.base_url is required")
	}
	if p.Model == "" {
		return fmt.Errorf("provider.model is required")
	}
	if p.MaxTokens <= 0 {
		return fmt.Errorf("provider.max_tokens must be greater than zero")
	}
	if (p.Backend == "openrouter" || p.Backend == "codex") && p.APIKey == "" {
		return fmt.Errorf("provider.api_key is required for backend %q", p.Backend)
	}
	if p.ThinkingEffort != "" && !e[p.ThinkingEffort] {
		return fmt.Errorf("provider.thinking_effort must be one of low, medium, high")
	}
	return nil
}

func validateSignal(s SignalConfig) error {
	v := map[string]bool{"allowlist": true, "open": true, "disabled": true}

	if !s.Enabled {
		return nil
	}
	if s.Account == "" {
		return fmt.Errorf("signal.account is required when signal.enabled=true")
	}
	if len(s.Account) < 8 || len(s.Account) > 16 || s.Account[0] != '+' {
		return fmt.Errorf("signal.account must be valid E.164")
	}
	for i := 1; i < len(s.Account); i++ {
		if s.Account[i] < '0' || s.Account[i] > '9' {
			return fmt.Errorf("signal.account must be valid E.164")
		}
	}
	if s.HTTPHost == "" || s.HTTPPort <= 0 || s.CLIPath == "" {
		return fmt.Errorf("signal.http_host, signal.http_port, and signal.cli_path are required when signal.enabled=true")
	}
	if !v[s.DMPolicy] {
		return fmt.Errorf("signal.dm_policy must be one of allowlist, open, disabled")
	}
	if !v[s.GroupPolicy] {
		return fmt.Errorf("signal.group_policy must be one of allowlist, open, disabled")
	}
	if (s.DMPolicy == "allowlist" || s.GroupPolicy == "allowlist") && len(s.Allowlist) == 0 {
		return fmt.Errorf("signal.allowlist is required when a signal policy is allowlist")
	}
	if s.TextChunkLimit <= 0 || s.MediaMaxMB <= 0 {
		return fmt.Errorf("signal.text_chunk_limit and signal.media_max_mb must be greater than zero")
	}
	return nil
}

func validateWebhooks(w WebhookConfig) error {
	v := map[string]bool{"text": true, "json": true}

	if !w.Enabled {
		return nil
	}
	if w.Listen == "" {
		return fmt.Errorf("webhook.listen is required when webhook.enabled=true")
	}
	if len(w.Hooks) == 0 {
		return fmt.Errorf("webhook.hooks is required when webhook.enabled=true")
	}
	for i, h := range w.Hooks {
		if h.ID == "" {
			return fmt.Errorf("webhook.hooks[%d].id is required", i)
		}
		if h.Path == "" {
			return fmt.Errorf("webhook.hooks[%d].path is required", i)
		}
		if !strings.HasPrefix(h.Path, "/") {
			return fmt.Errorf("webhook.hooks[%d].path must start with /", i)
		}
		if !v[h.Format] {
			return fmt.Errorf("webhook.hooks[%d].format must be text or json", i)
		}
	}
	return nil
}

func validateSandbox(s SandboxConfig) error {
	v := map[string]bool{"ro": true, "rw": true}

	if !s.Enabled {
		return nil
	}
	if s.Network == "" {
		return fmt.Errorf("sandbox.network is required when sandbox.enabled=true")
	}
	if s.HostUser == "" {
		return fmt.Errorf("sandbox.host_user is required when sandbox.enabled=true")
	}
	for i, m := range s.Mounts {
		if m.Host == "" {
			return fmt.Errorf("sandbox.mounts[%d].host is required", i)
		}
		if m.Container == "" {
			return fmt.Errorf("sandbox.mounts[%d].container is required", i)
		}
		if !v[m.Mode] {
			return fmt.Errorf("sandbox.mounts[%d].mode must be ro or rw", i)
		}
	}
	if s.SSHKeyPath != "" {
		if _, err := expandHome(s.SSHKeyPath); err != nil {
			return fmt.Errorf("sandbox.ssh_key_path: %v", err)
		}
	}
	return nil
}

func validateMemory(m MemoryConfig) error {
	v := map[string]bool{"on": true, "off": true, "auto": true}

	if !m.Enabled {
		return nil
	}
	if m.EmbeddingURL == "" {
		return fmt.Errorf("memory.embedding_url is required when memory.enabled=true")
	}
	if m.EmbeddingModel == "" {
		return fmt.Errorf("memory.embedding_model is required when memory.enabled=true")
	}
	if m.MinScore < 0 || m.MinScore > 1 {
		return fmt.Errorf("memory.min_score must be between 0 and 1")
	}
	if m.DefaultResults <= 0 {
		return fmt.Errorf("memory.default_results must be greater than zero")
	}
	if !v[m.Citations] {
		return fmt.Errorf("memory.citations must be one of on, off, auto")
	}
	return nil
}
