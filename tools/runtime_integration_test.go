package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/agusx1211/miclaw/model"
	"github.com/agusx1211/miclaw/provider"
	"github.com/agusx1211/miclaw/store"
)

func callTool(t *testing.T, tl Tool, params map[string]any) ToolResult {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal %s params: %v", tl.Name(), err)
	}
	got, err := tl.Run(context.Background(), model.ToolCallPart{
		ID:         "1",
		Name:       tl.Name(),
		Parameters: raw,
	})
	if err != nil {
		t.Fatalf("%s: %v", tl.Name(), err)
	}
	return got
}

func openRuntimeMemoryStore(t *testing.T) *store.SQLiteStore {
	t.Helper()
	db, err := store.OpenSQLite(":memory:")
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close memory store: %v", err)
		}
	})
	return db
}

func waitProcessToComplete(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		got := callTool(t, processTool(), map[string]any{
			"action": "status",
			"pid":    pid,
		})
		if got.IsError {
			t.Fatalf("process %d status: %s", pid, got.Content)
		}
		st := parseProcessStatus(t, got.Content)
		if !st.Running {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("process %d did not complete in time", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestRuntimeExecForeground(t *testing.T) {
	got := callTool(t, execTool(), map[string]any{
		"command": "echo hello && echo world",
	})
	if got.IsError {
		t.Fatalf("unexpected tool error: %s", got.Content)
	}
	if code := execResultExitCode(t, got.Content); code != 0 {
		t.Fatalf("want exit code 0, got %d", code)
	}
	output := execResultOutput(got.Content)
	if !strings.Contains(output, "hello") || !strings.Contains(output, "world") {
		t.Fatalf("missing expected output: %q", output)
	}
}

func TestRuntimeExecBackgroundPollComplete(t *testing.T) {
	got := callTool(t, execTool(), map[string]any{
		"command":    "echo bg-output; sleep 0.2; echo bg-done",
		"background": true,
	})
	if got.IsError {
		t.Fatalf("unexpected tool error: %s", got.Content)
	}
	pid := execBackgroundPID(t, got.Content)
	t.Cleanup(func() {
		_, _ = callTool(t, processTool(), map[string]any{
			"action": "signal",
			"pid":    pid,
			"signal": "SIGKILL",
		})
	})
	waitProcessToComplete(t, pid)
	poll := callTool(t, processTool(), map[string]any{
		"action": "poll",
		"pid":    pid,
	})
	if poll.IsError {
		t.Fatalf("poll process %d: %s", pid, poll.Content)
	}
	if !strings.Contains(poll.Content, "bg-output") || !strings.Contains(poll.Content, "bg-done") {
		t.Fatalf("missing background output: %q", poll.Content)
	}
}

func TestRuntimeExecBackgroundSignal(t *testing.T) {
	got := callTool(t, execTool(), map[string]any{
		"command":    "sleep 60",
		"background": true,
	})
	if got.IsError {
		t.Fatalf("unexpected tool error: %s", got.Content)
	}
	pid := execBackgroundPID(t, got.Content)
	t.Cleanup(func() {
		_, _ = callTool(t, processTool(), map[string]any{
			"action": "signal",
			"pid":    pid,
			"signal": "SIGKILL",
		})
	})
	signal := callTool(t, processTool(), map[string]any{
		"action": "signal",
		"pid":    pid,
		"signal": "SIGTERM",
	})
	if signal.IsError {
		t.Fatalf("signal process %d: %s", pid, signal.Content)
	}
	waitProcessToComplete(t, pid)
}

func TestRuntimeSessionLifecycle(t *testing.T) {
	now := time.Now().UTC()
	db := openRuntimeMemoryStore(t)
	session := &model.Session{ID: "test-sess", Title: "Test", CreatedAt: now, UpdatedAt: now}
	if err := db.SessionStore().Create(session); err != nil {
		t.Fatalf("create session: %v", err)
	}
	got := callTool(t, sessionsSendTool(db.SessionStore(), db.MessageStore()), map[string]any{
		"session_id": "test-sess",
		"message":    "hello runtime",
	})
	if got.IsError {
		t.Fatalf("sessions_send: %s", got.Content)
	}
	if !strings.Contains(got.Content, "message sent to session test-sess") {
		t.Fatalf("unexpected sessions_send response: %q", got.Content)
	}
	history := callTool(t, sessionsHistoryTool(db.SessionStore(), db.MessageStore()), map[string]any{
		"session_id": "test-sess",
	})
	if history.IsError {
		t.Fatalf("sessions_history: %s", history.Content)
	}
	if !strings.Contains(history.Content, "user\thello runtime") {
		t.Fatalf("message missing from history: %q", history.Content)
	}
}

func TestRuntimeSpawnSubAgent(t *testing.T) {
	db := openRuntimeMemoryStore(t)
	now := time.Now().UTC()
	if err := db.SessionStore().Create(&model.Session{ID: "test-session", Title: "parent", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	p := &spawnProvider{
		model: provider.ModelInfo{Name: "test"},
		fn:    streamEvents(provider.ProviderEvent{Type: provider.EventContentDelta, Delta: "sub-agent says hello"}, provider.ProviderEvent{Type: provider.EventComplete}),
	}
	tool := sessionsSpawnToolWithTimeout(db.SessionStore(), db.MessageStore(), p, nil, nil, 5*time.Second)
	got, err := runSpawnCall(parentCtx("test-session"), tool, "runtime-child", map[string]any{
		"prompt": "say hi",
	})
	if err != nil {
		t.Fatalf("run sessions_spawn: %v", err)
	}
	if !strings.Contains(got.Content, "sub-agent says hello") {
		t.Fatalf("unexpected sub-agent response: %q", got.Content)
	}
}

func TestRuntimeSpawnDepthRejection(t *testing.T) {
	for _, tool := range SubAgentTools(nil, nil) {
		if tool.Name() == "sessions_spawn" {
			db := openRuntimeMemoryStore(t)
			p := &spawnProvider{
				model: provider.ModelInfo{Name: "test"},
				fn:    streamEvents(provider.ProviderEvent{Type: provider.EventContentDelta, Delta: "should fail"}, provider.ProviderEvent{Type: provider.EventComplete}),
			}
			tool := sessionsSpawnToolWithTimeout(db.SessionStore(), db.MessageStore(), p, nil, nil, time.Second)
			if _, err := runSpawnCall(parentCtx("child"), tool, "reject-child", map[string]any{"prompt": "spawn"}); err == nil {
				t.Fatalf("expected nested spawn to be rejected")
			}
			return
		}
	}
}

func TestRuntimeConcurrentExec(t *testing.T) {
	type result struct {
		want string
		got  ToolResult
	}
	out := make(chan result, 3)
	for i := range 3 {
		want := fmt.Sprintf("hello-%d", i)
		command := "echo " + want
		go func(want string) {
			got := callTool(t, execTool(), map[string]any{"command": command})
			out <- result{want: want, got: got}
		}(want)
	}
	for range 3 {
		got := <-out
		if got.got.IsError {
			t.Fatalf("concurrent exec: %s", got.got.Content)
		}
		if exitCode := execResultExitCode(t, got.got.Content); exitCode != 0 {
			t.Fatalf("expected exit code 0 for %q, got %d", got.want, exitCode)
		}
		if !strings.Contains(execResultOutput(got.got.Content), got.want) {
			t.Fatalf("missing command output %q: %q", got.want, got.got.Content)
		}
	}
}
