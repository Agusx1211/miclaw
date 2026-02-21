package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/agusx1211/miclaw/config"
	"github.com/agusx1211/miclaw/model"
)

func TestExecSimpleCommand(t *testing.T) {
	got, err := runExecCall(t, context.Background(), map[string]any{
		"command": "echo hello",
	})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if got.IsError {
		t.Fatalf("unexpected tool error: %s", got.Content)
	}
	if exitCode := execResultExitCode(t, got.Content); exitCode != 0 {
		t.Fatalf("want exit code 0, got %d", exitCode)
	}
	if out := execResultOutput(got.Content); !strings.Contains(out, "hello") {
		t.Fatalf("missing output: %q", out)
	}
}

func TestExecNonZeroExit(t *testing.T) {
	got, err := runExecCall(t, context.Background(), map[string]any{
		"command": "exit 1",
	})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if got.IsError {
		t.Fatalf("unexpected tool error: %s", got.Content)
	}
	if exitCode := execResultExitCode(t, got.Content); exitCode != 1 {
		t.Fatalf("want exit code 1, got %d", exitCode)
	}
}

func TestExecTimeout(t *testing.T) {
	got, err := runExecCall(t, context.Background(), map[string]any{
		"command": "sleep 10",
		"timeout": 1,
	})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if got.IsError {
		t.Fatalf("unexpected tool error: %s", got.Content)
	}
	if execResultExitCode(t, got.Content) == 0 {
		t.Fatalf("expected non-zero exit code on timeout: %q", got.Content)
	}
	if !strings.Contains(strings.ToLower(got.Content), "timeout") {
		t.Fatalf("expected timeout status in %q", got.Content)
	}
}

func TestExecOutputTruncation(t *testing.T) {
	got, err := runExecCall(t, context.Background(), map[string]any{
		"command": "yes x | head -n 60000",
	})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if got.IsError {
		t.Fatalf("unexpected tool error: %s", got.Content)
	}
	output := execResultOutput(got.Content)
	if len(output) > 100000 {
		t.Fatalf("output should be truncated near 100K, got %d", len(output))
	}
	if !strings.Contains(output, "[output truncated]") {
		t.Fatalf("missing truncation marker in %q", output)
	}
}

func TestExecWorkingDir(t *testing.T) {
	got, err := runExecCall(t, context.Background(), map[string]any{
		"command":     "pwd",
		"working_dir": "/tmp",
	})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if got.IsError {
		t.Fatalf("unexpected tool error: %s", got.Content)
	}
	if !strings.Contains(execResultOutput(got.Content), "/tmp") {
		t.Fatalf("expected working_dir output to contain /tmp: %q", got.Content)
	}
}

func TestExecBackground(t *testing.T) {
	got, err := runExecCall(t, context.Background(), map[string]any{
		"command":    "echo bg-output; sleep 0.5; echo bg-done",
		"background": true,
	})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	pid := execBackgroundPID(t, got.Content)
	running := true
	for i := 0; i < 40; i++ {
		wasRunning, _, _, err := execProcessManager.Status(pid)
		if err != nil {
			t.Fatalf("status process %d: %v", pid, err)
		}
		if !wasRunning {
			running = false
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if running {
		t.Fatalf("background process %d still running after timeout", pid)
	}
	output, err := execProcessManager.Poll(pid)
	if err != nil {
		t.Fatalf("poll process %d: %v", pid, err)
	}
	if !strings.Contains(output, "bg-output") {
		t.Fatalf("expected background output, got %q", output)
	}
}

func TestExecContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan struct {
		got ToolResult
		err error
	}, 1)
	go func() {
		got, err := runExecCall(t, ctx, map[string]any{
			"command": "sleep 10",
		})
		result <- struct {
			got ToolResult
			err error
		}{got: got, err: err}
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case got := <-result:
		if got.err != nil {
			t.Fatalf("tool call: %v", got.err)
		}
		if got.got.IsError {
			t.Fatalf("unexpected tool error: %s", got.got.Content)
		}
		if execResultExitCode(t, got.got.Content) == 0 {
			t.Fatalf("expected non-zero exit on cancellation: %q", got.got.Content)
		}
		if !strings.Contains(strings.ToLower(got.got.Content), "canceled") {
			t.Fatalf("expected canceled status: %q", got.got.Content)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("timed out waiting for canceled exec")
	}
}

func TestExecSandboxRoutesHostCommandThroughSSH(t *testing.T) {
	ssh := &fakeSSHExecutor{output: "host-output", exitCode: 0}
	called := false
	runner := newExecRunner(
		config.SandboxConfig{Enabled: true, SSHKeyPath: "/tmp/key", HostUser: "runner"},
		[]string{"git"},
		"host.docker.internal",
		func(keyPath, user, host string) (hostExecutor, error) {
			called = true
			if keyPath != "/tmp/key" || user != "runner" || host != "host.docker.internal" {
				t.Fatalf("unexpected ssh config: %q %q %q", keyPath, user, host)
			}
			return ssh, nil
		},
	)
	got, err := runExecCallWithRunner(t, context.Background(), runner, map[string]any{
		"command": "git status",
	})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if !called {
		t.Fatal("expected ssh executor to be used")
	}
	if got.IsError {
		t.Fatalf("unexpected tool error: %s", got.Content)
	}
	if code := execResultExitCode(t, got.Content); code != 0 {
		t.Fatalf("want exit code 0, got %d", code)
	}
	if out := execResultOutput(got.Content); !strings.Contains(out, "host-output") {
		t.Fatalf("missing host output: %q", out)
	}
	if ssh.command != "git status" {
		t.Fatalf("unexpected command: %q", ssh.command)
	}
}

func TestExecSandboxRunsNonHostCommandsLocally(t *testing.T) {
	called := false
	runner := newExecRunner(
		config.SandboxConfig{Enabled: true, SSHKeyPath: "/tmp/key", HostUser: "runner"},
		[]string{"git"},
		"host.docker.internal",
		func(string, string, string) (hostExecutor, error) {
			called = true
			return &fakeSSHExecutor{}, nil
		},
	)
	got, err := runExecCallWithRunner(t, context.Background(), runner, map[string]any{
		"command": "echo local",
	})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if got.IsError {
		t.Fatalf("unexpected tool error: %s", got.Content)
	}
	if called {
		t.Fatal("unexpected ssh executor call for local command")
	}
	if !strings.Contains(execResultOutput(got.Content), "local") {
		t.Fatalf("missing local output: %q", got.Content)
	}
}

func TestExecSandboxReportsSSHSetupErrors(t *testing.T) {
	runner := newExecRunner(
		config.SandboxConfig{Enabled: true, SSHKeyPath: "/tmp/key", HostUser: "runner"},
		[]string{"git"},
		"host.docker.internal",
		func(string, string, string) (hostExecutor, error) {
			return nil, errors.New("bad key")
		},
	)
	got, err := runExecCallWithRunner(t, context.Background(), runner, map[string]any{
		"command": "git status",
	})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if !got.IsError {
		t.Fatalf("expected tool error, got %q", got.Content)
	}
	if !strings.Contains(got.Content, "failed to start command: bad key") {
		t.Fatalf("unexpected error: %q", got.Content)
	}
}

func runExecCall(t *testing.T, ctx context.Context, params map[string]any) (ToolResult, error) {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal exec params: %v", err)
	}
	if _, ok := params["working_dir"]; ok {
		path := params["working_dir"].(string)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("working_dir %q: %v", path, err)
		}
	}
	return execTool().Run(ctx, model.ToolCallPart{
		ID:         "1",
		Name:       "exec",
		Parameters: raw,
	})
}

func runExecCallWithRunner(t *testing.T, ctx context.Context, runner execRunner, params map[string]any) (ToolResult, error) {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal exec params: %v", err)
	}
	if _, ok := params["working_dir"]; ok {
		path := params["working_dir"].(string)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("working_dir %q: %v", path, err)
		}
	}
	return runner.run(ctx, model.ToolCallPart{
		ID:         "1",
		Name:       "exec",
		Parameters: raw,
	})
}

type fakeSSHExecutor struct {
	command  string
	output   string
	exitCode int
	err      error
}

func (f *fakeSSHExecutor) Execute(_ context.Context, command string) (string, int, error) {
	f.command = command
	return f.output, f.exitCode, f.err
}

func execResultExitCode(t *testing.T, content string) int {
	t.Helper()
	line := strings.SplitN(content, "\n", 2)[0]
	if !strings.HasPrefix(line, "exit code:") {
		t.Fatalf("missing exit code line: %q", content)
	}
	for _, token := range strings.Fields(strings.TrimPrefix(line, "exit code:")) {
		trimmed := strings.Trim(token, ";")
		code, err := strconv.Atoi(trimmed)
		if err == nil {
			return code
		}
	}
	t.Fatalf("invalid exit code: %q", line)
	return 0
}

func execResultOutput(content string) string {
	parts := strings.SplitN(content, "\n", 2)
	if len(parts) < 2 {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func execBackgroundPID(t *testing.T, content string) int {
	t.Helper()
	re := regexp.MustCompile(`\d+`)
	m := re.FindString(content)
	if m == "" {
		t.Fatalf("missing pid in %q", content)
	}
	pid, err := strconv.Atoi(m)
	if err != nil {
		t.Fatalf("invalid pid: %v", err)
	}
	return pid
}
