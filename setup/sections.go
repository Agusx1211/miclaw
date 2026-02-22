package setup

import "github.com/agusx1211/miclaw/config"

func configurePaths(u *ui, cfg *config.Config) error {
	u.section("Core Paths")
	workspace, err := u.askRequiredString("Workspace path", cfg.Workspace)
	if err != nil {
		return err
	}
	statePath, err := u.askRequiredString("State path", cfg.StatePath)
	if err != nil {
		return err
	}
	cfg.Workspace = workspace
	cfg.StatePath = statePath
	return nil
}

func configureSignal(u *ui, s *config.SignalConfig) error {
	u.section("Signal")
	enabled, err := u.askBool("Enable Signal integration", s.Enabled)
	if err != nil {
		return err
	}
	s.Enabled = enabled
	if !s.Enabled {
		return nil
	}
	if err := configureSignalBasics(u, s); err != nil {
		return err
	}
	return configureSignalPolicies(u, s)
}

func configureSignalBasics(u *ui, s *config.SignalConfig) error {
	account, err := u.askRequiredString("Signal account (+15551234567)", s.Account)
	if err != nil {
		return err
	}
	host, err := u.askRequiredString("Signal HTTP host", s.HTTPHost)
	if err != nil {
		return err
	}
	port, err := u.askInt("Signal HTTP port", maxInt(s.HTTPPort, 8080), 1)
	if err != nil {
		return err
	}
	cliPath, err := u.askRequiredString("signal-cli path", s.CLIPath)
	if err != nil {
		return err
	}
	autoStart, err := u.askBool("Auto-start signal-cli", s.AutoStart)
	if err != nil {
		return err
	}
	s.Account = account
	s.HTTPHost = host
	s.HTTPPort = port
	s.CLIPath = cliPath
	s.AutoStart = autoStart
	return nil
}

func configureSignalPolicies(u *ui, s *config.SignalConfig) error {
	dm, err := u.chooseOne("DM policy", []string{"allowlist", "open", "disabled"}, s.DMPolicy)
	if err != nil {
		return err
	}
	group, err := u.chooseOne("Group policy", []string{"allowlist", "open", "disabled"}, s.GroupPolicy)
	if err != nil {
		return err
	}
	s.DMPolicy = dm
	s.GroupPolicy = group
	if s.DMPolicy == "allowlist" || s.GroupPolicy == "allowlist" {
		allowlist, err := u.askCSV("Allowlist entries", s.Allowlist)
		if err != nil {
			return err
		}
		s.Allowlist = allowlist
	}
	textLimit, err := u.askInt("Text chunk limit", maxInt(s.TextChunkLimit, 4000), 1)
	if err != nil {
		return err
	}
	mediaMax, err := u.askInt("Max media size MB", maxInt(s.MediaMaxMB, 8), 1)
	if err != nil {
		return err
	}
	s.TextChunkLimit = textLimit
	s.MediaMaxMB = mediaMax
	return nil
}

func configureSandbox(u *ui, s *config.SandboxConfig) error {
	u.section("Docker Sandbox")
	enabled, err := u.askBool("Enable sandbox isolation", s.Enabled)
	if err != nil {
		return err
	}
	s.Enabled = enabled
	if !s.Enabled {
		return nil
	}
	network, err := chooseNetwork(u, s.Network)
	if err != nil {
		return err
	}
	hostUser, err := u.askRequiredString("Host user", s.HostUser)
	if err != nil {
		return err
	}
	sshKeyPath, err := u.askString("SSH key path (optional)", s.SSHKeyPath)
	if err != nil {
		return err
	}
	hostCommands, err := u.askCSV("Host command shims (optional, comma separated)", s.HostCommands)
	if err != nil {
		return err
	}
	mounts, err := configureMounts(u, s.Mounts)
	if err != nil {
		return err
	}
	s.Network = network
	s.HostUser = hostUser
	s.SSHKeyPath = sshKeyPath
	s.HostCommands = hostCommands
	s.Mounts = mounts
	return nil
}

func configureMounts(u *ui, current []config.Mount) ([]config.Mount, error) {
	count, err := u.askInt("Number of bind mounts", len(current), 0)
	if err != nil {
		return nil, err
	}
	out := make([]config.Mount, 0, count)
	for i := 0; i < count; i++ {
		existing := config.Mount{}
		if i < len(current) {
			existing = current[i]
		}
		host, err := u.askRequiredString("Mount host path", existing.Host)
		if err != nil {
			return nil, err
		}
		container, err := u.askRequiredString("Mount container path", existing.Container)
		if err != nil {
			return nil, err
		}
		mode, err := u.chooseOne("Mount mode", []string{"ro", "rw"}, chooseMode(existing.Mode))
		if err != nil {
			return nil, err
		}
		out = append(out, config.Mount{Host: host, Container: container, Mode: mode})
	}
	return out, nil
}

func configureMemory(u *ui, m *config.MemoryConfig) error {
	u.section("Memory")
	enabled, err := u.askBool("Enable memory retrieval", m.Enabled)
	if err != nil {
		return err
	}
	m.Enabled = enabled
	if !m.Enabled {
		return nil
	}
	if err := configureMemoryEndpoint(u, m); err != nil {
		return err
	}
	return configureMemoryTuning(u, m)
}

func configureMemoryEndpoint(u *ui, m *config.MemoryConfig) error {
	url, err := u.askRequiredString("Embedding API URL", m.EmbeddingURL)
	if err != nil {
		return err
	}
	model, err := u.askRequiredString("Embedding model", m.EmbeddingModel)
	if err != nil {
		return err
	}
	secret, err := u.askRequiredSecret("Embedding API key", m.EmbeddingAPIKey)
	if err != nil {
		return err
	}
	m.EmbeddingURL = url
	m.EmbeddingModel = model
	m.EmbeddingAPIKey = secret
	return nil
}

func configureMemoryTuning(u *ui, m *config.MemoryConfig) error {
	minScore, err := u.askFloat("Minimum score", clamp(m.MinScore, 0.35), 0, 1)
	if err != nil {
		return err
	}
	defaultResults, err := u.askInt("Default result count", maxInt(m.DefaultResults, 6), 1)
	if err != nil {
		return err
	}
	citations, err := u.chooseOne("Citations mode", []string{"on", "off", "auto"}, m.Citations)
	if err != nil {
		return err
	}
	m.MinScore = minScore
	m.DefaultResults = defaultResults
	m.Citations = citations
	return nil
}

func configureWebhook(u *ui, w *config.WebhookConfig) error {
	u.section("Webhooks")
	enabled, err := u.askBool("Enable webhooks", w.Enabled)
	if err != nil {
		return err
	}
	w.Enabled = enabled
	if !w.Enabled {
		return nil
	}
	listen, err := u.askRequiredString("Webhook listen address", w.Listen)
	if err != nil {
		return err
	}
	count, err := u.askInt("Number of webhook routes", len(w.Hooks), 1)
	if err != nil {
		return err
	}
	hooks, err := configureHooks(u, w.Hooks, count)
	if err != nil {
		return err
	}
	w.Listen = listen
	w.Hooks = hooks
	return nil
}

func configureHooks(u *ui, current []config.WebhookDef, count int) ([]config.WebhookDef, error) {
	hooks := make([]config.WebhookDef, 0, count)
	for i := 0; i < count; i++ {
		existing := config.WebhookDef{}
		if i < len(current) {
			existing = current[i]
		}
		id, err := u.askRequiredString("Hook ID", existing.ID)
		if err != nil {
			return nil, err
		}
		path, err := u.askRequiredString("Hook path (/hook)", existing.Path)
		if err != nil {
			return nil, err
		}
		secret, _, err := u.askSecret("Hook secret (optional)", existing.Secret != "")
		if err != nil {
			return nil, err
		}
		format, err := u.chooseOne("Hook payload format", []string{"text", "json"}, chooseFormat(existing.Format))
		if err != nil {
			return nil, err
		}
		h := config.WebhookDef{ID: id, Path: path, Secret: existing.Secret, Format: format}
		if secret != "" {
			h.Secret = secret
		}
		hooks = append(hooks, h)
	}
	return hooks, nil
}

func chooseNetwork(u *ui, current string) (string, error) {
	mode, err := u.chooseOne("Sandbox network", []string{"none", "host", "bridge", "custom"}, chooseNetworkMode(current))
	if err != nil {
		return "", err
	}
	if mode != "custom" {
		return mode, nil
	}
	return u.askRequiredString("Custom Docker network name", current)
}

func chooseNetworkMode(current string) string {
	switch current {
	case "none", "host", "bridge":
		return current
	case "":
		return "none"
	default:
		return "custom"
	}
}

func chooseFormat(v string) string {
	if v == "" {
		return "text"
	}
	return v
}

func chooseMode(v string) string {
	if v == "" {
		return "ro"
	}
	return v
}

func maxInt(v, def int) int {
	if v > 0 {
		return v
	}
	return def
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func clamp(v, def float64) float64 {
	if v < 0 || v > 1 {
		return def
	}
	return v
}
