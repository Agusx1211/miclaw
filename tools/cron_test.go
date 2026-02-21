package tools

import (
	"context"
	"encoding/json"
	"github.com/agusx1211/miclaw/model"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCronParseEveryMinute(t *testing.T) {
	expr, err := ParseCronExpr("* * * * *")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	times := []string{
		"2026-02-21T09:00:00Z",
		"2026-02-21T12:13:47Z",
		"2026-02-21T23:59:59Z",
	}
	for _, raw := range times {
		tm, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			t.Fatalf("parse time %q: %v", raw, err)
		}
		if !expr.Matches(tm) {
			t.Fatalf("expected %q to match", raw)
		}
	}
}

func TestCronParseSpecificTime(t *testing.T) {
	expr, err := ParseCronExpr("30 14 * * *")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	a := time.Date(2026, 2, 21, 14, 30, 0, 0, time.UTC)
	b := time.Date(2026, 2, 21, 14, 31, 0, 0, time.UTC)
	c := time.Date(2026, 2, 21, 13, 30, 0, 0, time.UTC)
	if !expr.Matches(a) || expr.Matches(b) || expr.Matches(c) {
		t.Fatalf("unexpected matches: a=%v b=%v c=%v", expr.Matches(a), expr.Matches(b), expr.Matches(c))
	}
}

func TestCronParseStepValue(t *testing.T) {
	expr, err := ParseCronExpr("*/5 * * * *")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !expr.Matches(time.Date(2026, 2, 21, 10, 25, 0, 0, time.UTC)) {
		t.Fatalf("expected 25 to match")
	}
	if expr.Matches(time.Date(2026, 2, 21, 10, 26, 0, 0, time.UTC)) {
		t.Fatalf("expected 26 not to match")
	}
}

func TestCronParseRange(t *testing.T) {
	expr, err := ParseCronExpr("1-5 * * * *")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !expr.Matches(time.Date(2026, 2, 21, 8, 3, 0, 0, time.UTC)) {
		t.Fatalf("expected minute 3 to match")
	}
	if expr.Matches(time.Date(2026, 2, 21, 8, 6, 0, 0, time.UTC)) {
		t.Fatalf("expected minute 6 not to match")
	}
}

func TestCronParseCommaList(t *testing.T) {
	expr, err := ParseCronExpr("0,15,30,45 * * * *")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !expr.Matches(time.Date(2026, 2, 21, 7, 30, 0, 0, time.UTC)) {
		t.Fatalf("expected minute 30 to match")
	}
	if expr.Matches(time.Date(2026, 2, 21, 7, 16, 0, 0, time.UTC)) {
		t.Fatalf("expected minute 16 not to match")
	}
}

func TestCronParseInvalid(t *testing.T) {
	if _, err := ParseCronExpr("* * *"); err == nil {
		t.Fatal("expected parse error")
	}
	if _, err := ParseCronExpr("60 * * * *"); err == nil {
		t.Fatal("expected parse error")
	}
	if _, err := ParseCronExpr("*/0 * * * *"); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestCronAddListRemove(t *testing.T) {
	s, err := NewScheduler(filepath.Join(t.TempDir(), "cron.db"))
	if err != nil {
		t.Fatalf("new scheduler: %v", err)
	}
	defer s.Close()

	id, err := s.AddJob("30 14 * * *", "ping")
	if err != nil {
		t.Fatalf("add job: %v", err)
	}
	jobs, err := s.ListJobs()
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("want 1 job, got %d", len(jobs))
	}
	if jobs[0].ID != id || jobs[0].Expression != "30 14 * * *" || jobs[0].Prompt != "ping" {
		t.Fatalf("unexpected job: %#v", jobs[0])
	}
	if err := s.RemoveJob(id); err != nil {
		t.Fatalf("remove job: %v", err)
	}
	jobs, err = s.ListJobs()
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("expected empty jobs after remove, got %d", len(jobs))
	}
}

func TestCronJobFires(t *testing.T) {
	tmp := t.TempDir()
	s, err := NewScheduler(filepath.Join(tmp, "cron.db"))
	if err != nil {
		t.Fatalf("new scheduler: %v", err)
	}
	var now atomic.Int64
	base := time.Date(2026, 2, 21, 10, 0, 0, 0, time.UTC)
	now.Store(base.UnixNano())
	s.now = func() time.Time { return time.Unix(0, now.Load()).UTC() }
	s.tick = 10 * time.Millisecond
	calls := make(chan string, 1)
	s.Start(context.Background(), func(sessionID, content string) {
		calls <- content
	})
	defer s.Stop()

	if _, err := s.AddJob("*/1 * * * *", "ping"); err != nil {
		t.Fatalf("add job: %v", err)
	}
	now.Store(base.Add(time.Minute).UnixNano())

	select {
	case got := <-calls:
		if strings.TrimSpace(got) != "ping" {
			t.Fatalf("unexpected prompt: %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected cron job to fire")
	}
}

func TestCronPersistence(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cron.db")
	s, err := NewScheduler(dbPath)
	if err != nil {
		t.Fatalf("new scheduler: %v", err)
	}
	id, err := s.AddJob("*/5 * * * *", "pulse")
	if err != nil {
		t.Fatalf("add job: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close scheduler: %v", err)
	}

	s, err = NewScheduler(dbPath)
	if err != nil {
		t.Fatalf("new scheduler: %v", err)
	}
	defer s.Close()
	jobs, err := s.ListJobs()
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].ID != id || jobs[0].Expression != "*/5 * * * *" || jobs[0].Prompt != "pulse" {
		t.Fatalf("unexpected persisted job: %#v", jobs[0])
	}
}

func TestCronToolListAndRemoveInTool(t *testing.T) {
	s, err := NewScheduler(filepath.Join(t.TempDir(), "cron.db"))
	if err != nil {
		t.Fatalf("new scheduler: %v", err)
	}
	defer s.Close()
	tool := CronTool(s)

	addRaw, _ := json.Marshal(map[string]any{"action": "add", "expression": "*/5 * * * *", "prompt": "hey"})
	add, err := tool.Run(context.Background(), rawToolCall(t, addRaw))
	if err != nil {
		t.Fatalf("run add: %v", err)
	}
	if add.IsError {
		t.Fatalf("add returned error: %q", add.Content)
	}

	listRaw, _ := json.Marshal(map[string]any{"action": "list"})
	list, err := tool.Run(context.Background(), rawToolCall(t, listRaw))
	if err != nil {
		t.Fatalf("run list: %v", err)
	}
	if !strings.Contains(list.Content, "*/5 * * * *") {
		t.Fatalf("expected job expression in list: %q", list.Content)
	}
}

func rawToolCall(t *testing.T, raw json.RawMessage) model.ToolCallPart {
	t.Helper()
	return model.ToolCallPart{Parameters: raw}
}
