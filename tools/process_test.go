package tools

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/agusx1211/miclaw/model"
)

type processStatus struct {
	Running  bool
	ExitCode int
	Runtime  string
}

func TestProcessStatusOfRunningProcess(t *testing.T) {
	pid := startBackgroundProcess(t, "sleep 1")
	got, err := runProcessCall(t, map[string]any{"action": "status", "pid": pid})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if got.IsError {
		t.Fatalf("unexpected tool error: %s", got.Content)
	}
	st := parseProcessStatus(t, got.Content)
	if !st.Running {
		t.Fatalf("expected running process, got %q", got.Content)
	}
}

func TestProcessStatusOfCompletedProcess(t *testing.T) {
	pid := startBackgroundProcess(t, "exit 7")
	st := waitProcessDone(t, pid)
	if st.Running {
		t.Fatalf("expected process %d to be completed", pid)
	}
	if st.ExitCode != 7 {
		t.Fatalf("want exit code 7, got %d", st.ExitCode)
	}
}

func TestProcessPollReturnsProcessOutput(t *testing.T) {
	pid := startBackgroundProcess(t, "echo one; sleep 0.1; echo two")
	waitProcessDone(t, pid)
	got, err := runProcessCall(t, map[string]any{"action": "poll", "pid": pid})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if got.IsError {
		t.Fatalf("unexpected tool error: %s", got.Content)
	}
	if !strings.Contains(got.Content, "one") || !strings.Contains(got.Content, "two") {
		t.Fatalf("missing process output: %q", got.Content)
	}
}

func TestProcessSignalSendsSIGTERM(t *testing.T) {
	pid := startBackgroundProcess(t, "sleep 60")
	got, err := runProcessCall(t, map[string]any{
		"action": "signal",
		"pid":    pid,
		"signal": "SIGTERM",
	})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if got.IsError {
		t.Fatalf("unexpected tool error: %s", got.Content)
	}
	st := waitProcessDone(t, pid)
	if st.Running {
		t.Fatalf("expected process %d to stop after SIGTERM", pid)
	}
}

func TestProcessInputWritesDataToStdin(t *testing.T) {
	pid := startBackgroundProcess(t, "cat")
	t.Cleanup(func() {
		_, _ = runProcessCall(t, map[string]any{"action": "signal", "pid": pid, "signal": "SIGKILL"})
	})
	got, err := runProcessCall(t, map[string]any{
		"action": "input",
		"pid":    pid,
		"data":   "hello\\n",
	})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if got.IsError {
		t.Fatalf("unexpected tool error: %s", got.Content)
	}
	waitProcessOutput(t, pid, "hello")
}

func TestProcessUnknownPIDReturnsError(t *testing.T) {
	got, err := runProcessCall(t, map[string]any{"action": "status", "pid": 99999999})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if !got.IsError {
		t.Fatalf("expected error for unknown pid, got %q", got.Content)
	}
}

func TestProcessSignalToCompletedProcessReturnsError(t *testing.T) {
	pid := startBackgroundProcess(t, "true")
	waitProcessDone(t, pid)
	got, err := runProcessCall(t, map[string]any{
		"action": "signal",
		"pid":    pid,
		"signal": "SIGTERM",
	})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if !got.IsError {
		t.Fatalf("expected error signaling completed process, got %q", got.Content)
	}
}

func TestProcessInputToCompletedProcessReturnsError(t *testing.T) {
	pid := startBackgroundProcess(t, "true")
	waitProcessDone(t, pid)
	got, err := runProcessCall(t, map[string]any{
		"action": "input",
		"pid":    pid,
		"data":   "ignored",
	})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if !got.IsError {
		t.Fatalf("expected error writing to completed process, got %q", got.Content)
	}
}

func runProcessCall(t *testing.T, params map[string]any) (ToolResult, error) {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal process params: %v", err)
	}
	return processTool().Run(context.Background(), model.ToolCallPart{
		ID:         "1",
		Name:       "process",
		Parameters: raw,
	})
}

func startBackgroundProcess(t *testing.T, command string) int {
	t.Helper()
	got, err := runExecCall(t, context.Background(), map[string]any{
		"command":    command,
		"background": true,
	})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}
	if got.IsError {
		t.Fatalf("start process error: %s", got.Content)
	}
	return execBackgroundPID(t, got.Content)
}

func waitProcessDone(t *testing.T, pid int) processStatus {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		got, err := runProcessCall(t, map[string]any{"action": "status", "pid": pid})
		if err != nil {
			t.Fatalf("status process %d: %v", pid, err)
		}
		if got.IsError {
			t.Fatalf("status process %d error: %s", pid, got.Content)
		}
		st := parseProcessStatus(t, got.Content)
		if !st.Running {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("process %d did not finish in time", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitProcessOutput(t *testing.T, pid int, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		got, err := runProcessCall(t, map[string]any{"action": "poll", "pid": pid})
		if err != nil {
			t.Fatalf("poll process %d: %v", pid, err)
		}
		if got.IsError {
			t.Fatalf("poll process %d error: %s", pid, got.Content)
		}
		if strings.Contains(got.Content, want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("did not find %q in output %q", want, got.Content)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func parseProcessStatus(t *testing.T, content string) processStatus {
	t.Helper()
	var out processStatus
	for _, line := range strings.Split(strings.TrimSpace(content), "\n") {
		if strings.HasPrefix(line, "running: ") {
			out.Running = strings.TrimSpace(strings.TrimPrefix(line, "running: ")) == "true"
		}
		if strings.HasPrefix(line, "exit code: ") {
			v := strings.TrimSpace(strings.TrimPrefix(line, "exit code: "))
			n, err := strconv.Atoi(v)
			if err != nil {
				t.Fatalf("invalid exit code in %q", content)
			}
			out.ExitCode = n
		}
		if strings.HasPrefix(line, "runtime: ") {
			out.Runtime = strings.TrimSpace(strings.TrimPrefix(line, "runtime: "))
		}
	}
	if out.Runtime == "" {
		t.Fatalf("missing runtime in %q", content)
	}
	return out
}
