package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/agusx1211/miclaw/config"
	"github.com/agusx1211/miclaw/model"
)

const (
	openRouterDefaultBaseURL = "https://openrouter.ai/api/v1"
	openRouterReferer        = "https://github.com/agusx1211/miclaw"
	openRouterTitle          = "miclaw"
	defaultMaxTokens         = 8192
)

type OpenRouter struct {
	baseURL   string
	apiKey    string
	model     string
	maxTokens int
	client    *http.Client
}

type openRouterRequest struct {
	Model     string              `json:"model"`
	Messages  []openRouterMessage `json:"messages"`
	Tools     []openRouterTool    `json:"tools,omitempty"`
	Stream    bool                `json:"stream"`
	MaxTokens int                 `json:"max_tokens"`
}

type openRouterMessage struct {
	Role       string               `json:"role"`
	Content    string               `json:"content,omitempty"`
	ToolCalls  []openRouterToolCall `json:"tool_calls,omitempty"`
	ToolCallID string               `json:"tool_call_id,omitempty"`
}

type openRouterToolCall struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Function openRouterFunctionCall `json:"function"`
}

type openRouterFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openRouterTool struct {
	Type     string                   `json:"type"`
	Function openRouterToolDefinition `json:"function"`
}

type openRouterToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

func NewOpenRouter(cfg config.ProviderConfig) *OpenRouter {

	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		base = openRouterDefaultBaseURL
	}
	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}
	p := &OpenRouter{
		baseURL:   base,
		apiKey:    cfg.APIKey,
		model:     cfg.Model,
		maxTokens: maxTokens,
		client:    &http.Client{},
	}

	return p
}

func (o *OpenRouter) Model() ModelInfo {

	info := ModelInfo{
		ID:        o.model,
		Name:      o.model,
		MaxOutput: o.maxTokens,
	}

	return info
}

func (o *OpenRouter) Stream(ctx context.Context, messages []model.Message, tools []ToolDef) <-chan ProviderEvent {

	out := make(chan ProviderEvent, 16)

	go o.stream(ctx, messages, tools, out)
	return out
}

func (o *OpenRouter) stream(ctx context.Context, messages []model.Message, tools []ToolDef, out chan<- ProviderEvent) {

	defer close(out)
	payload, err := marshalRequest(o.model, o.maxTokens, messages, tools)
	if err != nil {
		out <- errorEvent(err)
		return
	}
	resp, err := o.post(ctx, payload)
	if err != nil {
		out <- errorEvent(err)
		return
	}
	if resp.StatusCode != http.StatusOK {
		out <- errorEvent(readStatusError(resp))
		return
	}
	for e := range parseSSEStream(resp.Body) {
		out <- e
	}
}

func marshalRequest(modelID string, maxTokens int, messages []model.Message, tools []ToolDef) ([]byte, error) {

	body := openRouterRequest{
		Model:     modelID,
		Messages:  encodeMessages(messages),
		Tools:     encodeTools(tools),
		Stream:    true,
		MaxTokens: maxTokens,
	}

	return json.Marshal(body)
}

func encodeMessages(messages []model.Message) []openRouterMessage {

	out := make([]openRouterMessage, 0, len(messages))
	for _, m := range messages {
		out = append(out, encodeMessage(m)...)
	}

	return out
}

func encodeMessage(m model.Message) []openRouterMessage {

	out := make([]openRouterMessage, 0, len(m.Parts))
	msg := openRouterMessage{Role: string(m.Role)}
	for _, p := range m.Parts {
		out, msg = encodePart(out, msg, p)
	}
	if msg.Content != "" || len(msg.ToolCalls) > 0 {
		out = append(out, msg)
	}

	return out
}

func encodePart(out []openRouterMessage, msg openRouterMessage, part model.MessagePart) ([]openRouterMessage, openRouterMessage) {

	switch p := part.(type) {
	case model.TextPart:
		msg.Content += p.Text
	case model.ReasoningPart:
		msg.Content += p.Text
	case model.ToolCallPart:
		msg.ToolCalls = append(msg.ToolCalls, encodeToolCall(p))
	case model.ToolResultPart:
		role := msg.Role
		if msg.Content != "" || len(msg.ToolCalls) > 0 {
			out = append(out, msg)
		}
		out = append(out, openRouterMessage{Role: string(model.RoleTool), ToolCallID: p.ToolCallID, Content: p.Content})
		msg = openRouterMessage{Role: role}
	case model.FinishPart:
	case model.BinaryPart:
		panic("binary part is not supported for openrouter")
	default:
		panic(fmt.Sprintf("unknown message part type: %T", part))
	}

	return out, msg
}

func encodeToolCall(p model.ToolCallPart) openRouterToolCall {

	c := openRouterToolCall{
		ID:   p.ID,
		Type: "function",
		Function: openRouterFunctionCall{
			Name:      p.Name,
			Arguments: string(p.Parameters),
		},
	}

	return c
}

func encodeTools(tools []ToolDef) []openRouterTool {

	out := make([]openRouterTool, 0, len(tools))
	for _, t := range tools {

		out = append(out, openRouterTool{
			Type:     "function",
			Function: openRouterToolDefinition(t),
		})
	}

	return out
}

func (o *OpenRouter) post(ctx context.Context, payload []byte) (*http.Response, error) {

	return withRetry(ctx, 0, func() (*http.Response, error) {

		return o.doPost(ctx, payload)
	})
}

func (o *OpenRouter) doPost(ctx context.Context, payload []byte) (*http.Response, error) {

	u := o.baseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	applyHeaders(req, o.apiKey)
	r, err := o.client.Do(req)
	if err != nil {
		return nil, err
	}

	return r, nil
}

func applyHeaders(req *http.Request, apiKey string) {

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("HTTP-Referer", openRouterReferer)
	req.Header.Set("X-Title", openRouterTitle)

}

func readStatusError(resp *http.Response) error {

	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 16*1024))
	msg := strings.TrimSpace(string(b))
	if msg == "" {
		return fmt.Errorf("openrouter stream failed: status %d", resp.StatusCode)
	}
	return fmt.Errorf("openrouter stream failed: status %d: %s", resp.StatusCode, msg)
}

func errorEvent(err error) ProviderEvent {

	return ProviderEvent{Type: EventError, Error: err}
}
