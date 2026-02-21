package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/agusx1211/miclaw/config"
	"github.com/agusx1211/miclaw/model"
)

const codexDefaultBaseURL = "https://api.openai.com/v1"
const codexChatGPTBaseURL = "https://chatgpt.com/backend-api/codex"

type Codex struct {
	baseURL        string
	apiKey         string
	chatgptAccount string
	useResponses   bool
	model          string
	maxTokens      int
	thinkingEffort string
	store          bool
	client         *http.Client
}

type codexRequest struct {
	Model           string              `json:"model"`
	Messages        []openRouterMessage `json:"messages"`
	Tools           []openRouterTool    `json:"tools,omitempty"`
	Stream          bool                `json:"stream"`
	MaxOutputTokens int                 `json:"max_output_tokens"`
	Store           bool                `json:"store"`
	Reasoning       *codexReasoning     `json:"reasoning,omitempty"`
}

type codexReasoning struct {
	Effort string `json:"effort"`
}

func NewCodex(cfg config.ProviderConfig) *Codex {

	accountID := chatgptAccountID(cfg.APIKey)
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		if accountID != "" {
			base = codexChatGPTBaseURL
		} else {
			base = codexDefaultBaseURL
		}
	}
	if accountID != "" && strings.HasPrefix(base, codexDefaultBaseURL) {
		base = codexChatGPTBaseURL
	}
	useResponses := strings.Contains(base, "/backend-api/codex")
	if base == "" {
		base = codexDefaultBaseURL
	}
	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}
	p := &Codex{
		baseURL:        base,
		apiKey:         cfg.APIKey,
		chatgptAccount: accountID,
		useResponses:   useResponses,
		model:          cfg.Model,
		maxTokens:      maxTokens,
		thinkingEffort: strings.TrimSpace(cfg.ThinkingEffort),
		store:          cfg.Store,
		client:         &http.Client{},
	}

	return p
}

func (c *Codex) Model() ModelInfo {

	info := ModelInfo{
		ID:        c.model,
		Name:      c.model,
		MaxOutput: c.maxTokens,
	}

	return info
}

func (c *Codex) Stream(ctx context.Context, messages []model.Message, tools []ToolDef) <-chan ProviderEvent {

	out := make(chan ProviderEvent, 16)

	go c.stream(ctx, messages, tools, out)
	return out
}

func (c *Codex) stream(ctx context.Context, messages []model.Message, tools []ToolDef, out chan<- ProviderEvent) {

	defer close(out)
	payload, path, err := c.marshalRequest(messages, tools)
	if err != nil {
		out <- errorEvent(err)
		return
	}
	resp, err := c.postPath(ctx, path, payload)
	if err != nil {
		out <- errorEvent(err)
		return
	}
	if resp.StatusCode != http.StatusOK {
		out <- errorEvent(readStatusError("codex", resp))
		return
	}
	for e := range parseSSEStream(resp.Body) {
		out <- normalizeCodexToolCallID(e)
	}
}

func (c *Codex) marshalRequest(messages []model.Message, tools []ToolDef) ([]byte, string, error) {
	if c.useResponses {
		payload, err := marshalCodexResponsesRequest(c.model, c.thinkingEffort, messages, tools)
		return payload, "/responses", err
	}
	payload, err := marshalCodexRequest(c.model, c.maxTokens, c.thinkingEffort, c.store, messages, tools)
	return payload, "/chat/completions", err
}

func marshalCodexRequest(modelID string, maxTokens int, effort string, store bool, messages []model.Message, tools []ToolDef) ([]byte, error) {

	body := codexRequest{
		Model:           modelID,
		Messages:        encodeMessages(messages),
		Tools:           encodeTools(tools),
		Stream:          true,
		MaxOutputTokens: maxTokens,
		Store:           store,
	}
	if effort != "" {
		body.Reasoning = &codexReasoning{Effort: effort}
	}

	return json.Marshal(body)
}

func (c *Codex) postPath(ctx context.Context, path string, payload []byte) (*http.Response, error) {

	return withRetry(ctx, 0, func() (*http.Response, error) {

		return c.doPostPath(ctx, path, payload)
	})
}

func (c *Codex) doPostPath(ctx context.Context, path string, payload []byte) (*http.Response, error) {

	u := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if c.chatgptAccount != "" {
		req.Header.Set("ChatGPT-Account-ID", c.chatgptAccount)
		req.Header.Set("originator", "codex_cli_rs")
	}
	r, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}

	return r, nil
}

func normalizeCodexToolCallID(e ProviderEvent) ProviderEvent {

	if e.ToolCallID == "" {
		return e
	}
	id, _, ok := strings.Cut(e.ToolCallID, "|")
	if ok {
		e.ToolCallID = id
	}

	return e
}
