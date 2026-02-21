package signal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agusx1211/miclaw/config"
)

type rpcCall struct {
	method string
	params map[string]any
}

type signalMock struct {
	t      *testing.T
	mu     sync.Mutex
	calls  []rpcCall
	callCh chan rpcCall
	events chan Envelope
	server *httptest.Server
}

type inboundCapture struct {
	sessionID string
	content   string
	metadata  map[string]string
}

func newSignalMock(t *testing.T) *signalMock {
	t.Helper()
	m := &signalMock{
		t:      t,
		calls:  make([]rpcCall, 0),
		callCh: make(chan rpcCall, 4096),
		events: make(chan Envelope, 16),
	}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v1/events"):
			m.handleEvents(w, r)
		case r.URL.Path == "/api/v1/rpc":
			m.handleRPC(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	return m
}

func (m *signalMock) close() {
	m.server.Close()
	close(m.events)
}

func (m *signalMock) url() string {
	return m.server.URL
}

func (m *signalMock) emit(env Envelope) {
	m.events <- env
}

func (m *signalMock) handleEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "flusher required", http.StatusInternalServerError)
		return
	}
	for {
		select {
		case env, ok := <-m.events:
			if !ok {
				return
			}
			data, err := json.Marshal(struct {
				Envelope Envelope `json:"envelope"`
			}{Envelope: env})
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (m *signalMock) handleRPC(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req struct {
		Method string         `json:"method"`
		Params map[string]any `json:"params"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "bad rpc", http.StatusBadRequest)
		return
	}
	m.recordCall(req.Method, req.Params)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
}

func (m *signalMock) recordCall(method string, params map[string]any) {
	m.mu.Lock()
	m.calls = append(m.calls, rpcCall{method: method, params: params})
	call := m.calls[len(m.calls)-1]
	m.mu.Unlock()
	m.callCh <- call
}

func (m *signalMock) waitCall(method string) rpcCall {
	m.t.Helper()
	timeout := time.After(500 * time.Millisecond)
	for {
		select {
		case call := <-m.callCh:
			if call.method == method {
				return call
			}
		case <-timeout:
			m.t.Fatalf("timed out waiting for rpc %q", method)
		}
	}
}

func (m *signalMock) waitCallCount(n int) []rpcCall {
	m.t.Helper()
	out := make([]rpcCall, 0, n)
	timeout := time.After(2 * time.Second)
	for len(out) < n {
		select {
		case call := <-m.callCh:
			out = append(out, call)
		case <-timeout:
			m.t.Fatalf("timed out waiting for %d calls, got %d", n, len(out))
		}
	}
	return out
}

func (m *signalMock) noMore(method string, since time.Duration) {
	m.t.Helper()
	timeout := time.After(since)
	for {
		select {
		case call := <-m.callCh:
			if call.method == method {
				m.t.Fatalf("unexpected rpc %q after response", method)
			}
		case <-timeout:
			return
		}
	}
}

func defaultSignalConfig() config.SignalConfig {
	return config.SignalConfig{
		Enabled:        true,
		Account:        "+5678",
		DMPolicy:       "open",
		GroupPolicy:    "open",
		TextChunkLimit: 4000,
	}
}

func startPipeline(ctx context.Context, p *Pipeline) <-chan error {
	done := make(chan error, 1)
	go func() {
		done <- p.Start(ctx)
	}()
	return done
}

func waitInbound(t *testing.T, in <-chan inboundCapture) inboundCapture {
	t.Helper()
	select {
	case got := <-in:
		return got
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for enqueue")
	}
	return inboundCapture{}
}

func readTextMessage(t *testing.T, params map[string]any) string {
	t.Helper()
	msg, ok := params["message"].(string)
	if !ok {
		t.Fatalf("message missing: %#v", params["message"])
	}
	return msg
}

func readRecipient(t *testing.T, params map[string]any) string {
	t.Helper()
	raw, ok := params["recipient"].([]any)
	if !ok || len(raw) == 0 {
		t.Fatalf("recipient missing: %#v", params["recipient"])
	}
	recipient, ok := raw[0].(string)
	if !ok {
		t.Fatalf("recipient not string: %#v", raw[0])
	}
	return recipient
}

func decodeStyles(t *testing.T, params map[string]any) []string {
	t.Helper()
	raw, ok := params["text-style"]
	if !ok {
		return nil
	}
	list, ok := raw.([]any)
	if !ok {
		t.Fatalf("text-style wrong type: %#v", raw)
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		enc, ok := item.(string)
		if !ok {
			t.Fatalf("text-style item wrong type: %#v", item)
		}
		out = append(out, enc)
	}
	return out
}

func hasStyle(styles []string, want string) bool {
	return slices.ContainsFunc(styles, func(v string) bool {
		return strings.HasSuffix(v, ":"+want)
	})
}

func assertContextCanceled(t *testing.T, done <-chan error) {
	t.Helper()
	err := <-done
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled context, got %v", err)
	}
}

func TestSignalInboundDM(t *testing.T) {
	mock := newSignalMock(t)
	defer mock.close()
	inbox := make(chan inboundCapture, 1)
	subscribeCh := make(chan Event)

	p := NewPipeline(
		NewClient(mock.url(), "+5678"),
		defaultSignalConfig(),
		func(sessionID, content string, metadata map[string]string) {
			inbox <- inboundCapture{sessionID: sessionID, content: content, metadata: metadata}
		},
		func() (<-chan Event, func()) { return subscribeCh, func() {} },
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := startPipeline(ctx, p)
	mock.emit(Envelope{
		SourceNumber: "+15550001111",
		SourceUUID:   "uuid-dm",
		SourceName:   "Alice",
		DataMessage:  &DataMessage{Message: "hello"},
	})

	got := waitInbound(t, inbox)
	cancel()
	assertContextCanceled(t, done)
	if got.sessionID != "signal:dm:uuid-dm" {
		t.Fatalf("sessionID = %q", got.sessionID)
	}
	if got.content != "hello" {
		t.Fatalf("content = %q", got.content)
	}
	if got.metadata["source_uuid"] != "uuid-dm" {
		t.Fatalf("metadata = %#v", got.metadata)
	}
}

func TestSignalOutboundResponse(t *testing.T) {
	subscribeCh := make(chan Event, 1)
	mock := newSignalMock(t)
	p := NewPipeline(
		NewClient(mock.url(), "+5678"),
		defaultSignalConfig(),
		func(sessionID, content string, metadata map[string]string) {},
		func() (<-chan Event, func()) { return subscribeCh, func() {} },
	)
	defer mock.close()
	ctx, cancel := context.WithCancel(context.Background())
	done := startPipeline(ctx, p)
	subscribeCh <- Event{SessionID: "signal:dm:user-abc", Text: "hello response"}

	call := mock.waitCall("send")
	cancel()
	assertContextCanceled(t, done)
	if call.params == nil {
		t.Fatal("no rpc params")
	}
	if readRecipient(t, call.params) != "user-abc" {
		t.Fatalf("recipient = %q", readRecipient(t, call.params))
	}
	if readTextMessage(t, call.params) != "hello response" {
		t.Fatalf("text = %q", readTextMessage(t, call.params))
	}
}

func TestSignalMarkdownConversion(t *testing.T) {
	subscribeCh := make(chan Event, 1)
	mock := newSignalMock(t)
	defer mock.close()

	p := NewPipeline(
		NewClient(mock.url(), "+5678"),
		defaultSignalConfig(),
		func(sessionID, content string, metadata map[string]string) {},
		func() (<-chan Event, func()) { return subscribeCh, func() {} },
	)
	ctx, cancel := context.WithCancel(context.Background())
	done := startPipeline(ctx, p)
	subscribeCh <- Event{
		SessionID: "signal:dm:user-md",
		Text:      "**bold** and *italic* and `code`",
	}

	call := mock.waitCall("send")
	cancel()
	assertContextCanceled(t, done)
	if readTextMessage(t, call.params) != "bold and italic and code" {
		t.Fatalf("text = %q", readTextMessage(t, call.params))
	}
	styles := decodeStyles(t, call.params)
	if len(styles) != 3 {
		t.Fatalf("styles = %v", styles)
	}
	if !hasStyle(styles, "BOLD") || !hasStyle(styles, "ITALIC") || !hasStyle(styles, "MONOSPACE") {
		t.Fatalf("styles = %v", styles)
	}
}

func TestSignalChunking(t *testing.T) {
	subscribeCh := make(chan Event, 1)
	mock := newSignalMock(t)
	defer mock.close()
	cfg := defaultSignalConfig()
	cfg.TextChunkLimit = 100

	p := NewPipeline(
		NewClient(mock.url(), "+5678"),
		cfg,
		func(sessionID, content string, metadata map[string]string) {},
		func() (<-chan Event, func()) { return subscribeCh, func() {} },
	)
	ctx, cancel := context.WithCancel(context.Background())
	done := startPipeline(ctx, p)
	subscribeCh <- Event{SessionID: "signal:dm:user-1", Text: strings.Repeat("x", 4101)}

	expected := (4101 + cfg.TextChunkLimit - 1) / cfg.TextChunkLimit
	calls := mock.waitCallCount(expected)
	cancel()
	assertContextCanceled(t, done)
	if len(calls) != expected {
		t.Fatalf("calls = %d", len(calls))
	}
	for _, c := range calls {
		if c.method != "send" {
			t.Fatalf("method = %q", c.method)
		}
		if len(readTextMessage(t, c.params)) > 100 {
			t.Fatalf("chunk too long")
		}
	}
}

func TestSignalAccessControl(t *testing.T) {
	mock := newSignalMock(t)
	defer mock.close()
	inbox := make(chan inboundCapture, 2)
	cfg := defaultSignalConfig()
	cfg.DMPolicy = "allowlist"
	cfg.Allowlist = []string{"allowed-uuid"}

	p := NewPipeline(
		NewClient(mock.url(), "+5678"),
		cfg,
		func(sessionID, content string, metadata map[string]string) {
			inbox <- inboundCapture{sessionID: sessionID, content: content, metadata: metadata}
		},
		func() (<-chan Event, func()) { return make(chan Event), func() {} },
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := startPipeline(ctx, p)
	mock.emit(Envelope{
		SourceNumber: "+1111",
		SourceUUID:   "blocked-uuid",
		DataMessage:  &DataMessage{Message: "block"},
	})
	mock.emit(Envelope{
		SourceNumber: "+1111",
		SourceUUID:   "allowed-uuid",
		DataMessage:  &DataMessage{Message: "allow"},
	})

	got := waitInbound(t, inbox)
	cancel()
	assertContextCanceled(t, done)
	if got.sessionID != "signal:dm:allowed-uuid" {
		t.Fatalf("sessionID = %q", got.sessionID)
	}
	if got.content != "allow" {
		t.Fatalf("content = %q", got.content)
	}
}

func TestSignalAccessControlByPhoneNumber(t *testing.T) {
	mock := newSignalMock(t)
	defer mock.close()
	inbox := make(chan inboundCapture, 2)
	cfg := defaultSignalConfig()
	cfg.DMPolicy = "allowlist"
	cfg.Allowlist = []string{"+1111"}

	p := NewPipeline(
		NewClient(mock.url(), "+5678"),
		cfg,
		func(sessionID, content string, metadata map[string]string) {
			inbox <- inboundCapture{sessionID: sessionID, content: content, metadata: metadata}
		},
		func() (<-chan Event, func()) { return make(chan Event), func() {} },
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := startPipeline(ctx, p)
	mock.emit(Envelope{
		SourceNumber: "+9999",
		SourceUUID:   "allowed-uuid",
		DataMessage:  &DataMessage{Message: "block"},
	})
	mock.emit(Envelope{
		SourceNumber: "+1111",
		SourceUUID:   "other-uuid",
		DataMessage:  &DataMessage{Message: "allow"},
	})

	var got inboundCapture
	select {
	case got = <-inbox:
	case <-time.After(500 * time.Millisecond):
		cancel()
		assertContextCanceled(t, done)
		t.Fatal("timed out waiting for enqueue")
	}
	cancel()
	assertContextCanceled(t, done)
	if got.sessionID != "signal:dm:other-uuid" {
		t.Fatalf("sessionID = %q", got.sessionID)
	}
	if got.content != "allow" {
		t.Fatalf("content = %q", got.content)
	}
}

func TestSignalTypingIndicator(t *testing.T) {
	mock := newSignalMock(t)
	defer mock.close()
	subscribeCh := make(chan Event, 1)
	cfg := defaultSignalConfig()
	p := NewPipeline(
		NewClient(mock.url(), "+5678"),
		cfg,
		func(sessionID, content string, metadata map[string]string) {},
		func() (<-chan Event, func()) { return subscribeCh, func() {} },
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := startPipeline(ctx, p)
	mock.emit(Envelope{
		SourceNumber: "+1555",
		SourceUUID:   "typing-uuid",
		DataMessage:  &DataMessage{Message: "inbound"},
	})

	_ = mock.waitCall("sendTyping")
	subscribeCh <- Event{SessionID: "signal:dm:typing-uuid", Text: "outbound"}
	_ = mock.waitCall("send")
	mock.noMore("sendTyping", 120*time.Millisecond)
	cancel()
	assertContextCanceled(t, done)
}

func TestSignalGroupMessage(t *testing.T) {
	mock := newSignalMock(t)
	defer mock.close()
	inbox := make(chan inboundCapture, 1)
	cfg := defaultSignalConfig()
	cfg.DMPolicy = "disabled"
	cfg.GroupPolicy = "open"

	p := NewPipeline(
		NewClient(mock.url(), "+5678"),
		cfg,
		func(sessionID, content string, metadata map[string]string) {
			inbox <- inboundCapture{sessionID: sessionID, content: content, metadata: metadata}
		},
		func() (<-chan Event, func()) { return make(chan Event), func() {} },
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := startPipeline(ctx, p)
	mock.emit(Envelope{
		SourceNumber: "+1555",
		SourceUUID:   "user-1",
		SourceName:   "Group User",
		DataMessage: &DataMessage{
			Message: "hello group",
			GroupInfo: &GroupInfo{
				GroupID: "group-1",
			},
		},
	})

	got := waitInbound(t, inbox)
	cancel()
	assertContextCanceled(t, done)
	if got.sessionID != "signal:group:group-1" {
		t.Fatalf("sessionID = %q", got.sessionID)
	}
	if got.content != "hello group" {
		t.Fatalf("content = %q", got.content)
	}
}
