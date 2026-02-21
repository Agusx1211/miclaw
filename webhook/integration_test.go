package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agusx1211/miclaw/config"
)

type enqueueCall struct {
	source   string
	content  string
	metadata map[string]string
}

type enqueueCapture struct {
	mu    sync.Mutex
	calls []enqueueCall
}

func (e *enqueueCapture) add(source, content string, metadata map[string]string) {
	e.mu.Lock()
	e.calls = append(e.calls, enqueueCall{source: source, content: content, metadata: metadata})
	e.mu.Unlock()
}

func (e *enqueueCapture) snapshot() []enqueueCall {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]enqueueCall, len(e.calls))
	copy(out, e.calls)
	return out
}

type runningWebhookServer struct {
	base  string
	srv   *Server
	calls *enqueueCapture
	done  chan error
}

func startWebhookServer(t *testing.T, cfg config.WebhookConfig) *runningWebhookServer {
	t.Helper()
	calls := &enqueueCapture{}
	srv := New(cfg, calls.add)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- srv.server.Serve(ln)
	}()
	base := "http://" + ln.Addr().String()
	waitForReady(t, base+"/health")
	return &runningWebhookServer{base: base, srv: srv, calls: calls, done: done}
}

func (r *runningWebhookServer) stop(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := r.srv.Stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}
	select {
	case err := <-r.done:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("serve: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for server stop")
	}
}

func waitForReady(t *testing.T, url string) {
	t.Helper()
	client := &http.Client{Timeout: 100 * time.Millisecond}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		res, err := client.Get(url)
		if err == nil {
			io.Copy(io.Discard, res.Body)
			res.Body.Close()
			if res.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("server not ready")
}

func signWebhook(payload, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(payload))
	return "sha256=" + hex.EncodeToString(h.Sum(nil))
}

func TestWebhookIntegrationUnsignedPostEnqueues(t *testing.T) {
	server := startWebhookServer(t, config.WebhookConfig{
		Listen: "127.0.0.1:0",
		Hooks: []config.WebhookDef{
			{ID: "unsigned", Path: "/hook", Format: "text"},
		},
	})
	defer server.stop(t)
	res, err := http.Post(server.base+"/hook", "text/plain", strings.NewReader("payload"))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("status=%d", res.StatusCode)
	}
	got := server.calls.snapshot()
	if len(got) != 1 || got[0].content != "[webhook:unsigned] payload" || got[0].metadata["id"] != "unsigned" {
		t.Fatalf("got=%+v", got)
	}
}

func TestWebhookIntegrationSignedValidPostEnqueues(t *testing.T) {
	server := startWebhookServer(t, config.WebhookConfig{
		Listen: "127.0.0.1:0",
		Hooks: []config.WebhookDef{
			{ID: "signed", Path: "/hook", Format: "text", Secret: "secret"},
		},
	})
	defer server.stop(t)
	body := "signed payload"
	req, err := http.NewRequest(http.MethodPost, server.base+"/hook", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Webhook-Signature", signWebhook(body, "secret"))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("status=%d", res.StatusCode)
	}
	got := server.calls.snapshot()
	if len(got) != 1 || got[0].content != "[webhook:signed] "+body {
		t.Fatalf("got=%+v", got)
	}
}

func TestWebhookIntegrationSignedInvalidPostRejected(t *testing.T) {
	server := startWebhookServer(t, config.WebhookConfig{
		Listen: "127.0.0.1:0",
		Hooks: []config.WebhookDef{
			{ID: "signed", Path: "/hook", Secret: "secret", Format: "text"},
		},
	})
	defer server.stop(t)
	req, err := http.NewRequest(http.MethodPost, server.base+"/hook", strings.NewReader("payload"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Webhook-Signature", "sha256=deadbeef")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d", res.StatusCode)
	}
	if got := server.calls.snapshot(); len(got) != 0 {
		t.Fatalf("len=%d", len(got))
	}
}

func TestWebhookIntegrationSignedMissingSignatureRejected(t *testing.T) {
	server := startWebhookServer(t, config.WebhookConfig{
		Listen: "127.0.0.1:0",
		Hooks: []config.WebhookDef{
			{ID: "signed", Path: "/hook", Secret: "secret", Format: "text"},
		},
	})
	defer server.stop(t)
	res, err := http.Post(server.base+"/hook", "text/plain", strings.NewReader("payload"))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d", res.StatusCode)
	}
	if got := server.calls.snapshot(); len(got) != 0 {
		t.Fatalf("len=%d", len(got))
	}
}

func TestWebhookIntegrationHealthEndpoint(t *testing.T) {
	server := startWebhookServer(t, config.WebhookConfig{
		Listen: "127.0.0.1:0",
	})
	defer server.stop(t)
	res, err := http.Get(server.base + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", res.StatusCode)
	}
	var got map[string]string
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got["status"] != "ok" {
		t.Fatalf("status=%q", got["status"])
	}
}

func TestWebhookIntegrationUnknownPathReturns404(t *testing.T) {
	server := startWebhookServer(t, config.WebhookConfig{
		Listen: "127.0.0.1:0",
	})
	defer server.stop(t)
	res, err := http.Get(server.base + "/unknown")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d", res.StatusCode)
	}
}

func TestWebhookIntegrationMultipleRapidPostsPreserveOrder(t *testing.T) {
	server := startWebhookServer(t, config.WebhookConfig{
		Listen: "127.0.0.1:0",
		Hooks: []config.WebhookDef{
			{ID: "stream", Path: "/hook", Format: "text"},
		},
	})
	defer server.stop(t)
	for i := 0; i < 10; i++ {
		body := "event-" + strconv.Itoa(i)
		res, err := http.Post(server.base+"/hook", "text/plain", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusAccepted {
			t.Fatalf("status=%d", res.StatusCode)
		}
	}
	got := server.calls.snapshot()
	if len(got) != 10 {
		t.Fatalf("len=%d", len(got))
	}
	for i := range got {
		want := "[webhook:stream] event-" + strconv.Itoa(i)
		if got[i].content != want {
			t.Fatalf("i=%d got=%q want=%q", i, got[i].content, want)
		}
	}
}

func TestWebhookIntegrationGracefulShutdownStopsServer(t *testing.T) {
	server := startWebhookServer(t, config.WebhookConfig{
		Listen: "127.0.0.1:0",
		Hooks: []config.WebhookDef{
			{ID: "stop", Path: "/hook", Format: "text"},
		},
	})
	res, err := http.Post(server.base+"/hook", "text/plain", strings.NewReader("before"))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("status=%d", res.StatusCode)
	}
	server.stop(t)
	_, err = http.Get(server.base+"/health")
	if err == nil {
		t.Fatal("expected request to fail after shutdown")
	}
}
