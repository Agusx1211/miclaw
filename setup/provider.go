package setup

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/agusx1211/miclaw/config"
	"github.com/agusx1211/miclaw/provider"
)

func configureProvider(u *ui, p *config.ProviderConfig) error {
	u.section("Provider")
	backend, err := u.chooseOne("Provider backend", []string{"lmstudio", "openrouter", "codex"}, p.Backend)
	if err != nil {
		return err
	}
	p.Backend = backend
	base, err := u.askString("Provider base URL", pickBaseURL(backend, p.BaseURL))
	if err != nil {
		return err
	}
	p.BaseURL = base
	if err := configureProviderAuth(u, p); err != nil {
		return err
	}
	model, err := chooseModel(u, *p)
	if err != nil {
		return err
	}
	p.Model = model
	maxTokens, err := u.askInt("Max output tokens", maxInt(p.MaxTokens, 8192), 1)
	if err != nil {
		return err
	}
	p.MaxTokens = maxTokens
	return configureCodexExtras(u, p)
}

func configureProviderAuth(u *ui, p *config.ProviderConfig) error {
	if p.Backend == "lmstudio" {
		p.APIKey = "lmstudio"
		return nil
	}
	if p.Backend != "codex" {
		secret, err := u.askRequiredSecret("Provider API key", p.APIKey)
		if err != nil {
			return err
		}
		p.APIKey = secret
		return nil
	}
	mode, err := u.chooseOne("Codex auth mode", []string{"api_key", "oauth"}, codexAuthMode(p.APIKey))
	if err != nil {
		return err
	}
	if mode == "api_key" {
		secret, err := u.askRequiredSecret("OpenAI API key", p.APIKey)
		if err != nil {
			return err
		}
		p.APIKey = secret
		return nil
	}
	key, err := runCodexOAuthFlow(u)
	if err != nil {
		return err
	}
	p.APIKey = key
	return nil
}

func configureCodexExtras(u *ui, p *config.ProviderConfig) error {
	if p.Backend != "codex" {
		p.ThinkingEffort = ""
		p.Store = false
		return nil
	}
	effort, err := chooseThinkingEffort(u, p.ThinkingEffort)
	if err != nil {
		return err
	}
	p.ThinkingEffort = effort
	store, err := u.askBool("Enable provider-side conversation store", p.Store)
	if err != nil {
		return err
	}
	p.Store = store
	return nil
}

func runCodexOAuthFlow(u *ui) (string, error) {
	verifier, challenge, err := provider.GenerateOpenAICodexPKCE()
	if err != nil {
		return "", err
	}
	state, err := provider.GenerateOpenAICodexState()
	if err != nil {
		return "", err
	}
	authURL := provider.BuildOpenAICodexAuthorizeURL(provider.OpenAICodexRedirectURI, challenge, state)
	u.note("Open this URL in your browser, sign in, then paste the full redirect URL.")
	u.note(authURL)
	redirectURL, err := u.askRequiredString("Paste redirect URL", "")
	if err != nil {
		return "", err
	}
	code, err := provider.ParseOpenAICodexRedirectURL(redirectURL, state)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	tokens, err := provider.ExchangeOpenAICodexOAuthCode(ctx, code, verifier, provider.OpenAICodexRedirectURI)
	if err != nil {
		return "", err
	}
	if tokens.UsedAccessTokenAsKey {
		u.note("OpenAI did not return an exchangeable organization token; using OAuth access token directly.")
	}
	u.note("OAuth login complete.")
	return tokens.APIKey, nil
}

func chooseModel(u *ui, p config.ProviderConfig) (string, error) {
	auto, err := u.askBool("Auto-load available models", true)
	if err != nil {
		return "", err
	}
	if !auto {
		return u.askRequiredString("Model ID", p.Model)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	models, err := provider.DiscoverModelIDs(ctx, p)
	if err != nil || len(models) == 0 {
		u.note("Model discovery failed. Enter a model manually.")
		return u.askRequiredString("Model ID", p.Model)
	}
	return searchPickModel(u, models, p.Model)
}

func searchPickModel(u *ui, models []string, current string) (string, error) {
	for {
		query, err := u.askString("Search models (blank=all, /manual=manual input)", "")
		if err != nil {
			return "", err
		}
		if query == "/manual" {
			return u.askRequiredString("Model ID", current)
		}
		filtered := filterModels(models, query)
		if len(filtered) == 0 {
			u.note("No models matched.")
			continue
		}
		limit := minInt(len(filtered), 30)
		for i := 0; i < limit; i++ {
			fmt.Fprintf(u.out, "  %d. %s\n", i+1, filtered[i])
		}
		choice, err := u.askString("Choose number (Enter to search again)", "")
		if err != nil {
			return "", err
		}
		if choice == "" {
			continue
		}
		n, err := strconv.Atoi(choice)
		if err == nil && n >= 1 && n <= limit {
			return filtered[n-1], nil
		}
		u.note("Invalid number.")
	}
}

func filterModels(models []string, query string) []string {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return append([]string(nil), models...)
	}
	out := make([]string, 0, len(models))
	for _, m := range models {
		lm := strings.ToLower(m)
		if containsAll(lm, terms) {
			out = append(out, m)
		}
	}
	return out
}

func containsAll(value string, terms []string) bool {
	for _, term := range terms {
		if !strings.Contains(value, term) {
			return false
		}
	}
	return true
}

func chooseThinkingEffort(u *ui, current string) (string, error) {
	v, err := u.chooseOne("Thinking effort", []string{"none", "low", "medium", "high"}, thinkingChoice(current))
	if err != nil {
		return "", err
	}
	if v == "none" {
		return "", nil
	}
	return v, nil
}

func thinkingChoice(current string) string {
	if current == "" {
		return "none"
	}
	return current
}

func codexAuthMode(apiKey string) string {
	if apiKey == "" {
		return "oauth"
	}
	return "api_key"
}

func pickBaseURL(backend, current string) string {
	if current != "" {
		return current
	}
	def := provider.DefaultBaseURL(backend)
	if def != "" {
		return def
	}
	return current
}
