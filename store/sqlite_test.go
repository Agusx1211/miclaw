package store

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/agusx1211/miclaw/model"
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

func makeMessage(id, text string, at time.Time) *model.Message {
	return &model.Message{
		ID:        id,
		Role:      model.RoleUser,
		Parts:     []model.MessagePart{model.TextPart{Text: text}},
		CreatedAt: at,
	}
}

func TestCreateAndGetMessage(t *testing.T) {
	s := openTestStore(t)
	want := makeMessage("m1", "one", time.Date(2026, 2, 21, 10, 0, 0, 0, time.UTC))
	if err := s.Messages.Create(want); err != nil {
		t.Fatalf("create message: %v", err)
	}
	got, err := s.Messages.Get("m1")
	if err != nil {
		t.Fatalf("get message: %v", err)
	}
	if got.ID != want.ID || got.Role != want.Role || got.CreatedAt != want.CreatedAt {
		t.Fatalf("message mismatch: want %#v got %#v", want, got)
	}
	part := got.Parts[0].(model.TextPart)
	if part.Text != "one" {
		t.Fatalf("unexpected text part: %#v", got.Parts[0])
	}
}

func TestListMessages(t *testing.T) {
	s := openTestStore(t)
	base := time.Date(2026, 2, 21, 11, 0, 0, 0, time.UTC)
	for i, text := range []string{"one", "two", "three"} {
		if err := s.Messages.Create(makeMessage(fmt.Sprintf("m%d", i+1), text, base.Add(time.Duration(i)*time.Minute))); err != nil {
			t.Fatalf("create message %d: %v", i, err)
		}
	}
	got, err := s.Messages.List(10, 0)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(got))
	}
	if got[0].ID != "m1" || got[1].ID != "m2" || got[2].ID != "m3" {
		t.Fatalf("unexpected order: %q %q %q", got[0].ID, got[1].ID, got[2].ID)
	}
	paged, err := s.Messages.List(1, 1)
	if err != nil {
		t.Fatalf("list paged: %v", err)
	}
	if len(paged) != 1 || paged[0].ID != "m2" {
		t.Fatalf("unexpected paged result: %#v", paged)
	}
}

func TestCountMessages(t *testing.T) {
	s := openTestStore(t)
	for i := range 3 {
		if err := s.Messages.Create(makeMessage(fmt.Sprintf("m%d", i+1), "x", time.Date(2026, 2, 21, 12, i, 0, 0, time.UTC))); err != nil {
			t.Fatalf("create message %d: %v", i, err)
		}
	}
	n, err := s.Messages.Count()
	if err != nil {
		t.Fatalf("count messages: %v", err)
	}
	if n != 3 {
		t.Fatalf("expected count 3, got %d", n)
	}
}

func TestReplaceAllMessages(t *testing.T) {
	s := openTestStore(t)
	if err := s.Messages.Create(makeMessage("old", "old", time.Date(2026, 2, 21, 13, 0, 0, 0, time.UTC))); err != nil {
		t.Fatalf("create old message: %v", err)
	}
	repl := []*model.Message{
		makeMessage("new-1", "new one", time.Date(2026, 2, 21, 13, 1, 0, 0, time.UTC)),
		makeMessage("new-2", "new two", time.Date(2026, 2, 21, 13, 2, 0, 0, time.UTC)),
	}
	if err := s.Messages.ReplaceAll(repl); err != nil {
		t.Fatalf("replace all: %v", err)
	}
	got, err := s.Messages.List(10, 0)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(got) != 2 || got[0].ID != "new-1" || got[1].ID != "new-2" {
		t.Fatalf("unexpected replace result: %#v", got)
	}
}

func TestDeleteAllMessages(t *testing.T) {
	s := openTestStore(t)
	if err := s.Messages.Create(makeMessage("m1", "one", time.Date(2026, 2, 21, 14, 0, 0, 0, time.UTC))); err != nil {
		t.Fatalf("create message: %v", err)
	}
	if err := s.Messages.DeleteAll(); err != nil {
		t.Fatalf("delete all: %v", err)
	}
	n, err := s.Messages.Count()
	if err != nil {
		t.Fatalf("count messages: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 messages, got %d", n)
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
