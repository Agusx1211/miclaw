package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/agusx1211/miclaw/config"
)

type modelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

func DiscoverModelIDs(ctx context.Context, cfg config.ProviderConfig) ([]string, error) {
	u, err := modelsURL(cfg)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	applyModelHeaders(req, cfg)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, readModelsStatus(resp)
	}
	var body modelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	return uniqueSortedModelIDs(body), nil
}

func modelsURL(cfg config.ProviderConfig) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		switch cfg.Backend {
		case "lmstudio":
			base = lmStudioDefaultBaseURL
		case "openrouter":
			base = openRouterDefaultBaseURL
		case "codex":
			base = codexDefaultBaseURL
		default:
			return "", fmt.Errorf("unknown backend %q", cfg.Backend)
		}
	}
	return base + "/models", nil
}

func applyModelHeaders(req *http.Request, cfg config.ProviderConfig) {
	if cfg.Backend == "openrouter" || cfg.Backend == "codex" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	if cfg.Backend == "openrouter" {
		req.Header.Set("HTTP-Referer", openRouterReferer)
		req.Header.Set("X-Title", openRouterTitle)
	}
}

func readModelsStatus(resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 16*1024))
	msg := strings.TrimSpace(string(b))
	if msg == "" {
		return fmt.Errorf("models list failed: status %d", resp.StatusCode)
	}
	return fmt.Errorf("models list failed: status %d: %s", resp.StatusCode, msg)
}

func uniqueSortedModelIDs(body modelsResponse) []string {
	set := map[string]bool{}
	for _, m := range body.Data {
		if m.ID != "" {
			set[m.ID] = true
		}
	}
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
