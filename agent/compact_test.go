package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/agusx1211/miclaw/provider"
)

func TestCleanHistoryFillsMissingToolResponses(t *testing.T) {
	sessionID := "s1"
	msgs := []*Message{
		{ID: "u1", SessionID: sessionID, Role: RoleUser, Parts: []MessagePart{TextPart{Text: "start"}}, CreatedAt: time.Date(2026, 2, 21, 0, 0, 0, 0, time.UTC)},
		{ID: "a1", SessionID: sessionID, Role: RoleAssistant, Parts: []MessagePart{
			ToolCallPart{ID: "call-1", Name: "echo", Parameters: json.RawMessage(`{}`)},
		}, CreatedAt: time.Date(2026, 2, 21, 0, 0, 1, 0, time.UTC)},
	}

	cleaned := cleanHistory(msgs)
	if len(cleaned) != 4 {
		t.Fatalf("expected 4 messages after cleanup, got %d", len(cleaned))
	}
	result := cleaned[2]
	part, ok := result.Parts[0].(ToolResultPart)
	if !ok {
		t.Fatalf("expected tool result filler, got %#v", result.Parts)
	}
	if part.Content != "Tool no response" || !part.IsError {
		t.Fatalf("unexpected filler tool result: %#v", part)
	}
	last := cleaned[3]
	if last.Role != RoleAssistant {
		t.Fatalf("expected assistant followup, got %q", last.Role)
	}
	text, ok := last.Parts[0].(TextPart)
	if !ok || text.Text != "Understood." {
		t.Fatalf("unexpected assistant followup: %#v", last.Parts)
	}
}

func TestCleanHistoryRemovesOrphanedToolResults(t *testing.T) {
	sessionID := "s1"
	msgs := []*Message{
		{ID: "u1", SessionID: sessionID, Role: RoleUser, Parts: []MessagePart{TextPart{Text: "hello"}}, CreatedAt: time.Date(2026, 2, 21, 0, 0, 0, 0, time.UTC)},
		{ID: "t1", SessionID: sessionID, Role: RoleTool, Parts: []MessagePart{
			ToolResultPart{ToolCallID: "missing", Content: "nope", IsError: true},
		}, CreatedAt: time.Date(2026, 2, 21, 0, 0, 1, 0, time.UTC)},
	}

	cleaned := cleanHistory(msgs)
	if len(cleaned) != 1 {
		t.Fatalf("expected one message after cleanup, got %d", len(cleaned))
	}
	if cleaned[0].Role != RoleUser {
		t.Fatalf("expected user message retained, got %q", cleaned[0].Role)
	}
}

func TestCleanHistoryValidSequence(t *testing.T) {
	sessionID := "s1"
	msgs := []*Message{
		{ID: "u1", SessionID: sessionID, Role: RoleUser, Parts: []MessagePart{TextPart{Text: "start"}}, CreatedAt: time.Date(2026, 2, 21, 0, 0, 0, 0, time.UTC)},
		{ID: "a1", SessionID: sessionID, Role: RoleAssistant, Parts: []MessagePart{ToolCallPart{ID: "call-1", Name: "echo", Parameters: json.RawMessage(`{}`)}}, CreatedAt: time.Date(2026, 2, 21, 0, 0, 1, 0, time.UTC)},
		{ID: "t1", SessionID: sessionID, Role: RoleTool, Parts: []MessagePart{
			ToolResultPart{ToolCallID: "call-1", Content: "ok", IsError: false},
		}, CreatedAt: time.Date(2026, 2, 21, 0, 0, 2, 0, time.UTC)},
		{ID: "u2", SessionID: sessionID, Role: RoleUser, Parts: []MessagePart{TextPart{Text: "next"}}, CreatedAt: time.Date(2026, 2, 21, 0, 0, 3, 0, time.UTC)},
	}

	cleaned := cleanHistory(msgs)
	if len(cleaned) != len(msgs) {
		t.Fatalf("expected %d messages after cleanup, got %d", len(msgs), len(cleaned))
	}
	for i := range msgs {
		if cleaned[i].ID != msgs[i].ID {
			t.Fatalf("unexpected message at index %d: %q", i, cleaned[i].ID)
		}
	}
}

func TestCompactSummarizesAndReplaces(t *testing.T) {
	s := openAgentStore(t)
	now := time.Date(2026, 2, 21, 0, 0, 0, 0, time.UTC)
	session := &Session{ID: "s1", CreatedAt: now, UpdatedAt: now}
	if err := s.Sessions.Create(session); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := s.Messages.Create(&Message{ID: "u1", SessionID: "s1", Role: RoleUser, Parts: []MessagePart{TextPart{Text: "first request"}}, CreatedAt: now}); err != nil {
		t.Fatalf("create message: %v", err)
	}
	if err := s.Messages.Create(&Message{ID: "a1", SessionID: "s1", Role: RoleAssistant, Parts: []MessagePart{TextPart{Text: "reply"}}, CreatedAt: now}); err != nil {
		t.Fatalf("create message: %v", err)
	}

	p := &scriptedProvider{
		streams: []streamScript{
			eventStream(
				provider.ProviderEvent{Type: provider.EventContentDelta, Delta: "summary"},
				provider.ProviderEvent{Type: provider.EventComplete, Usage: &provider.UsageInfo{PromptTokens: 15, CompletionTokens: 5}},
			),
			eventStream(
				provider.ProviderEvent{Type: provider.EventComplete, Usage: &provider.UsageInfo{PromptTokens: 12, CompletionTokens: 4}},
			),
		},
	}
	a := NewAgent(s.SessionStore(), s.MessageStore(), nil, p)
	if err := a.Compact(context.Background(), session); err != nil {
		t.Fatalf("compact: %v", err)
	}

	msgs, err := s.Messages.ListBySession("s1", 10, 0)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected compacted history to have one message, got %d", len(msgs))
	}
	if msgs[0].Role != RoleUser {
		t.Fatalf("expected summary to be a user message, got %q", msgs[0].Role)
	}
	if got := messageText(msgs[0]); got != "summary\n\nLast request from user was: first request" {
		t.Fatalf("unexpected compacted summary: %q", got)
	}
	ref, err := s.Sessions.Get("s1")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if ref.PromptTokens != 12 || ref.CompletionTokens != 4 {
		t.Fatalf("unexpected token recount: prompt=%d completion=%d", ref.PromptTokens, ref.CompletionTokens)
	}
	if ref.SummaryMessageID != msgs[0].ID {
		t.Fatalf("expected summary message id saved, got %q want %q", ref.SummaryMessageID, msgs[0].ID)
	}
	if ref.MessageCount != 1 {
		t.Fatalf("expected message count reset to 1, got %d", ref.MessageCount)
	}
}

func TestCompactPreservesLastUserIntent(t *testing.T) {
	s := openAgentStore(t)
	now := time.Date(2026, 2, 21, 0, 0, 0, 0, time.UTC)
	session := &Session{ID: "s1", CreatedAt: now, UpdatedAt: now}
	if err := s.Sessions.Create(session); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := s.Messages.Create(&Message{ID: "u1", SessionID: "s1", Role: RoleUser, Parts: []MessagePart{TextPart{Text: "first request"}}, CreatedAt: now}); err != nil {
		t.Fatalf("create message: %v", err)
	}
	if err := s.Messages.Create(&Message{ID: "u2", SessionID: "s1", Role: RoleUser, Parts: []MessagePart{TextPart{Text: "latest request"}}, CreatedAt: now}); err != nil {
		t.Fatalf("create message: %v", err)
	}

	p := &scriptedProvider{
		streams: []streamScript{
			eventStream(
				provider.ProviderEvent{Type: provider.EventContentDelta, Delta: "concise context"},
				provider.ProviderEvent{Type: provider.EventComplete, Usage: &provider.UsageInfo{PromptTokens: 9, CompletionTokens: 1}},
			),
			eventStream(
				provider.ProviderEvent{Type: provider.EventComplete, Usage: &provider.UsageInfo{PromptTokens: 7, CompletionTokens: 2}},
			),
		},
	}
	a := NewAgent(s.SessionStore(), s.MessageStore(), nil, p)
	if err := a.Compact(context.Background(), session); err != nil {
		t.Fatalf("compact: %v", err)
	}

	msgs, err := s.Messages.ListBySession("s1", 10, 0)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if !strings.HasSuffix(messageText(msgs[0]), "Last request from user was: latest request") {
		t.Fatalf("did not preserve last user intent: %q", messageText(msgs[0]))
	}
}

func TestCompactPublishesEvent(t *testing.T) {
	s := openAgentStore(t)
	now := time.Date(2026, 2, 21, 0, 0, 0, 0, time.UTC)
	session := &Session{ID: "s1", CreatedAt: now, UpdatedAt: now}
	if err := s.Sessions.Create(session); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := s.Messages.Create(&Message{ID: "u1", SessionID: "s1", Role: RoleUser, Parts: []MessagePart{TextPart{Text: "need this"}}, CreatedAt: now}); err != nil {
		t.Fatalf("create message: %v", err)
	}

	p := &scriptedProvider{
		streams: []streamScript{
			eventStream(
				provider.ProviderEvent{Type: provider.EventContentDelta, Delta: "summary"},
				provider.ProviderEvent{Type: provider.EventComplete, Usage: &provider.UsageInfo{PromptTokens: 1, CompletionTokens: 1}},
			),
			eventStream(
				provider.ProviderEvent{Type: provider.EventComplete, Usage: &provider.UsageInfo{PromptTokens: 2, CompletionTokens: 3}},
			),
		},
	}
	a := NewAgent(s.SessionStore(), s.MessageStore(), nil, p)
	evCh, unsub := a.Events().Subscribe()
	defer unsub()

	if err := a.Compact(context.Background(), session); err != nil {
		t.Fatalf("compact: %v", err)
	}
	ev := waitEvent(t, evCh)
	if ev.Type != EventCompact {
		t.Fatalf("expected EventCompact, got %q", ev.Type)
	}
	if ev.SessionID != "s1" {
		t.Fatalf("unexpected session id in compact event: %q", ev.SessionID)
	}
}

func messageText(msg *Message) string {
	for _, part := range msg.Parts {
		if text, ok := part.(TextPart); ok {
			return text.Text
		}
	}
	return ""
}
