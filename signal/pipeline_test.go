package signal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/agusx1211/miclaw/config"
)

type capturedInput struct {
	sessionID string
	content   string
	metadata  map[string]string
}

type capturedCall struct {
	method string
	params map[string]any
}

func newSignalServer(t *testing.T, env *Envelope, calls chan<- capturedCall) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v1/events"):
			w.Header().Set("Content-Type", "text/event-stream")
			if env != nil {
				data, _ := json.Marshal(struct {
					Envelope Envelope `json:"envelope"`
				}{Envelope: *env})
				fmt.Fprintf(w, "data: %s\n\n", data)
				env = nil
			}
			flusher := w.(http.Flusher)
			flusher.Flush()
			<-r.Context().Done()
		case r.URL.Path == "/api/v1/rpc":
			body, _ := io.ReadAll(r.Body)
			var req struct {
				Method string         `json:"method"`
				Params map[string]any `json:"params"`
			}
			_ = json.Unmarshal(body, &req)
			if calls != nil {
				calls <- capturedCall{method: req.Method, params: req.Params}
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":{}}`)
		default:
			http.NotFound(w, r)
		}
	}))
}

func waitInput(t *testing.T, ch <-chan capturedInput) capturedInput {
	t.Helper()
	select {
	case got := <-ch:
		return got
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for input")
	}
	return capturedInput{}
}

func waitCall(t *testing.T, ch <-chan capturedCall) capturedCall {
	t.Helper()
	select {
	case got := <-ch:
		return got
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for call")
	}
	return capturedCall{}
}

func readEnvelopeRecipient(t *testing.T, params map[string]any) string {
	t.Helper()
	val, ok := params["recipient"].([]any)
	if !ok || len(val) == 0 {
		t.Fatalf("missing recipient: %#v", params)
	}
	recipient, ok := val[0].(string)
	if !ok {
		t.Fatalf("recipient is not string: %#v", params["recipient"])
	}
	return recipient
}

func readGroupID(t *testing.T, params map[string]any) string {
	t.Helper()
	groupID, ok := params["groupId"].(string)
	if !ok {
		t.Fatalf("groupId is not string: %#v", params["groupId"])
	}
	return groupID
}

func TestPipelineEnqueuesInboundMessage(t *testing.T) {
	inbox := make(chan capturedInput, 1)
	env := &Envelope{
		SourceNumber: "+15559990000",
		SourceUUID:   "user-1",
		SourceName:   "Tester",
		DataMessage: &DataMessage{
			Message: "hello signal",
		},
	}
	srv := newSignalServer(t, env, nil)
	defer srv.Close()
	p := NewPipeline(
		NewClient(srv.URL, "+1000"),
		config.SignalConfig{Account: "+1000", DMPolicy: "open", TextChunkLimit: 100},
		func(sessionID, content string, metadata map[string]string) {
			inbox <- capturedInput{sessionID: sessionID, content: content, metadata: metadata}
		},
		func() (<-chan Event, func()) {
			ch := make(chan Event)
			return ch, func() {}
		},
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Start(ctx) }()
	input := waitInput(t, inbox)
	cancel()
	if err := <-done; err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled context, got %v", err)
	}
	if input.sessionID != "signal:dm:user-1" {
		t.Fatalf("sessionID = %q", input.sessionID)
	}
	if input.content != "hello signal" {
		t.Fatalf("content = %q", input.content)
	}
	if input.metadata["source_uuid"] != "user-1" {
		t.Fatalf("metadata = %#v", input.metadata)
	}
}

func TestPipelineRejectsUnauthorized(t *testing.T) {
	inbox := make(chan capturedInput, 1)
	env := &Envelope{
		SourceNumber: "+15559990000",
		SourceUUID:   "user-1",
		DataMessage: &DataMessage{
			Message: "private",
		},
	}
	srv := newSignalServer(t, env, nil)
	defer srv.Close()
	p := NewPipeline(
		NewClient(srv.URL, "+1000"),
		config.SignalConfig{Account: "+1000", DMPolicy: "disabled", TextChunkLimit: 100},
		func(sessionID, content string, metadata map[string]string) {
			inbox <- capturedInput{sessionID: sessionID, content: content, metadata: metadata}
		},
		func() (<-chan Event, func()) {
			ch := make(chan Event)
			return ch, func() {}
		},
	)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Start(ctx) }()
	select {
	case got := <-inbox:
		cancel()
		<-done
		t.Fatalf("unexpected enqueue: %+v", got)
	case <-time.After(200 * time.Millisecond):
		cancel()
		if err := <-done; err == nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("expected canceled context, got %v", err)
		}
	}
}

func TestPipelineSkipsSelfMessage(t *testing.T) {
	inbox := make(chan capturedInput, 1)
	env := &Envelope{
		SourceNumber: "+1000",
		SourceUUID:   "user-self",
		DataMessage: &DataMessage{
			Message: "ignore me",
		},
	}
	srv := newSignalServer(t, env, nil)
	defer srv.Close()
	p := NewPipeline(
		NewClient(srv.URL, "+1000"),
		config.SignalConfig{Account: "+1000", DMPolicy: "open", TextChunkLimit: 100},
		func(sessionID, content string, metadata map[string]string) {
			inbox <- capturedInput{sessionID: sessionID, content: content, metadata: metadata}
		},
		func() (<-chan Event, func()) {
			ch := make(chan Event)
			return ch, func() {}
		},
	)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Start(ctx) }()
	select {
	case got := <-inbox:
		cancel()
		<-done
		t.Fatalf("unexpected enqueue: %+v", got)
	case <-time.After(200 * time.Millisecond):
		cancel()
		if err := <-done; err == nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("expected canceled context, got %v", err)
		}
	}
}

func TestPipelineSendsResponse(t *testing.T) {
	calls := make(chan capturedCall, 2)
	server := newSignalServer(t, nil, calls)
	defer server.Close()
	events := make(chan Event, 1)
	p := NewPipeline(
		NewClient(server.URL, "+1000"),
		config.SignalConfig{Account: "+1000", TextChunkLimit: 100},
		func(sessionID, content string, metadata map[string]string) {},
		func() (<-chan Event, func()) {
			return events, func() {}
		},
	)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- p.Start(ctx)
	}()
	events <- Event{SessionID: "signal:dm:user-1", Text: "hello there"}
	call := waitCall(t, calls)
	cancel()
	if err := <-done; err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled context, got %v", err)
	}
	if call.method != "send" {
		t.Fatalf("method = %q", call.method)
	}
	if to := readEnvelopeRecipient(t, call.params); to != "user-1" {
		t.Fatalf("to = %q", to)
	}
	if call.params["message"].(string) != "hello there" {
		t.Fatalf("message = %v", call.params["message"])
	}
}

func TestPipelineRendersMentions(t *testing.T) {
	inbox := make(chan capturedInput, 1)
	env := &Envelope{
		SourceNumber: "+15559990000",
		SourceUUID:   "user-1",
		DataMessage: &DataMessage{
			Message: "hello everyone",
			Mentions: []Mention{
				{Start: 6, Length: 5, Name: "Alice"},
			},
		},
	}
	srv := newSignalServer(t, env, nil)
	defer srv.Close()
	p := NewPipeline(
		NewClient(srv.URL, "+1000"),
		config.SignalConfig{Account: "+1000", DMPolicy: "open", TextChunkLimit: 100},
		func(sessionID, content string, metadata map[string]string) {
			inbox <- capturedInput{sessionID: sessionID, content: content, metadata: metadata}
		},
		func() (<-chan Event, func()) {
			ch := make(chan Event)
			return ch, func() {}
		},
	)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Start(ctx) }()
	input := waitInput(t, inbox)
	cancel()
	if err := <-done; err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled context, got %v", err)
	}
	if input.content != "hello @Aliceone" {
		t.Fatalf("content = %q", input.content)
	}
}

func TestPipelineChunksLongResponse(t *testing.T) {
	calls := make(chan capturedCall, 10)
	srv := newSignalServer(t, nil, calls)
	defer srv.Close()
	ch := make(chan Event, 1)
	p := NewPipeline(
		NewClient(srv.URL, "+1000"),
		config.SignalConfig{Account: "+1000", TextChunkLimit: 8},
		func(sessionID, content string, metadata map[string]string) {},
		func() (<-chan Event, func()) {
			return ch, func() {}
		},
	)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Start(ctx) }()
	ch <- Event{SessionID: "signal:group:grp-1", Text: strings.Repeat("x", 25)}
	call1 := waitCall(t, calls)
	call2 := waitCall(t, calls)
	call3 := waitCall(t, calls)
	if call1.method != "send" || call2.method != "send" || call3.method != "send" {
		t.Fatalf("method mismatch: %s %s %s", call1.method, call2.method, call3.method)
	}
	if id := readGroupID(t, call1.params); id != "grp-1" {
		t.Fatalf("groupId = %q", id)
	}
	if len(call1.params["message"].(string)) > 8 || len(call2.params["message"].(string)) > 8 || len(call3.params["message"].(string)) > 8 {
		t.Fatalf("chunk too long: %+v", []string{call1.params["message"].(string), call2.params["message"].(string), call3.params["message"].(string)})
	}
	cancel()
	<-done
}
