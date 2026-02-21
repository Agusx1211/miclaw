//go:build integration

package agent_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ag "github.com/agusx1211/miclaw/agent"
	"github.com/agusx1211/miclaw/prompt"
)

func TestIntegrationCompaction(t *testing.T) {
	env := newIntegrationEnv(t)
	sessionID := "integration-compact"
	session := seedCompactionSession(t, env, sessionID, 6)
	session.PromptTokens = 24_000
	session.CompletionTokens = 7_000
	if err := env.store.SessionStore().Update(session); err != nil {
		t.Fatalf("update session token counts: %v", err)
	}
	before, err := env.store.MessageStore().CountBySession(sessionID)
	if err != nil {
		t.Fatalf("count messages before compact: %v", err)
	}
	compactWithTimeout(t, env.agent, session)
	msgs, err := env.store.MessageStore().ListBySession(sessionID, 20, 0)
	if err != nil {
		t.Fatalf("list compacted messages: %v", err)
	}
	if before < 10 {
		t.Fatalf("expected seeded conversation to have >=10 messages, got %d", before)
	}
	if len(msgs) > 2 || len(msgs) >= before {
		t.Fatalf("expected compaction to shrink messages from %d to <=2, got %d", before, len(msgs))
	}
	if summarySectionHits(messageText(msgs[0])) < 2 {
		t.Fatalf("expected compacted summary to include section-like headers, got: %q", messageText(msgs[0]))
	}
	reply := runAndWaitForResponse(t, env.agent, sessionID, "Reply with only compact-ok.")
	got := strings.ToLower(messageText(reply))
	if !strings.Contains(got, "compact-ok") && !strings.Contains(got, "compact ok") {
		t.Fatalf("expected post-compact response to contain compact-ok, got: %q", messageText(reply))
	}
}

func TestIntegrationCompactPreservesMemory(t *testing.T) {
	env := newIntegrationEnv(t)
	memPath := filepath.Join(env.workspace, "MEMORY.md")
	want := "Integration memory anchor: compaction must not mutate workspace files.\n"
	if err := os.WriteFile(memPath, []byte(want), 0o644); err != nil {
		t.Fatalf("write memory file: %v", err)
	}
	ws, err := prompt.LoadWorkspace(env.workspace)
	if err != nil {
		t.Fatalf("load workspace: %v", err)
	}
	env.agent.SetWorkspace(ws)
	session := seedCompactionSession(t, env, "integration-compact-memory", 5)
	compactWithTimeout(t, env.agent, session)
	got, err := os.ReadFile(memPath)
	if err != nil {
		t.Fatalf("read memory file after compact: %v", err)
	}
	if string(got) != want {
		t.Fatalf("memory file changed after compact: got %q want %q", string(got), want)
	}
}

func TestIntegrationCompactIdempotent(t *testing.T) {
	env := newIntegrationEnv(t)
	sessionID := "integration-compact-idempotent"
	session := seedCompactionSession(t, env, sessionID, 5)
	compactWithTimeout(t, env.agent, session)
	refreshed, err := env.store.SessionStore().Get(sessionID)
	if err != nil {
		t.Fatalf("get session after first compact: %v", err)
	}
	compactWithTimeout(t, env.agent, refreshed)
}

func compactWithTimeout(t *testing.T, a *ag.Agent, s *ag.Session) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := a.Compact(ctx, s); err != nil {
		t.Fatalf("compact: %v", err)
	}
}

func seedCompactionSession(t *testing.T, env *integrationEnv, sessionID string, turns int) *ag.Session {
	t.Helper()
	now := time.Now().UTC().Add(-10 * time.Minute)
	s := &ag.Session{ID: sessionID, CreatedAt: now, UpdatedAt: now}
	if err := env.store.SessionStore().Create(s); err != nil {
		t.Fatalf("create session: %v", err)
	}
	for i := 0; i < turns; i++ {
		u := fmt.Sprintf("Turn %d user: port openclaw to Go, keep code small, touched parser.go and store/sqlite.go, pending tests.", i+1)
		a := fmt.Sprintf("Turn %d assistant: implemented minimal change, verified behavior, next work includes compaction and integration tests.", i+1)
		createTextMessage(t, env, fmt.Sprintf("%s-u-%02d", sessionID, i), sessionID, ag.RoleUser, u, now.Add(time.Duration(i*2)*time.Second))
		createTextMessage(t, env, fmt.Sprintf("%s-a-%02d", sessionID, i), sessionID, ag.RoleAssistant, a, now.Add(time.Duration(i*2+1)*time.Second))
	}
	s.MessageCount = turns * 2
	s.UpdatedAt = now.Add(time.Duration(turns*2) * time.Second)
	if err := env.store.SessionStore().Update(s); err != nil {
		t.Fatalf("update seeded session: %v", err)
	}
	return s
}

func createTextMessage(t *testing.T, env *integrationEnv, id, sessionID string, role ag.Role, text string, when time.Time) {
	t.Helper()
	msg := &ag.Message{
		ID:        id,
		SessionID: sessionID,
		Role:      role,
		Parts:     []ag.MessagePart{ag.TextPart{Text: text}},
		CreatedAt: when,
	}
	if err := env.store.MessageStore().Create(msg); err != nil {
		t.Fatalf("create message %s: %v", id, err)
	}
}

func summarySectionHits(text string) int {
	lower := strings.ToLower(text)
	needles := []string{
		"primary goals",
		"timeline",
		"technical context",
		"files and code",
		"active work",
		"pending tasks",
		"next step",
	}
	hits := 0
	for _, needle := range needles {
		if strings.Contains(lower, needle) {
			hits++
		}
	}
	return hits
}
