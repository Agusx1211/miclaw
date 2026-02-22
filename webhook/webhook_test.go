package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agusx1211/miclaw/config"
)

func TestWebhookEnqueuesMessage(t *testing.T) {
	t.Helper()
	cfg := config.WebhookConfig{
		Listen: ":0",
		Hooks: []config.WebhookDef{
			{ID: "alpha", Path: "/webhook", Format: "text"},
		},
	}
	var gotSource string
	var gotContent string
	var gotMetadata map[string]string
	server := New(cfg, func(source, content string, metadata map[string]string) {
		gotSource = source
		gotContent = content
		gotMetadata = metadata
	})
	ts := httptest.NewServer(server.server.Handler)
	defer ts.Close()

	req := strings.NewReader("hello")
	res, err := ts.Client().Post(ts.URL+"/webhook", "text/plain", req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("status=%d", res.StatusCode)
	}
	if gotSource != "webhook:alpha" {
		t.Fatalf("got source=%q", gotSource)
	}
	if gotContent != "hello" {
		t.Fatalf("got content=%q", gotContent)
	}
	if gotMetadata["id"] != "alpha" {
		t.Fatalf("got metadata=%v", gotMetadata)
	}
}

func TestWebhookHMACValid(t *testing.T) {
	t.Helper()
	cfg := config.WebhookConfig{
		Listen: ":0",
		Hooks: []config.WebhookDef{
			{ID: "signed", Path: "/webhook", Secret: "secret", Format: "text"},
		},
	}
	body := "secret payload"
	server := New(cfg, func(string, string, map[string]string) {})
	ts := httptest.NewServer(server.server.Handler)
	defer ts.Close()

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/webhook", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Webhook-Signature", sign(body, "secret"))
	res, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("status=%d", res.StatusCode)
	}
}

func TestWebhookHMACInvalid(t *testing.T) {
	t.Helper()
	cfg := config.WebhookConfig{
		Listen: ":0",
		Hooks: []config.WebhookDef{
			{ID: "signed", Path: "/webhook", Secret: "secret", Format: "text"},
		},
	}
	server := New(cfg, func(string, string, map[string]string) {})
	ts := httptest.NewServer(server.server.Handler)
	defer ts.Close()

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/webhook", strings.NewReader("secret payload"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Webhook-Signature", "sha256=deadbeef")
	res, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d", res.StatusCode)
	}
}

func TestWebhookHMACMissing(t *testing.T) {
	t.Helper()
	cfg := config.WebhookConfig{
		Listen: ":0",
		Hooks: []config.WebhookDef{
			{ID: "signed", Path: "/webhook", Secret: "secret", Format: "text"},
		},
	}
	server := New(cfg, func(string, string, map[string]string) {})
	ts := httptest.NewServer(server.server.Handler)
	defer ts.Close()

	res, err := ts.Client().Post(ts.URL+"/webhook", "text/plain", strings.NewReader("secret payload"))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d", res.StatusCode)
	}
}

func TestWebhookUnsigned(t *testing.T) {
	t.Helper()
	cfg := config.WebhookConfig{
		Listen: ":0",
		Hooks: []config.WebhookDef{
			{ID: "open", Path: "/webhook", Secret: "", Format: "text"},
		},
	}
	server := New(cfg, func(string, string, map[string]string) {})
	ts := httptest.NewServer(server.server.Handler)
	defer ts.Close()

	res, err := ts.Client().Post(ts.URL+"/webhook", "text/plain", strings.NewReader("public payload"))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("status=%d", res.StatusCode)
	}
}

func TestWebhookTextFormat(t *testing.T) {
	t.Helper()
	cfg := config.WebhookConfig{
		Listen: ":0",
		Hooks: []config.WebhookDef{
			{ID: "text", Path: "/webhook", Format: "text"},
		},
	}
	var got string
	server := New(cfg, func(_ string, content string, _ map[string]string) {
		got = content
	})
	ts := httptest.NewServer(server.server.Handler)
	defer ts.Close()

	res, err := ts.Client().Post(ts.URL+"/webhook", "text/plain", strings.NewReader("plain text"))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("status=%d", res.StatusCode)
	}
	if got != "plain text" {
		t.Fatalf("got=%q", got)
	}
}

func TestWebhookJSONFormat(t *testing.T) {
	t.Helper()
	cfg := config.WebhookConfig{
		Listen: ":0",
		Hooks: []config.WebhookDef{
			{ID: "json", Path: "/webhook", Format: "json"},
		},
	}
	var got string
	server := New(cfg, func(_ string, content string, _ map[string]string) {
		got = content
	})
	ts := httptest.NewServer(server.server.Handler)
	defer ts.Close()

	res, err := ts.Client().Post(ts.URL+"/webhook", "application/json", strings.NewReader(`{"x":1}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("status=%d", res.StatusCode)
	}
	if got != `{"x":1}` {
		t.Fatalf("got=%q", got)
	}
}

func TestHealthEndpoint(t *testing.T) {
	t.Helper()
	server := New(config.WebhookConfig{Listen: ":0"}, func(string, string, map[string]string) {})
	ts := httptest.NewServer(server.server.Handler)
	defer ts.Close()

	res, err := ts.Client().Get(ts.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", res.StatusCode)
	}
	if string(body) != `{"status":"ok"}
` {
		t.Fatalf("body=%q", body)
	}
}

func TestUnknownPath(t *testing.T) {
	t.Helper()
	server := New(config.WebhookConfig{Listen: ":0"}, func(string, string, map[string]string) {})
	ts := httptest.NewServer(server.server.Handler)
	defer ts.Close()

	res, err := ts.Client().Get(ts.URL + "/missing")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d", res.StatusCode)
	}
}

func TestNonPostMethod(t *testing.T) {
	t.Helper()
	cfg := config.WebhookConfig{
		Listen: ":0",
		Hooks: []config.WebhookDef{
			{ID: "x", Path: "/webhook", Format: "text"},
		},
	}
	server := New(cfg, func(string, string, map[string]string) {})
	ts := httptest.NewServer(server.server.Handler)
	defer ts.Close()

	res, err := ts.Client().Get(ts.URL + "/webhook")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d", res.StatusCode)
	}
}

func sign(payload, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
