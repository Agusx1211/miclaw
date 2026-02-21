//go:build integration

package agent_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ag "github.com/agusx1211/miclaw/agent"
	"github.com/agusx1211/miclaw/config"
	"github.com/agusx1211/miclaw/provider"
	"github.com/agusx1211/miclaw/store"
	"github.com/agusx1211/miclaw/tooling"
	"github.com/agusx1211/miclaw/tools"
)

const integrationModel = "google/gemini-2.0-flash-001"

type integrationEnv struct {
	agent     *ag.Agent
	store     *store.SQLiteStore
	workspace string
}

func loadAPIKey(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Skip("DEV_VARS.md not found or empty")
	}
	devVarsPath := filepath.Join(wd, "..", "DEV_VARS.md")
	f, err := os.Open(devVarsPath)
	if err != nil {
		t.Skip("DEV_VARS.md not found or empty")
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
				t.Skip("DEV_VARS.md not found or empty")
			}
			return key
		}
	}
	t.Skip("DEV_VARS.md not found or empty")
	return ""
}

func newIntegrationEnv(t *testing.T) *integrationEnv {
	t.Helper()
	apiKey := loadAPIKey(t)
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	dbPath := filepath.Join(root, "agent.db")
	s, err := store.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Fatalf("close sqlite: %v", err)
		}
	})
	prov := provider.NewOpenRouter(config.ProviderConfig{APIKey: apiKey, Model: integrationModel})
	var a *ag.Agent
	allTools := tools.MainAgentTools(tools.MainToolDeps{
		Sessions: s.SessionStore(),
		Messages: s.MessageStore(),
		Provider: prov,
		Model:    integrationModel,
		IsActive: func() bool {
			if a == nil {
				return false
			}
			return a.IsActive()
		},
	})
	a = ag.NewAgent(s.SessionStore(), s.MessageStore(), onlyFilesystemTools(allTools), prov)
	return &integrationEnv{agent: a, store: s, workspace: workspace}
}

func onlyFilesystemTools(all []tooling.Tool) []tooling.Tool {
	allow := map[string]bool{
		"read":  true,
		"write": true,
		"edit":  true,
		"patch": true,
		"grep":  true,
		"glob":  true,
		"ls":    true,
		"exec":  true,
	}
	out := make([]tooling.Tool, 0, len(all))
	for _, tool := range all {
		if allow[tool.Name()] {
			out = append(out, tool)
		}
	}
	return out
}

func runAndWaitForResponse(t *testing.T, a *ag.Agent, sessionID, prompt string) *ag.Message {
	t.Helper()
	events, unsub := a.Events().Subscribe()
	defer unsub()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	err := a.RunOnce(ctx, ag.Input{SessionID: sessionID, Content: prompt, Source: ag.SourceAPI})
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	return waitResponseEvent(t, events, sessionID, 5*time.Second)
}

func waitResponseEvent(t *testing.T, events <-chan ag.AgentEvent, sessionID string, timeout time.Duration) *ag.Message {
	t.Helper()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case ev := <-events:
			if ev.SessionID != sessionID {
				continue
			}
			if ev.Type == ag.EventError {
				t.Fatalf("unexpected event error: %v", ev.Error)
			}
			if ev.Type == ag.EventResponse {
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

func waitResponseCount(t *testing.T, events <-chan ag.AgentEvent, sessionID string, n int, timeout time.Duration) []*ag.Message {
	t.Helper()
	msgs := make([]*ag.Message, 0, n)
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for len(msgs) < n {
		select {
		case ev := <-events:
			if ev.SessionID != sessionID {
				continue
			}
			if ev.Type == ag.EventError {
				t.Fatalf("unexpected event error: %v", ev.Error)
			}
			if ev.Type == ag.EventResponse {
				if ev.Message == nil {
					t.Fatal("response event without message")
				}
				msgs = append(msgs, ev.Message)
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for %d responses (got %d)", n, len(msgs))
		}
	}
	return msgs
}

func messageText(msg *ag.Message) string {
	parts := make([]string, 0, len(msg.Parts))
	for _, part := range msg.Parts {
		txt, ok := part.(ag.TextPart)
		if ok && txt.Text != "" {
			parts = append(parts, txt.Text)
		}
	}
	return strings.Join(parts, "")
}

func toolMessageCount(msgs []*ag.Message) int {
	n := 0
	for _, msg := range msgs {
		if msg.Role == ag.RoleTool {
			n++
		}
	}
	return n
}

func TestIntegrationSimplePrompt(t *testing.T) {
	env := newIntegrationEnv(t)
	sessionID := "integration-simple"
	msg := runAndWaitForResponse(t, env.agent, sessionID, "What is 2+2? Reply with only the number.")
	if !strings.Contains(messageText(msg), "4") {
		t.Fatalf("expected response to contain 4, got: %q", messageText(msg))
	}
	session, err := env.store.SessionStore().Get(sessionID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if session.MessageCount < 2 {
		t.Fatalf("expected at least 2 messages, got %d", session.MessageCount)
	}
	if session.PromptTokens+session.CompletionTokens == 0 {
		t.Fatalf("expected token usage to be tracked, got prompt=%d completion=%d", session.PromptTokens, session.CompletionTokens)
	}
}

func TestIntegrationToolUse(t *testing.T) {
	env := newIntegrationEnv(t)
	sessionID := "integration-tool-use"
	prompt := fmt.Sprintf(
		"Create a file called test.txt with the content hello world in %s, then read it back and tell me what it says. Use write and read tools.",
		env.workspace,
	)
	msg := runAndWaitForResponse(t, env.agent, sessionID, prompt)
	b, err := os.ReadFile(filepath.Join(env.workspace, "test.txt"))
	if err != nil {
		t.Fatalf("read created file: %v", err)
	}
	if !strings.Contains(string(b), "hello world") {
		t.Fatalf("expected file content to contain hello world, got: %q", string(b))
	}
	if !strings.Contains(strings.ToLower(messageText(msg)), "hello world") {
		t.Fatalf("expected response to mention hello world, got: %q", messageText(msg))
	}
	session, err := env.store.SessionStore().Get(sessionID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if session.MessageCount <= 2 {
		t.Fatalf("expected message_count > 2 for tool use, got %d", session.MessageCount)
	}
}

func TestIntegrationMultiTurnToolUse(t *testing.T) {
	env := newIntegrationEnv(t)
	sessionID := "integration-multi-turn"
	content := "sequential content"
	targetName := "random_name_7f9d2b.txt"
	targetPath := filepath.Join(env.workspace, targetName)
	if err := os.WriteFile(targetPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(env.workspace, "note.log"), []byte("noise"), 0o644); err != nil {
		t.Fatalf("write note file: %v", err)
	}
	prompt := fmt.Sprintf(
		"List files in %s with ls. Then read the only .txt file you found and tell me exactly what it says. Call one tool at a time and wait for each result.",
		env.workspace,
	)
	msg := runAndWaitForResponse(t, env.agent, sessionID, prompt)
	if !strings.Contains(strings.ToLower(messageText(msg)), content) {
		t.Fatalf("expected response to contain file content %q, got: %q", content, messageText(msg))
	}
	msgs, err := env.store.MessageStore().ListBySession(sessionID, 100, 0)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if toolMessageCount(msgs) < 2 {
		t.Fatalf("expected multiple tool messages, got %d", toolMessageCount(msgs))
	}
}

func TestIntegrationCancellation(t *testing.T) {
	env := newIntegrationEnv(t)
	sessionID := "integration-cancel"
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- env.agent.RunOnce(ctx, ag.Input{
			SessionID: sessionID,
			Content:   "Explain the history of computing in exhaustive detail with many sections.",
			Source:    ag.SourceAPI,
		})
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context canceled, got %v", err)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("timed out waiting for canceled run")
	}
	if env.agent.IsActive() {
		t.Fatal("expected agent to be inactive after cancellation")
	}
}

func TestIntegrationQueueProcessing(t *testing.T) {
	env := newIntegrationEnv(t)
	sessionID := "integration-queue"
	events, unsub := env.agent.Events().Subscribe()
	defer unsub()
	defer env.agent.Cancel()
	env.agent.Enqueue(ag.Input{SessionID: sessionID, Content: "Reply with only alpha.", Source: ag.SourceAPI})
	env.agent.Enqueue(ag.Input{SessionID: sessionID, Content: "Reply with only beta.", Source: ag.SourceAPI})
	_ = waitResponseCount(t, events, sessionID, 2, 60*time.Second)
	session, err := env.store.SessionStore().Get(sessionID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if session.MessageCount < 4 {
		t.Fatalf("expected at least 4 messages after two queued prompts, got %d", session.MessageCount)
	}
}
