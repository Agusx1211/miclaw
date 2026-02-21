package provider

import (
	"bytes"
	"context"
	"net/http"
	"strings"

	"github.com/agusx1211/miclaw/config"
	"github.com/agusx1211/miclaw/model"
)

const lmStudioDefaultBaseURL = "http://127.0.0.1:1234/v1"

type LMStudio struct {
	baseURL   string
	apiKey    string
	model     string
	maxTokens int
	client    *http.Client
}

func NewLMStudio(cfg config.ProviderConfig) *LMStudio {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		base = lmStudioDefaultBaseURL
	}
	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}
	return &LMStudio{
		baseURL:   base,
		apiKey:    cfg.APIKey,
		model:     cfg.Model,
		maxTokens: maxTokens,
		client:    &http.Client{},
	}
}

func (l *LMStudio) Model() ModelInfo {
	return ModelInfo{
		ID:                 l.model,
		Name:               l.model,
		MaxOutput:          l.maxTokens,
		CostPerInputToken:  0,
		CostPerOutputToken: 0,
	}
}

func (l *LMStudio) Stream(ctx context.Context, messages []model.Message, tools []ToolDef) <-chan ProviderEvent {
	out := make(chan ProviderEvent, 16)
	go l.stream(ctx, messages, tools, out)
	return out
}

func (l *LMStudio) stream(ctx context.Context, messages []model.Message, tools []ToolDef, out chan<- ProviderEvent) {
	defer close(out)
	payload, err := marshalRequest(l.model, l.maxTokens, messages, tools)
	if err != nil {
		out <- errorEvent(err)
		return
	}
	resp, err := l.post(ctx, payload)
	if err != nil {
		out <- errorEvent(err)
		return
	}
	if resp.StatusCode != http.StatusOK {
		out <- errorEvent(readStatusError("lmstudio", resp))
		return
	}
	for e := range parseSSEStream(resp.Body) {
		out <- e
	}
}

func (l *LMStudio) post(ctx context.Context, payload []byte) (*http.Response, error) {
	return withRetry(ctx, 0, func() (*http.Response, error) {
		return l.doPost(ctx, payload)
	})
}

func (l *LMStudio) doPost(ctx context.Context, payload []byte) (*http.Response, error) {
	u := l.baseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+l.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	return l.client.Do(req)
}
