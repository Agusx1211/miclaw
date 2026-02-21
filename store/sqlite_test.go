package store

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/agusx1211/miclaw/agent"
)

func openTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	p := filepath.Join(t.TempDir(), "store.db")
	s, err := OpenSQLite(p)
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

func makeSession(id string, at time.Time) *agent.Session {
	return &agent.Session{
		ID:               id,
		ParentSessionID:  "",
		Title:            "title-" + id,
		MessageCount:     0,
		PromptTokens:     1,
		CompletionTokens: 2,
		SummaryMessageID: "",
		Cost:             0.25,
		CreatedAt:        at,
		UpdatedAt:        at,
	}
}

func makeMessage(id, sessionID, text string, at time.Time) *agent.Message {
	return &agent.Message{
		ID:        id,
		SessionID: sessionID,
		Role:      agent.RoleUser,
		Parts: []agent.MessagePart{
			agent.TextPart{Text: text},
		},
		CreatedAt: at,
	}
}

func TestCreateAndGetSession(t *testing.T) {
	s := openTestStore(t)
	w := makeSession("s1", time.Date(2026, 2, 21, 10, 0, 0, 0, time.UTC))

	if err := s.Sessions.Create(w); err != nil {
		t.Fatalf("create session: %v", err)
	}
	g, err := s.Sessions.Get("s1")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if !reflect.DeepEqual(w, g) {
		t.Fatalf("session mismatch: want %#v got %#v", w, g)
	}
}

func TestCreateAndListMessages(t *testing.T) {
	s := openTestStore(t)
	if err := s.Sessions.Create(makeSession("s1", time.Date(2026, 2, 21, 10, 0, 0, 0, time.UTC))); err != nil {
		t.Fatalf("create session: %v", err)
	}
	m1 := makeMessage("m1", "s1", "one", time.Date(2026, 2, 21, 10, 1, 0, 0, time.UTC))
	m2 := makeMessage("m2", "s1", "two", time.Date(2026, 2, 21, 10, 2, 0, 0, time.UTC))
	if err := s.Messages.Create(m1); err != nil {
		t.Fatalf("create m1: %v", err)
	}
	if err := s.Messages.Create(m2); err != nil {
		t.Fatalf("create m2: %v", err)
	}

	l, err := s.Messages.ListBySession("s1", 10, 0)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(l) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(l))
	}
	if l[0].ID != "m1" || l[1].ID != "m2" {
		t.Fatalf("unexpected order: %q %q", l[0].ID, l[1].ID)
	}
}

func TestUpdateSessionTokenCounts(t *testing.T) {
	s := openTestStore(t)
	v := makeSession("s1", time.Date(2026, 2, 21, 11, 0, 0, 0, time.UTC))
	if err := s.Sessions.Create(v); err != nil {
		t.Fatalf("create session: %v", err)
	}
	v.PromptTokens = 123
	v.CompletionTokens = 456
	v.UpdatedAt = time.Date(2026, 2, 21, 11, 30, 0, 0, time.UTC)
	if err := s.Sessions.Update(v); err != nil {
		t.Fatalf("update session: %v", err)
	}

	g, err := s.Sessions.Get("s1")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if g.PromptTokens != 123 || g.CompletionTokens != 456 {
		t.Fatalf("unexpected token counts: %d %d", g.PromptTokens, g.CompletionTokens)
	}
}

func TestDeleteSessionCascadesToMessages(t *testing.T) {
	s := openTestStore(t)
	if err := s.Sessions.Create(makeSession("s1", time.Date(2026, 2, 21, 12, 0, 0, 0, time.UTC))); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := s.Messages.Create(makeMessage("m1", "s1", "x", time.Date(2026, 2, 21, 12, 1, 0, 0, time.UTC))); err != nil {
		t.Fatalf("create message: %v", err)
	}
	if err := s.Sessions.Delete("s1"); err != nil {
		t.Fatalf("delete session: %v", err)
	}

	n, err := s.Messages.CountBySession("s1")
	if err != nil {
		t.Fatalf("count messages: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 messages after delete, got %d", n)
	}
}

func TestReplaceSessionMessages(t *testing.T) {
	s := openTestStore(t)
	if err := s.Sessions.Create(makeSession("s1", time.Date(2026, 2, 21, 13, 0, 0, 0, time.UTC))); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := s.Messages.Create(makeMessage("old-1", "s1", "old", time.Date(2026, 2, 21, 13, 1, 0, 0, time.UTC))); err != nil {
		t.Fatalf("create old message: %v", err)
	}
	newMsgs := []*agent.Message{
		makeMessage("new-1", "s1", "new one", time.Date(2026, 2, 21, 13, 2, 0, 0, time.UTC)),
		makeMessage("new-2", "s1", "new two", time.Date(2026, 2, 21, 13, 3, 0, 0, time.UTC)),
	}
	if err := s.Messages.ReplaceSessionMessages("s1", newMsgs); err != nil {
		t.Fatalf("replace messages: %v", err)
	}

	l, err := s.Messages.ListBySession("s1", 10, 0)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(l) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(l))
	}
	if l[0].ID != "new-1" || l[1].ID != "new-2" {
		t.Fatalf("unexpected replace result: %q %q", l[0].ID, l[1].ID)
	}
}

func TestListSessionsPagination(t *testing.T) {
	s := openTestStore(t)
	for i := range 5 {
		id := fmt.Sprintf("s%d", i+1)
		at := time.Date(2026, 2, 21, 14, i, 0, 0, time.UTC)
		if err := s.Sessions.Create(makeSession(id, at)); err != nil {
			t.Fatalf("create session %s: %v", id, err)
		}
	}

	l0, err := s.Sessions.List(2, 0)
	if err != nil {
		t.Fatalf("list page 1: %v", err)
	}
	if len(l0) != 2 || l0[0].ID != "s1" || l0[1].ID != "s2" {
		t.Fatalf("unexpected page 1: %#v", l0)
	}
	l1, err := s.Sessions.List(2, 2)
	if err != nil {
		t.Fatalf("list page 2: %v", err)
	}
	if len(l1) != 2 || l1[0].ID != "s3" || l1[1].ID != "s4" {
		t.Fatalf("unexpected page 2: %#v", l1)
	}
}

func TestCountBySession(t *testing.T) {
	s := openTestStore(t)
	if err := s.Sessions.Create(makeSession("s1", time.Date(2026, 2, 21, 15, 0, 0, 0, time.UTC))); err != nil {
		t.Fatalf("create session: %v", err)
	}
	for i := range 3 {
		id := fmt.Sprintf("m%d", i+1)
		if err := s.Messages.Create(makeMessage(id, "s1", "x", time.Date(2026, 2, 21, 15, i, 0, 0, time.UTC))); err != nil {
			t.Fatalf("create message %s: %v", id, err)
		}
	}

	n, err := s.Messages.CountBySession("s1")
	if err != nil {
		t.Fatalf("count by session: %v", err)
	}
	if n != 3 {
		t.Fatalf("expected 3, got %d", n)
	}
}

func TestListBySessionEmpty(t *testing.T) {
	s := openTestStore(t)
	if err := s.Sessions.Create(makeSession("s1", time.Date(2026, 2, 21, 16, 0, 0, 0, time.UTC))); err != nil {
		t.Fatalf("create session: %v", err)
	}

	l, err := s.Messages.ListBySession("s1", 10, 0)
	if err != nil {
		t.Fatalf("list empty session: %v", err)
	}
	if l == nil {
		t.Fatal("expected empty slice, got nil")
	}
	if len(l) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(l))
	}
}

func TestGetNonexistentSession(t *testing.T) {
	s := openTestStore(t)
	_, err := s.Sessions.Get("missing")
	if err == nil {
		t.Fatal("expected error for missing session")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestGetNonexistentMessage(t *testing.T) {
	s := openTestStore(t)
	_, err := s.Messages.Get("missing")
	if err == nil {
		t.Fatal("expected error for missing message")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}
}
