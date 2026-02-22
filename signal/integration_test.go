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

type signalMock struct {
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
		events: make(chan Envelope, 16),
	}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/v1/events") {
			http.NotFound(w, r)
			return
		}
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

	p := NewPipeline(
		NewClient(mock.url(), "+5678"),
		defaultSignalConfig(),
		func(sessionID, content string, metadata map[string]string) {
			inbox <- inboundCapture{sessionID: sessionID, content: content, metadata: metadata}
		},
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
