package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/agusx1211/miclaw/model"
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
		callTool(t, processTool(), map[string]any{
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
		callTool(t, processTool(), map[string]any{
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
