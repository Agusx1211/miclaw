//go:build integration

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agusx1211/miclaw/agent"
	"github.com/agusx1211/miclaw/config"
)

const integrationModel = "google/gemini-2.0-flash-001"

func loadAPIKey(t *testing.T) string {
	t.Helper()
	wd, _ := os.Getwd()
	devVarsPath := filepath.Join(wd, "..", "..", "DEV_VARS.md")
	f, err := os.Open(devVarsPath)
	if err != nil {
		t.Skip("DEV_VARS.md not found")
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		trimmed := strings.TrimPrefix(line, "export ")
		if strings.HasPrefix(trimmed, "OPENROUTER_API_KEY=") {
			key := strings.TrimPrefix(trimmed, "OPENROUTER_API_KEY=")
			key = strings.Trim(key, "\"'")
			if key == "" {
				t.Skip("empty API key")
			}
			return key
		}
	}
	t.Skip("OPENROUTER_API_KEY not found")
	return ""
}

func TestIntegrationSystemStartupAndVersion(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run([]string{"-version"}, &stdout, &stderr); err != nil {
		t.Fatalf("run version: %v", err)
	}
	got := strings.TrimSpace(stdout.String())
	if !strings.HasPrefix(got, "miclaw ") {
		t.Fatalf("version output = %q", got)
	}
}

func TestIntegrationWebhookToAgentResponse(t *testing.T) {
	root := t.TempDir()
	workspace := writeWorkspace(t, root)
	statePath := filepath.Join(root, "state")
	listen := reserveListenAddr(t)
	cfgPath := writeConfig(t, root, config.Config{
		Provider:  config.ProviderConfig{Backend: "openrouter", APIKey: loadAPIKey(t), Model: integrationModel},
		Webhook:   config.WebhookConfig{Enabled: true, Listen: listen, Hooks: []config.WebhookDef{{ID: "test", Path: "/test", Format: "text"}}},
		Signal:    config.SignalConfig{Enabled: false},
		Memory:    config.MemoryConfig{Enabled: false},
		Sandbox:   config.SandboxConfig{Enabled: false},
		Workspace: workspace,
		StatePath: statePath,
	})
	deps, err := initRuntime(cfgPath)
	if err != nil {
		t.Fatalf("init runtime: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 2)
	var wg sync.WaitGroup
	var stderr bytes.Buffer
	startMemorySync(ctx, deps, &stderr)
	if err := startScheduler(ctx, deps); err != nil {
		t.Fatalf("start scheduler: %v", err)
	}
	startSignalPipeline(ctx, deps, &wg, errCh)
	startWebhookServer(ctx, deps, &wg, errCh)
	waitForWebhookReady(t, "http://"+listen+"/health")

	events, unsubscribe := deps.agent.Events().Subscribe()
	defer unsubscribe()
	res, err := http.Post("http://"+listen+"/test", "text/plain", strings.NewReader("What is 2+2?"))
	if err != nil {
		t.Fatalf("post webhook: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("post status = %d", res.StatusCode)
	}
	msg := waitForResponseEvent(t, events, errCh, "webhook:test", 60*time.Second)
	if strings.TrimSpace(messageText(msg)) == "" {
		t.Fatal("agent response text is empty")
	}

	shutdown(deps, cancel, &wg, &stderr)
	if !strings.Contains(stderr.String(), "shutdown complete") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestIntegrationGracefulShutdown(t *testing.T) {
	root := t.TempDir()
	workspace := writeWorkspace(t, root)
	cfgPath := writeConfig(t, root, config.Config{
		Provider:  config.ProviderConfig{Backend: "openrouter", APIKey: loadAPIKey(t), Model: integrationModel},
		Webhook:   config.WebhookConfig{Enabled: false},
		Signal:    config.SignalConfig{Enabled: false},
		Memory:    config.MemoryConfig{Enabled: false},
		Sandbox:   config.SandboxConfig{Enabled: false},
		Workspace: workspace,
		StatePath: filepath.Join(root, "state"),
	})
	deps, err := initRuntime(cfgPath)
	if err != nil {
		t.Fatalf("init runtime: %v", err)
	}
	if _, err := deps.sqlStore.SessionStore().List(1, 0); err != nil {
		t.Fatalf("list sessions before shutdown: %v", err)
	}

	_, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	var stderr bytes.Buffer
	shutdown(deps, cancel, &wg, &stderr)
	if _, err := deps.sqlStore.SessionStore().List(1, 0); err == nil {
		t.Fatal("expected list sessions to fail after shutdown")
	}
	if !strings.Contains(stderr.String(), "shutdown complete") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func writeWorkspace(t *testing.T, root string) string {
	t.Helper()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "SOUL.md"), []byte("You are a helpful assistant. Answer concisely."), 0o644); err != nil {
		t.Fatalf("write SOUL.md: %v", err)
	}
	return workspace
}

func writeConfig(t *testing.T, root string, cfg config.Config) string {
	t.Helper()
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	path := filepath.Join(root, "config.json")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func reserveListenAddr(t *testing.T) string {
	t.Helper()
	for p := 19876; p <= 19910; p++ {
		addr := fmt.Sprintf("127.0.0.1:%d", p)
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			continue
		}
		if err := ln.Close(); err != nil {
			t.Fatalf("close listener: %v", err)
		}
		return addr
	}
	t.Fatal("no free local port in test range")
	return ""
}

func waitForWebhookReady(t *testing.T, healthURL string) {
	t.Helper()
	client := &http.Client{Timeout: 200 * time.Millisecond}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		res, err := client.Get(healthURL)
		if err == nil {
			_, _ = io.Copy(io.Discard, res.Body)
			_ = res.Body.Close()
			if res.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("webhook not ready: %s", healthURL)
}

func waitForResponseEvent(t *testing.T, events <-chan agent.AgentEvent, errCh <-chan error, sessionID string, timeout time.Duration) *agent.Message {
	t.Helper()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case err := <-errCh:
			t.Fatalf("runtime error: %v", err)
		case ev := <-events:
			if ev.SessionID != sessionID {
				continue
			}
			if ev.Type == agent.EventError {
				t.Fatalf("agent event error: %v", ev.Error)
			}
			if ev.Type == agent.EventResponse {
				if ev.Message == nil {
					t.Fatal("response event without message")
				}
				return ev.Message
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for response event for session %s", sessionID)
		}
	}
}
