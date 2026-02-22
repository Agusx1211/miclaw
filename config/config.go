package config

// Config is the complete runtime configuration loaded from one JSON file.
type Config struct {
	Provider  ProviderConfig `json:"provider"`
	Signal    SignalConfig   `json:"signal"`
	Webhook   WebhookConfig  `json:"webhook"`
	Sandbox   SandboxConfig  `json:"sandbox"`
	Memory    MemoryConfig   `json:"memory"`
	Workspace string         `json:"workspace"`
	StatePath string         `json:"state_path"`
}

type ProviderConfig struct {
	Backend        string `json:"backend"`
	BaseURL        string `json:"base_url"`
	APIKey         string `json:"api_key"`
	Model          string `json:"model"`
	MaxTokens      int    `json:"max_tokens"`
	ThinkingEffort string `json:"thinking_effort"`
	Store          bool   `json:"store"`
}

type SignalConfig struct {
	Enabled        bool     `json:"enabled"`
	Account        string   `json:"account"`
	HTTPHost       string   `json:"http_host"`
	HTTPPort       int      `json:"http_port"`
	CLIPath        string   `json:"cli_path"`
	AutoStart      bool     `json:"auto_start"`
	DMPolicy       string   `json:"dm_policy"`
	GroupPolicy    string   `json:"group_policy"`
	Allowlist      []string `json:"allowlist"`
	TextChunkLimit int      `json:"text_chunk_limit"`
	MediaMaxMB     int      `json:"media_max_mb"`
}

type WebhookConfig struct {
	Enabled bool         `json:"enabled"`
	Listen  string       `json:"listen"`
	Hooks   []WebhookDef `json:"hooks"`
}

type WebhookDef struct {
	ID     string `json:"id"`
	Path   string `json:"path"`
	Secret string `json:"secret"`
	Format string `json:"format"`
}

type SandboxConfig struct {
	Enabled      bool     `json:"enabled"`
	Network      string   `json:"network"`
	Mounts       []Mount  `json:"mounts"`
	HostUser     string   `json:"host_user"`
	HostCommands []string `json:"host_commands"`
}

type Mount struct {
	Host      string `json:"host"`
	Container string `json:"container"`
	Mode      string `json:"mode"`
}

type MemoryConfig struct {
	Enabled         bool    `json:"enabled"`
	EmbeddingURL    string  `json:"embedding_url"`
	EmbeddingModel  string  `json:"embedding_model"`
	EmbeddingAPIKey string  `json:"embedding_api_key"`
	MinScore        float64 `json:"min_score"`
	DefaultResults  int     `json:"default_results"`
	Citations       string  `json:"citations"`
}
