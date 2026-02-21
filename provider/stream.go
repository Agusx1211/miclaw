package provider

import (
	"bufio"
	"encoding/json"
	"io"
	"sort"
	"strings"
)

type openAIChunk struct {
	Choices []openAIChoice `json:"choices"`
	Usage   *openAIUsage   `json:"usage"`
}

type openAIChoice struct {
	Delta        openAIDelta `json:"delta"`
	FinishReason string      `json:"finish_reason"`
}

type openAIDelta struct {
	Content          string                `json:"content"`
	Thinking         string                `json:"thinking"`
	Reasoning        string                `json:"reasoning"`
	ReasoningContent string                `json:"reasoning_content"`
	ToolCalls        []openAIToolCallDelta `json:"tool_calls"`
}

type openAIToolCallDelta struct {
	Index    int                 `json:"index"`
	ID       string              `json:"id"`
	Function openAIFunctionDelta `json:"function"`
}

type openAIFunctionDelta struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	CacheReadTokens  int `json:"cache_read_tokens"`
	CacheWriteTokens int `json:"cache_write_tokens"`
}

type toolState struct {
	id   string
	name string
}

func parseSSEStream(body io.ReadCloser) <-chan ProviderEvent {
	must(body != nil, "stream body is nil")
	out := make(chan ProviderEvent, 16)
	must(out != nil, "provider event channel is nil")
	go parseSSE(body, out)
	return out
}

func parseSSE(body io.ReadCloser, out chan<- ProviderEvent) {
	must(body != nil, "stream body is nil")
	must(out != nil, "provider event channel is nil")
	defer close(out)
	defer body.Close()

	s := bufio.NewScanner(body)
	s.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	p := make(map[int]toolState)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		if flushData(strings.TrimSpace(strings.TrimPrefix(line, "data:")), p, out) {
			return
		}
	}
	if err := s.Err(); err != nil {
		out <- ProviderEvent{Type: EventError, Error: err}
	}
}

func flushData(data string, p map[int]toolState, out chan<- ProviderEvent) bool {
	must(p != nil, "pending tool call map is nil")
	must(out != nil, "provider event channel is nil")
	d := strings.TrimSpace(data)
	if d == "" {
		return false
	}
	if d == "[DONE]" {
		return true
	}
	chunk, ok := parseChunk(d)
	if !ok {
		return false
	}
	emitChunk(chunk, p, out)
	return false
}

func parseChunk(data string) (openAIChunk, bool) {
	must(data != "", "chunk payload is empty")
	must(strings.TrimSpace(data) == data, "chunk payload must be trimmed")
	var chunk openAIChunk
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		return openAIChunk{}, false
	}
	return chunk, true
}

func emitChunk(chunk openAIChunk, p map[int]toolState, out chan<- ProviderEvent) {
	must(p != nil, "pending tool call map is nil")
	must(out != nil, "provider event channel is nil")
	for _, c := range chunk.Choices {
		if c.Delta.Content != "" {
			out <- ProviderEvent{Type: EventContentDelta, Delta: c.Delta.Content}
		}
		if c.Delta.Thinking != "" {
			out <- ProviderEvent{Type: EventThinkingDelta, Delta: c.Delta.Thinking}
		}
		if c.Delta.Thinking == "" && c.Delta.Reasoning != "" {
			out <- ProviderEvent{Type: EventThinkingDelta, Delta: c.Delta.Reasoning}
		}
		if c.Delta.Thinking == "" && c.Delta.Reasoning == "" && c.Delta.ReasoningContent != "" {
			out <- ProviderEvent{Type: EventThinkingDelta, Delta: c.Delta.ReasoningContent}
		}
		emitToolCalls(c.Delta.ToolCalls, p, out)
		if c.FinishReason != "" {
			emitToolStops(p, out)
			out <- ProviderEvent{Type: EventComplete, Usage: usageInfo(chunk.Usage)}
		}
	}
}

func emitToolCalls(calls []openAIToolCallDelta, p map[int]toolState, out chan<- ProviderEvent) {
	must(p != nil, "pending tool call map is nil")
	must(out != nil, "provider event channel is nil")
	for _, c := range calls {
		st := p[c.Index]
		if c.ID != "" {
			st.id = c.ID
			if c.Function.Name != "" {
				st.name = c.Function.Name
			}
			p[c.Index] = st
			out <- ProviderEvent{Type: EventToolUseStart, ToolCallID: st.id, ToolName: st.name}
			continue
		}
		if c.Function.Name != "" {
			st.name = c.Function.Name
		}
		if st.id == "" && c.Function.Name == "" && c.Function.Arguments == "" {
			continue
		}
		p[c.Index] = st
		out <- ProviderEvent{
			Type:       EventToolUseDelta,
			ToolCallID: st.id,
			ToolName:   st.name,
			Delta:      c.Function.Arguments,
		}
	}
}

func emitToolStops(p map[int]toolState, out chan<- ProviderEvent) {
	must(p != nil, "pending tool call map is nil")
	must(out != nil, "provider event channel is nil")
	k := make([]int, 0, len(p))
	for i := range p {
		k = append(k, i)
	}
	sort.Ints(k)
	for _, i := range k {
		st := p[i]
		out <- ProviderEvent{Type: EventToolUseStop, ToolCallID: st.id, ToolName: st.name}
		delete(p, i)
	}
}

func usageInfo(u *openAIUsage) *UsageInfo {
	must(u == nil || u.PromptTokens >= 0, "prompt tokens must be non-negative")
	must(u == nil || u.CompletionTokens >= 0, "completion tokens must be non-negative")
	if u == nil {
		return nil
	}
	return &UsageInfo{
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		CacheReadTokens:  u.CacheReadTokens,
		CacheWriteTokens: u.CacheWriteTokens,
	}
}
