package signal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func newSignalServer(t *testing.T, env *Envelope) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/v1/events") {
			http.NotFound(w, r)
			return
		}
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
	srv := newSignalServer(t, env)
	defer srv.Close()
	p := NewPipeline(
		NewClient(srv.URL, "+1000"),
		config.SignalConfig{Account: "+1000", DMPolicy: "open", TextChunkLimit: 100},
		func(sessionID, content string, metadata map[string]string) {
			inbox <- capturedInput{sessionID: sessionID, content: content, metadata: metadata}
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
	srv := newSignalServer(t, env)
	defer srv.Close()
	p := NewPipeline(
		NewClient(srv.URL, "+1000"),
		config.SignalConfig{Account: "+1000", DMPolicy: "disabled", TextChunkLimit: 100},
		func(sessionID, content string, metadata map[string]string) {
			inbox <- capturedInput{sessionID: sessionID, content: content, metadata: metadata}
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
	srv := newSignalServer(t, env)
	defer srv.Close()
	p := NewPipeline(
		NewClient(srv.URL, "+1000"),
		config.SignalConfig{Account: "+1000", DMPolicy: "open", TextChunkLimit: 100},
		func(sessionID, content string, metadata map[string]string) {
			inbox <- capturedInput{sessionID: sessionID, content: content, metadata: metadata}
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
	srv := newSignalServer(t, env)
	defer srv.Close()
	p := NewPipeline(
		NewClient(srv.URL, "+1000"),
		config.SignalConfig{Account: "+1000", DMPolicy: "open", TextChunkLimit: 100},
		func(sessionID, content string, metadata map[string]string) {
			inbox <- capturedInput{sessionID: sessionID, content: content, metadata: metadata}
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

func TestPipelineReturnsErrorWhenEventStreamCloses(t *testing.T) {
	p := NewPipeline(
		NewClient("http://127.0.0.1:1", "+1000"),
		config.SignalConfig{Account: "+1000", DMPolicy: "open", TextChunkLimit: 100},
		func(sessionID, content string, metadata map[string]string) {},
	)
	err := p.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "signal events stream closed") {
		t.Fatalf("unexpected error: %v", err)
	}
}
