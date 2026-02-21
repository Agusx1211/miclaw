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

type responseChunk struct {
	Type     string                `json:"type"`
	Delta    string                `json:"delta"`
	ItemID   string                `json:"item_id"`
	Item     *responseChunkItem    `json:"item"`
	Response *responseChunkPayload `json:"response"`
}

type responseChunkItem struct {
	ID        string              `json:"id"`
	Type      string              `json:"type"`
	CallID    string              `json:"call_id"`
	Name      string              `json:"name"`
	Arguments string              `json:"arguments"`
	Content   []responseChunkText `json:"content"`
}

type responseChunkText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type responseChunkPayload struct {
	Usage *responseChunkUsage `json:"usage"`
	Error *responseChunkError `json:"error"`
}

type responseChunkUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	InputDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"input_tokens_details"`
	OutputDetails *struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"output_tokens_details"`
}

type responseChunkError struct {
	Message string `json:"message"`
}

type toolState struct {
	id   string
	name string
}

type responseToolState struct {
	toolState
	sawDelta bool
}

func parseSSEStream(body io.ReadCloser) <-chan ProviderEvent {

	out := make(chan ProviderEvent, 16)

	go parseSSE(body, out)
	return out
}

func parseSSE(body io.ReadCloser, out chan<- ProviderEvent) {

	defer close(out)
	defer body.Close()

	s := bufio.NewScanner(body)
	s.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	p := make(map[int]toolState)
	rp := make(map[string]responseToolState)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		if flushData(strings.TrimSpace(strings.TrimPrefix(line, "data:")), p, rp, out) {
			return
		}
	}
	if err := s.Err(); err != nil {
		out <- ProviderEvent{Type: EventError, Error: err}
	}
}

func flushData(data string, p map[int]toolState, rp map[string]responseToolState, out chan<- ProviderEvent) bool {

	d := strings.TrimSpace(data)
	if d == "" {
		return false
	}
	if d == "[DONE]" {
		return true
	}
	chunk, ok := parseChunk(d)
	if ok && (len(chunk.Choices) > 0 || chunk.Usage != nil) {
		emitChunk(chunk, p, out)
		return false
	}
	rc, rok := parseResponseChunk(d)
	if !rok {
		return false
	}
	return emitResponseChunk(rc, rp, out)
}

func parseChunk(data string) (openAIChunk, bool) {

	var chunk openAIChunk
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		return openAIChunk{}, false
	}
	return chunk, true
}

func parseResponseChunk(data string) (responseChunk, bool) {

	var chunk responseChunk
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		return responseChunk{}, false
	}
	if chunk.Type == "" {
		return responseChunk{}, false
	}
	return chunk, true
}

func emitChunk(chunk openAIChunk, p map[int]toolState, out chan<- ProviderEvent) {

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

func emitResponseChunk(chunk responseChunk, p map[string]responseToolState, out chan<- ProviderEvent) bool {
	switch chunk.Type {
	case "response.output_text.delta":
		if chunk.Delta != "" {
			out <- ProviderEvent{Type: EventContentDelta, Delta: chunk.Delta}
		}
	case "response.reasoning_text.delta", "response.reasoning_summary_text.delta":
		if chunk.Delta != "" {
			out <- ProviderEvent{Type: EventThinkingDelta, Delta: chunk.Delta}
		}
	case "response.output_item.added":
		emitResponseToolStart(chunk, p, out)
	case "response.function_call_arguments.delta":
		emitResponseToolDelta(chunk, p, out)
	case "response.output_item.done":
		emitResponseToolDone(chunk, p, out)
	case "response.completed", "response.done":
		emitResponseToolStops(p, out)
		out <- ProviderEvent{Type: EventComplete, Usage: responseUsageInfo(chunk.Response)}
		return true
	case "response.failed", "response.incomplete":
		msg := chunk.Type
		if chunk.Response != nil && chunk.Response.Error != nil && chunk.Response.Error.Message != "" {
			msg = chunk.Response.Error.Message
		}
		out <- ProviderEvent{Type: EventError, Error: &responseError{message: msg}}
		return true
	}
	return false
}

type responseError struct {
	message string
}

func (e *responseError) Error() string {
	return e.message
}

func emitResponseToolStart(chunk responseChunk, p map[string]responseToolState, out chan<- ProviderEvent) {
	if chunk.Item == nil || chunk.Item.Type != "function_call" {
		return
	}
	key := responseToolKey(chunk.Item.ID, chunk.Item.CallID)
	if key == "" {
		return
	}
	state := responseToolState{toolState: toolState{id: responseToolID(chunk.Item.CallID, chunk.Item.ID), name: chunk.Item.Name}}
	p[key] = state
	out <- ProviderEvent{Type: EventToolUseStart, ToolCallID: state.id, ToolName: state.name}
	if chunk.Item.Arguments != "" {
		out <- ProviderEvent{Type: EventToolUseDelta, ToolCallID: state.id, ToolName: state.name, Delta: chunk.Item.Arguments}
		state.sawDelta = true
		p[key] = state
	}
}

func emitResponseToolDelta(chunk responseChunk, p map[string]responseToolState, out chan<- ProviderEvent) {
	key := responseToolKey(chunk.ItemID, "")
	if key == "" {
		return
	}
	state, ok := p[key]
	if !ok {
		return
	}
	if chunk.Delta == "" {
		return
	}
	out <- ProviderEvent{Type: EventToolUseDelta, ToolCallID: state.id, ToolName: state.name, Delta: chunk.Delta}
	state.sawDelta = true
	p[key] = state
}

func emitResponseToolDone(chunk responseChunk, p map[string]responseToolState, out chan<- ProviderEvent) {
	if chunk.Item == nil || chunk.Item.Type != "function_call" {
		return
	}
	key := responseToolKey(chunk.Item.ID, chunk.Item.CallID)
	if key == "" {
		return
	}
	state, ok := p[key]
	if !ok {
		state = responseToolState{toolState: toolState{id: responseToolID(chunk.Item.CallID, chunk.Item.ID), name: chunk.Item.Name}}
	}
	if chunk.Item.Arguments != "" && !state.sawDelta {
		out <- ProviderEvent{Type: EventToolUseDelta, ToolCallID: state.id, ToolName: state.name, Delta: chunk.Item.Arguments}
	}
	out <- ProviderEvent{Type: EventToolUseStop, ToolCallID: state.id, ToolName: state.name}
	delete(p, key)
}

func emitResponseToolStops(p map[string]responseToolState, out chan<- ProviderEvent) {
	keys := make([]string, 0, len(p))
	for key := range p {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		state := p[key]
		out <- ProviderEvent{Type: EventToolUseStop, ToolCallID: state.id, ToolName: state.name}
		delete(p, key)
	}
}

func responseToolKey(itemID, callID string) string {
	if itemID != "" {
		return itemID
	}
	return callID
}

func responseToolID(callID, itemID string) string {
	if callID != "" {
		return callID
	}
	return itemID
}

func responseUsageInfo(resp *responseChunkPayload) *UsageInfo {
	if resp == nil || resp.Usage == nil {
		return nil
	}
	info := &UsageInfo{
		PromptTokens:     resp.Usage.InputTokens,
		CompletionTokens: resp.Usage.OutputTokens,
	}
	if resp.Usage.InputDetails != nil {
		info.CacheReadTokens = resp.Usage.InputDetails.CachedTokens
	}
	if resp.Usage.OutputDetails != nil {
		info.CacheWriteTokens = resp.Usage.OutputDetails.ReasoningTokens
	}
	return info
}

func emitToolCalls(calls []openAIToolCallDelta, p map[int]toolState, out chan<- ProviderEvent) {

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
