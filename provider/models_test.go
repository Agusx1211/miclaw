package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agusx1211/miclaw/config"
)

func TestDiscoverModelIDsOpenRouter(t *testing.T) {
	gotAuth, gotReferer, gotTitle := "", "", ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/models" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		gotReferer = r.Header.Get("HTTP-Referer")
		gotTitle = r.Header.Get("X-Title")
		_, _ = w.Write([]byte(`{"data":[{"id":"z-model"},{"id":"a-model"},{"id":"a-model"}]}`))
	}))
	defer srv.Close()

	cfg := config.ProviderConfig{
		Backend: "openrouter",
		BaseURL: srv.URL,
		APIKey:  "sk-or-test",
	}
	models, err := DiscoverModelIDs(context.Background(), cfg)
	if err != nil {
		t.Fatalf("discover models: %v", err)
	}
	if len(models) != 2 || models[0] != "a-model" || models[1] != "z-model" {
		t.Fatalf("unexpected models: %#v", models)
	}
	if gotAuth != "Bearer sk-or-test" {
		t.Fatalf("authorization = %q", gotAuth)
	}
	if gotReferer != openRouterReferer || gotTitle != openRouterTitle {
		t.Fatalf("missing openrouter attribution headers")
	}
}

func TestDiscoverModelIDsLMStudio(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"qwen"},{"id":"llama"}]}`))
	}))
	defer srv.Close()

	cfg := config.ProviderConfig{
		Backend: "lmstudio",
		BaseURL: srv.URL,
	}
	models, err := DiscoverModelIDs(context.Background(), cfg)
	if err != nil {
		t.Fatalf("discover models: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("unexpected models: %#v", models)
	}
}

func TestDiscoverModelIDsHandlesStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("unauthorized"))
	}))
	defer srv.Close()

	cfg := config.ProviderConfig{
		Backend: "openrouter",
		BaseURL: srv.URL,
		APIKey:  "sk-x",
	}
	_, err := DiscoverModelIDs(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected status error")
	}
	if !strings.Contains(err.Error(), "status 401") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDiscoverModelIDsCodexFallsBackOnStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("unauthorized"))
	}))
	defer srv.Close()

	cfg := config.ProviderConfig{
		Backend: "codex",
		BaseURL: srv.URL,
		APIKey:  "sk-x",
	}
	models, err := DiscoverModelIDs(context.Background(), cfg)
	if err != nil {
		t.Fatalf("discover models: %v", err)
	}
	if len(models) == 0 {
		t.Fatal("expected fallback codex model list")
	}
	if models[0] != "gpt-5.3-codex" {
		t.Fatalf("unexpected fallback models: %#v", models)
	}
}

func TestDiscoverModelIDsCodexResponseShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"models":[{"slug":"z"},{"slug":"a"},{"slug":"a"}]}`))
	}))
	defer srv.Close()

	cfg := config.ProviderConfig{
		Backend: "codex",
		BaseURL: srv.URL,
		APIKey:  "sk-x",
	}
	models, err := DiscoverModelIDs(context.Background(), cfg)
	if err != nil {
		t.Fatalf("discover models: %v", err)
	}
	if len(models) != 2 || models[0] != "a" || models[1] != "z" {
		t.Fatalf("unexpected models: %#v", models)
	}
}
