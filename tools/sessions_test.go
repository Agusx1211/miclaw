package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agusx1211/miclaw/model"
	"github.com/agusx1211/miclaw/store"
)

func openSessionsTestStore(t *testing.T) *store.SQLiteStore {
	t.Helper()
	p := filepath.Join(t.TempDir(), "sessions.db")
	s, err := store.OpenSQLite(p)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Fatalf("close sqlite: %v", err)
		}
	})
	return s
}

func createSession(t *testing.T, s *store.SQLiteStore, id, title string, at time.Time) *model.Session {
	t.Helper()
	v := &model.Session{
		ID:               id,
		ParentSessionID:  "",
		Title:            title,
		MessageCount:     0,
		PromptTokens:     10,
		CompletionTokens: 20,
		SummaryMessageID: "",
		Cost:             1.25,
		CreatedAt:        at,
		UpdatedAt:        at,
	}
	if err := s.SessionStore().Create(v); err != nil {
		t.Fatalf("create session: %v", err)
	}
	return v
}

func createTextMessage(t *testing.T, s *store.SQLiteStore, id, sessionID string, role model.Role, text string, at time.Time) {
	t.Helper()
	msg := &model.Message{
		ID:        id,
		SessionID: sessionID,
		Role:      role,
		Parts:     []model.MessagePart{model.TextPart{Text: text}},
		CreatedAt: at,
	}
	if err := s.MessageStore().Create(msg); err != nil {
		t.Fatalf("create message: %v", err)
	}
}

func runCall(t *testing.T, tl Tool, params map[string]any) (ToolResult, error) {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	call := model.ToolCallPart{Name: tl.Name(), Parameters: raw}
	return tl.Run(context.Background(), call)
}

func countLines(text string) int {
	if text == "" {
		return 0
	}
	return strings.Count(text, "\n") + 1
}

func TestSessionsListEmpty(t *testing.T) {
	s := openSessionsTestStore(t)
	tool := sessionsListTool(s.SessionStore())
	got, err := runCall(t, tool, map[string]any{})
	if err != nil {
		t.Fatalf("run sessions_list: %v", err)
	}
	if got.Content != "" {
		t.Fatalf("expected empty content, got %q", got.Content)
	}
}

func TestSessionsListWithPagination(t *testing.T) {
	s := openSessionsTestStore(t)
	at := time.Date(2026, 2, 21, 10, 0, 0, 0, time.UTC)
	for i := range 5 {
		id := "s" + string(rune('1'+i))
		createSession(t, s, id, "title-"+id, at.Add(time.Duration(i)*time.Minute))
	}

	tool := sessionsListTool(s.SessionStore())
	got, err := runCall(t, tool, map[string]any{"limit": 2, "offset": 1})
	if err != nil {
		t.Fatalf("run sessions_list: %v", err)
	}
	if !strings.Contains(got.Content, "s2") || !strings.Contains(got.Content, "s3") {
		t.Fatalf("missing paged sessions: %q", got.Content)
	}
	if strings.Contains(got.Content, "s1") || strings.Contains(got.Content, "s4") {
		t.Fatalf("unexpected sessions in page: %q", got.Content)
	}
}

func TestSessionsHistory(t *testing.T) {
	s := openSessionsTestStore(t)
	at := time.Date(2026, 2, 21, 11, 0, 0, 0, time.UTC)
	createSession(t, s, "s1", "history", at)
	createTextMessage(t, s, "m1", "s1", model.RoleUser, "hello", at.Add(time.Minute))
	msg := &model.Message{
		ID:        "m2",
		SessionID: "s1",
		Role:      model.RoleAssistant,
		Parts: []model.MessagePart{
			model.ToolCallPart{ID: "tc1", Name: "grep", Parameters: json.RawMessage(`{"pattern":"x"}`)},
		},
		CreatedAt: at.Add(2 * time.Minute),
	}
	if err := s.MessageStore().Create(msg); err != nil {
		t.Fatalf("create tool-call message: %v", err)
	}

	tool := sessionsHistoryTool(s.SessionStore(), s.MessageStore())
	got, err := runCall(t, tool, map[string]any{"session_id": "s1"})
	if err != nil {
		t.Fatalf("run sessions_history: %v", err)
	}
	if !strings.Contains(got.Content, "user\thello") {
		t.Fatalf("missing user message: %q", got.Content)
	}
	if !strings.Contains(got.Content, "assistant\t[tool_call name=grep id=tc1]") {
		t.Fatalf("missing tool call summary: %q", got.Content)
	}
}

func TestSessionsSend(t *testing.T) {
	s := openSessionsTestStore(t)
	createSession(t, s, "s1", "send", time.Date(2026, 2, 21, 12, 0, 0, 0, time.UTC))

	tool := sessionsSendTool(s.SessionStore(), s.MessageStore())
	got, err := runCall(t, tool, map[string]any{"session_id": "s1", "message": "ping"})
	if err != nil {
		t.Fatalf("run sessions_send: %v", err)
	}
	if !strings.Contains(got.Content, "message sent to session s1") {
		t.Fatalf("unexpected tool response: %q", got.Content)
	}
	msgs, err := s.MessageStore().ListBySession("s1", 10, 0)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Role != model.RoleUser {
		t.Fatalf("expected user role, got %q", msgs[0].Role)
	}
	if msgs[0].Parts[0].(model.TextPart).Text != "ping" {
		t.Fatalf("unexpected message text: %#v", msgs[0].Parts[0])
	}
}

func TestSessionsStatus(t *testing.T) {
	s := openSessionsTestStore(t)
	at := time.Date(2026, 2, 21, 13, 0, 0, 0, time.UTC)
	session := createSession(t, s, "s1", "status", at)
	session.PromptTokens = 111
	session.CompletionTokens = 222
	session.Cost = 3.5
	session.UpdatedAt = at.Add(time.Hour)
	if err := s.SessionStore().Update(session); err != nil {
		t.Fatalf("update session: %v", err)
	}
	createTextMessage(t, s, "m1", "s1", model.RoleUser, "a", at.Add(2*time.Hour))
	createTextMessage(t, s, "m2", "s1", model.RoleAssistant, "b", at.Add(3*time.Hour))

	tool := sessionsStatusTool(s.SessionStore(), s.MessageStore())
	got, err := runCall(t, tool, map[string]any{"session_id": "s1"})
	if err != nil {
		t.Fatalf("run sessions_status: %v", err)
	}
	if !strings.Contains(got.Content, "id=s1") {
		t.Fatalf("missing id: %q", got.Content)
	}
	if !strings.Contains(got.Content, "title=status") {
		t.Fatalf("missing title: %q", got.Content)
	}
	if !strings.Contains(got.Content, "message_count=2") {
		t.Fatalf("missing message count: %q", got.Content)
	}
	if !strings.Contains(got.Content, "prompt_tokens=111") {
		t.Fatalf("missing prompt tokens: %q", got.Content)
	}
	if !strings.Contains(got.Content, "completion_tokens=222") {
		t.Fatalf("missing completion tokens: %q", got.Content)
	}
	if !strings.Contains(got.Content, "cost=3.500000") {
		t.Fatalf("missing cost: %q", got.Content)
	}
	if !strings.Contains(got.Content, "updated_at="+session.UpdatedAt.Format(time.RFC3339Nano)) {
		t.Fatalf("missing updated timestamp: %q", got.Content)
	}
}

func TestAgentsList(t *testing.T) {
	tool := agentsListTool("model-x", func() bool { return false })
	got, err := runCall(t, tool, map[string]any{})
	if err != nil {
		t.Fatalf("run agents_list: %v", err)
	}
	if !strings.Contains(got.Content, "name=main") {
		t.Fatalf("missing agent name: %q", got.Content)
	}
	if !strings.Contains(got.Content, "model=model-x") {
		t.Fatalf("missing model: %q", got.Content)
	}
	if !strings.Contains(got.Content, "status=idle") {
		t.Fatalf("missing status: %q", got.Content)
	}
}

func TestSessionsHistoryNonexistent(t *testing.T) {
	s := openSessionsTestStore(t)
	tool := sessionsHistoryTool(s.SessionStore(), s.MessageStore())
	_, err := runCall(t, tool, map[string]any{"session_id": "missing"})
	if err == nil {
		t.Fatal("expected error for missing session")
	}
}

func TestSessionsListDefaults(t *testing.T) {
	s := openSessionsTestStore(t)
	at := time.Date(2026, 2, 21, 14, 0, 0, 0, time.UTC)
	for i := range 25 {
		id := "s" + string(rune('A'+i))
		createSession(t, s, id, id, at.Add(time.Duration(i)*time.Minute))
	}

	tool := sessionsListTool(s.SessionStore())
	got, err := runCall(t, tool, map[string]any{})
	if err != nil {
		t.Fatalf("run sessions_list: %v", err)
	}
	if countLines(got.Content) != 20 {
		t.Fatalf("expected 20 lines by default, got %d", countLines(got.Content))
	}
	if !strings.Contains(got.Content, "sA") {
		t.Fatalf("expected first page content: %q", got.Content)
	}
	if strings.Contains(got.Content, "sU") {
		t.Fatalf("unexpected overflow beyond default limit: %q", got.Content)
	}
}
