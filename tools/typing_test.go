package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/agusx1211/miclaw/model"
)

func runTypingCall(t *testing.T, tl Tool, params map[string]any) (ToolResult, error) {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	got, err := tl.Run(context.Background(), model.ToolCallPart{
		Name:       "typing",
		Parameters: raw,
	})
	return got, err
}

func newTypingToolForTest(start func(context.Context, string, time.Duration) error, stop func(context.Context, string) error) Tool {
	tl := typingTool(start, stop)
	if tl.Name() != "typing" {
		panic("typing tool not configured")
	}
	return tl
}

func TestTypingToolStartsWithoutTimeoutByDefault(t *testing.T) {
	var gotTo string
	var gotDur time.Duration
	typing := newTypingToolForTest(
		func(_ context.Context, to string, d time.Duration) error {
			gotTo = to
			gotDur = d
			return nil
		},
		func(context.Context, string) error { return nil },
	)
	got, err := runTypingCall(t, typing, map[string]any{"to": "signal:dm:user-1"})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if got.IsError {
		t.Fatalf("unexpected tool error: %q", got.Content)
	}
	if gotTo != "signal:dm:user-1" {
		t.Fatalf("to = %q", gotTo)
	}
	if gotDur != 0 {
		t.Fatalf("duration = %s", gotDur)
	}
	if got.Content != "typing on for signal:dm:user-1" {
		t.Fatalf("result = %q", got.Content)
	}
}

func TestTypingToolUsesProvidedSeconds(t *testing.T) {
	var gotDur time.Duration
	typing := newTypingToolForTest(
		func(_ context.Context, _ string, d time.Duration) error {
			gotDur = d
			return nil
		},
		func(context.Context, string) error { return nil },
	)
	got, err := runTypingCall(t, typing, map[string]any{"to": "signal:dm:user-1", "seconds": 7})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if got.IsError {
		t.Fatalf("unexpected tool error: %q", got.Content)
	}
	if gotDur != 7*time.Second {
		t.Fatalf("duration = %s", gotDur)
	}
}

func TestTypingToolStopsWhenStateIsOff(t *testing.T) {
	startCalled := false
	stopTo := ""
	typing := newTypingToolForTest(
		func(context.Context, string, time.Duration) error {
			startCalled = true
			return nil
		},
		func(_ context.Context, to string) error {
			stopTo = to
			return nil
		},
	)
	got, err := runTypingCall(t, typing, map[string]any{"to": "signal:dm:user-1", "state": "off"})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if got.IsError {
		t.Fatalf("unexpected tool error: %q", got.Content)
	}
	if startCalled {
		t.Fatal("start should not be called when state=off")
	}
	if stopTo != "signal:dm:user-1" {
		t.Fatalf("stop target = %q", stopTo)
	}
	if got.Content != "typing off for signal:dm:user-1" {
		t.Fatalf("result = %q", got.Content)
	}
}

func TestTypingToolRejectsInvalidTarget(t *testing.T) {
	called := false
	typing := newTypingToolForTest(
		func(context.Context, string, time.Duration) error {
			called = true
			return nil
		},
		func(context.Context, string) error {
			called = true
			return nil
		},
	)
	got, err := runTypingCall(t, typing, map[string]any{"to": "badtarget"})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if !got.IsError {
		t.Fatal("expected error")
	}
	if called {
		t.Fatal("typing should not be called for invalid target")
	}
	if !strings.Contains(got.Content, "to must include channel and address") {
		t.Fatalf("unexpected error: %q", got.Content)
	}
}

func TestTypingToolRejectsUnsupportedChannel(t *testing.T) {
	called := false
	typing := newTypingToolForTest(
		func(context.Context, string, time.Duration) error {
			called = true
			return nil
		},
		func(context.Context, string) error {
			called = true
			return nil
		},
	)
	got, err := runTypingCall(t, typing, map[string]any{"to": "slack:general"})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if !got.IsError {
		t.Fatal("expected error")
	}
	if called {
		t.Fatal("typing should not be called for unsupported channel")
	}
	if !strings.Contains(got.Content, "unsupported channel") {
		t.Fatalf("unexpected error: %q", got.Content)
	}
}

func TestTypingToolRejectsInvalidSeconds(t *testing.T) {
	called := false
	typing := newTypingToolForTest(
		func(context.Context, string, time.Duration) error {
			called = true
			return nil
		},
		func(context.Context, string) error {
			called = true
			return nil
		},
	)
	got, err := runTypingCall(t, typing, map[string]any{"to": "signal:dm:user-1", "seconds": 0})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if !got.IsError {
		t.Fatal("expected error")
	}
	if called {
		t.Fatal("typing should not be called when seconds is invalid")
	}
	if !strings.Contains(got.Content, "seconds must be >= 1") {
		t.Fatalf("unexpected error: %q", got.Content)
	}
}

func TestTypingToolRejectsUnknownState(t *testing.T) {
	typing := newTypingToolForTest(
		func(context.Context, string, time.Duration) error { return nil },
		func(context.Context, string) error { return nil },
	)
	got, err := runTypingCall(t, typing, map[string]any{"to": "signal:dm:user-1", "state": "maybe"})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if !got.IsError {
		t.Fatal("expected error")
	}
	if !strings.Contains(got.Content, "state must be on or off") {
		t.Fatalf("unexpected error: %q", got.Content)
	}
}

func TestTypingToolRejectsSecondsWhenStateOff(t *testing.T) {
	typing := newTypingToolForTest(
		func(context.Context, string, time.Duration) error { return nil },
		func(context.Context, string) error { return nil },
	)
	got, err := runTypingCall(t, typing, map[string]any{"to": "signal:dm:user-1", "state": "off", "seconds": 5})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if !got.IsError {
		t.Fatal("expected error")
	}
	if !strings.Contains(got.Content, "seconds is only valid when state is on") {
		t.Fatalf("unexpected error: %q", got.Content)
	}
}

func TestTypingToolPropagatesStartError(t *testing.T) {
	typing := newTypingToolForTest(
		func(context.Context, string, time.Duration) error {
			return errors.New("typing failed")
		},
		func(context.Context, string) error { return nil },
	)
	got, err := runTypingCall(t, typing, map[string]any{"to": "signal:dm:user-1"})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if !got.IsError {
		t.Fatal("expected error")
	}
	if !strings.Contains(got.Content, "typing failed") {
		t.Fatalf("unexpected error: %q", got.Content)
	}
}

func TestTypingToolPropagatesStopError(t *testing.T) {
	typing := newTypingToolForTest(
		func(context.Context, string, time.Duration) error { return nil },
		func(context.Context, string) error {
			return errors.New("stop failed")
		},
	)
	got, err := runTypingCall(t, typing, map[string]any{"to": "signal:dm:user-1", "state": "off"})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if !got.IsError {
		t.Fatal("expected error")
	}
	if !strings.Contains(got.Content, "stop failed") {
		t.Fatalf("unexpected error: %q", got.Content)
	}
}
