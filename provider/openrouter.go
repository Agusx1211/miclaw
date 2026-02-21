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
	must(strings.TrimSpace(cfg.APIKey) != "", "provider api key is required")
	must(strings.TrimSpace(cfg.Model) != "", "provider model is required")
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
	must(p.client != nil, "http client must not be nil")
	must(p.baseURL != "", "provider base url must not be empty")
	return p
}

func (o *OpenRouter) Model() ModelInfo {
	must(o != nil, "provider must not be nil")
	must(o.model != "", "provider model must not be empty")
	info := ModelInfo{
		ID:        o.model,
		Name:      o.model,
		MaxOutput: o.maxTokens,
	}
	must(info.ID != "", "model info id must not be empty")
	must(info.MaxOutput > 0, "model max output must be positive")
	return info
}

func (o *OpenRouter) Stream(ctx context.Context, messages []model.Message, tools []ToolDef) <-chan ProviderEvent {
	must(o != nil, "provider must not be nil")
	must(ctx != nil, "context must not be nil")
	out := make(chan ProviderEvent, 16)
	must(out != nil, "provider event channel must not be nil")
	must(len(messages) > 0, "messages must not be empty")
	go o.stream(ctx, messages, tools, out)
	return out
}

func (o *OpenRouter) stream(ctx context.Context, messages []model.Message, tools []ToolDef, out chan<- ProviderEvent) {
	must(ctx != nil, "context must not be nil")
	must(out != nil, "provider event channel must not be nil")
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
	must(strings.TrimSpace(modelID) != "", "model id must not be empty")
	must(maxTokens > 0, "max tokens must be positive")
	body := openRouterRequest{
		Model:     modelID,
		Messages:  encodeMessages(messages),
		Tools:     encodeTools(tools),
		Stream:    true,
		MaxTokens: maxTokens,
	}
	must(body.Stream, "streaming flag must be true")
	must(len(body.Messages) > 0, "request messages must not be empty")
	return json.Marshal(body)
}

func encodeMessages(messages []model.Message) []openRouterMessage {
	must(len(messages) > 0, "messages must not be empty")
	must(messages != nil, "messages slice must not be nil")
	out := make([]openRouterMessage, 0, len(messages))
	for _, m := range messages {
		out = append(out, encodeMessage(m)...)
	}
	must(len(out) > 0, "encoded messages must not be empty")
	must(out != nil, "encoded messages slice must not be nil")
	return out
}

func encodeMessage(m model.Message) []openRouterMessage {
	must(strings.TrimSpace(string(m.Role)) != "", "message role must not be empty")
	must(len(m.Parts) > 0, "message parts must not be empty")
	out := make([]openRouterMessage, 0, len(m.Parts))
	msg := openRouterMessage{Role: string(m.Role)}
	for _, p := range m.Parts {
		out, msg = encodePart(out, msg, p)
	}
	if msg.Content != "" || len(msg.ToolCalls) > 0 {
		out = append(out, msg)
	}
	must(len(out) > 0, "encoded message output must not be empty")
	must(out[0].Role != "", "encoded message role must not be empty")
	return out
}

func encodePart(out []openRouterMessage, msg openRouterMessage, part model.MessagePart) ([]openRouterMessage, openRouterMessage) {
	must(out != nil, "encoded messages output must not be nil")
	must(part != nil, "message part must not be nil")
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
	must(msg.Role != "", "encoded message role must not be empty")
	must(out != nil, "encoded messages output must not be nil")
	return out, msg
}

func encodeToolCall(p model.ToolCallPart) openRouterToolCall {
	must(strings.TrimSpace(p.ID) != "", "tool call id must not be empty")
	must(strings.TrimSpace(p.Name) != "", "tool call name must not be empty")
	c := openRouterToolCall{
		ID:   p.ID,
		Type: "function",
		Function: openRouterFunctionCall{
			Name:      p.Name,
			Arguments: string(p.Parameters),
		},
	}
	must(c.Function.Name != "", "tool call function name must not be empty")
	must(c.Type == "function", "tool call type must be function")
	return c
}

func encodeTools(tools []ToolDef) []openRouterTool {
	must(tools != nil || len(tools) == 0, "tools slice must be valid")
	must(len(tools) >= 0, "tools length cannot be negative")
	out := make([]openRouterTool, 0, len(tools))
	for _, t := range tools {
		must(strings.TrimSpace(t.Name) != "", "tool name must not be empty")
		out = append(out, openRouterTool{
			Type:     "function",
			Function: openRouterToolDefinition(t),
		})
	}
	must(len(out) == len(tools), "encoded tool count mismatch")
	must(out != nil, "encoded tool slice must not be nil")
	return out
}

func (o *OpenRouter) post(ctx context.Context, payload []byte) (*http.Response, error) {
	must(o != nil, "provider must not be nil")
	must(ctx != nil, "context must not be nil")
	return withRetry(ctx, 0, func() (*http.Response, error) {
		must(len(payload) > 0, "request payload must not be empty")
		return o.doPost(ctx, payload)
	})
}

func (o *OpenRouter) doPost(ctx context.Context, payload []byte) (*http.Response, error) {
	must(ctx != nil, "context must not be nil")
	must(len(payload) > 0, "request payload must not be empty")
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
	must(r != nil, "http response must not be nil")
	must(r.StatusCode >= 100, "http response status must be valid")
	return r, nil
}

func applyHeaders(req *http.Request, apiKey string) {
	must(req != nil, "http request must not be nil")
	must(strings.TrimSpace(apiKey) != "", "api key must not be empty")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("HTTP-Referer", openRouterReferer)
	req.Header.Set("X-Title", openRouterTitle)
	must(req.Header.Get("Authorization") != "", "authorization header must not be empty")
	must(req.Header.Get("HTTP-Referer") != "", "http referer header must not be empty")
}

func readStatusError(resp *http.Response) error {
	must(resp != nil, "http response must not be nil")
	must(resp.Body != nil, "http response body must not be nil")
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 16*1024))
	msg := strings.TrimSpace(string(b))
	if msg == "" {
		return fmt.Errorf("openrouter stream failed: status %d", resp.StatusCode)
	}
	return fmt.Errorf("openrouter stream failed: status %d: %s", resp.StatusCode, msg)
}

func errorEvent(err error) ProviderEvent {
	must(err != nil, "error must not be nil")
	must(strings.TrimSpace(err.Error()) != "", "error message must not be empty")
	return ProviderEvent{Type: EventError, Error: err}
}
