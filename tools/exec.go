package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/agusx1211/miclaw/config"
	"github.com/agusx1211/miclaw/model"
	"github.com/agusx1211/miclaw/sandbox"
)

const (
	execDefaultTimeout   = 1800
	execMaxOutputChars   = 100000
	execOutputTruncated  = "[output truncated]"
	execKillGraceTimeout = 5 * time.Second
	execSSHHost          = "host.docker.internal"
)

var execProcessManager = NewProcManager()
var hostCommandAllowlist = []string{"docker", "git", "systemctl", "journalctl"}

type hostExecutor interface {
	Execute(ctx context.Context, command string) (string, int, error)
}

type hostExecutorFactory func(keyPath, user, host string) (hostExecutor, error)

type execRunner struct {
	sandbox  config.SandboxConfig
	allow    []string
	host     string
	newSSHFn hostExecutorFactory
}

type execParams struct {
	Command    string
	Timeout    int
	WorkingDir string
	Background bool
}

func execTool() Tool {
	return execToolWithSandbox(config.SandboxConfig{})
}

func execToolWithSandbox(cfg config.SandboxConfig) Tool {
	runner := newExecRunner(cfg, hostCommandAllowlist, execSSHHost, newHostExecutor)
	return tool{
		name: "exec",
		desc: "Execute a shell command and return combined stdout/stderr output",
		params: JSONSchema{
			Type: "object",
			Properties: map[string]JSONSchema{
				"command": {
					Type: "string",
					Desc: "Shell command to execute",
				},
				"timeout": {
					Type: "integer",
					Desc: "Execution timeout in seconds (default: 1800)",
				},
				"working_dir": {
					Type: "string",
					Desc: "Directory to execute the command in",
				},
				"background": {
					Type: "boolean",
					Desc: "Run in background and return process ID",
				},
			},
			Required: []string{"command"},
		},
		runFn: runner.run,
	}
}

func newExecRunner(
	cfg config.SandboxConfig,
	allow []string,
	host string,
	newSSHFn hostExecutorFactory,
) execRunner {
	return execRunner{
		sandbox:  cfg,
		allow:    allow,
		host:     host,
		newSSHFn: newSSHFn,
	}
}

func newHostExecutor(keyPath, user, host string) (hostExecutor, error) {
	return sandbox.NewSSHExecutor(keyPath, user, host)
}

func (r execRunner) run(ctx context.Context, call model.ToolCallPart) (ToolResult, error) {
	params, err := parseExecParams(call.Parameters)
	if err != nil {
		return ToolResult{Content: err.Error(), IsError: true}, nil
	}
	if params.Background {
		return runExecBackground(params), nil
	}
	if r.shouldRunOnHost(params.Command) {
		return r.runExecHost(ctx, params), nil
	}
	return runExecLocal(ctx, params), nil
}

func (r execRunner) shouldRunOnHost(command string) bool {
	if !r.sandbox.Enabled || r.sandbox.SSHKeyPath == "" {
		return false
	}
	return sandbox.IsHostCommand(command, r.allow)
}

func runExecBackground(params execParams) ToolResult {
	cmd := localExecCommand(params)
	pid := execProcessManager.Start(cmd)
	return ToolResult{Content: fmt.Sprintf("started background process %d", pid)}
}

func runExecLocal(ctx context.Context, params execParams) ToolResult {
	cmd := localExecCommand(params)
	exitCode, output, status := runForegroundCommand(ctx, cmd, params.Timeout)
	return asExecResult(exitCode, status, output)
}

func localExecCommand(params execParams) *exec.Cmd {
	cmd := exec.Command("sh", "-c", params.Command)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if params.WorkingDir != "" {
		cmd.Dir = params.WorkingDir
	}
	return cmd
}

func (r execRunner) runExecHost(ctx context.Context, params execParams) ToolResult {
	exec, err := r.newSSHFn(r.sandbox.SSHKeyPath, r.sandbox.HostUser, r.host)
	if err != nil {
		return asExecResult(-1, fmt.Sprintf("failed to start command: %v", err), "")
	}
	exitCode, output, status := runForegroundHostCommand(ctx, exec, params.Command, params.Timeout)
	return asExecResult(exitCode, status, output)
}

func runForegroundHostCommand(
	ctx context.Context,
	exec hostExecutor,
	command string,
	timeout int,
) (int, string, string) {
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	output, exitCode, err := exec.Execute(runCtx, command)
	output = truncateExecOutput(output)
	if err == nil {
		return exitCode, output, ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return exitCode, output, "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return exitCode, output, "canceled"
	}
	return exitCode, output, fmt.Sprintf("failed to start command: %v", err)
}

func asExecResult(exitCode int, status, output string) ToolResult {
	content := formatExecResult(exitCode, status, output)
	if strings.HasPrefix(status, "failed to start command") {
		return ToolResult{Content: content, IsError: true}
	}
	return ToolResult{Content: content}
}

func parseExecParams(raw json.RawMessage) (execParams, error) {
	var input struct {
		Command    *string `json:"command"`
		Timeout    *int    `json:"timeout"`
		WorkingDir *string `json:"working_dir"`
		Background *bool   `json:"background"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return execParams{}, fmt.Errorf("parse exec parameters: %v", err)
	}
	if input.Command == nil || *input.Command == "" {
		return execParams{}, errors.New("exec parameter command is required")
	}
	timeout := execDefaultTimeout
	if input.Timeout != nil {
		timeout = *input.Timeout
	}
	if timeout <= 0 || timeout > execDefaultTimeout {
		return execParams{}, fmt.Errorf(
			"exec timeout must be between 1 and %d",
			execDefaultTimeout,
		)
	}
	params := execParams{Command: *input.Command, Timeout: timeout, Background: false}
	if input.WorkingDir != nil {
		params.WorkingDir = *input.WorkingDir
	}
	if input.Background != nil {
		params.Background = *input.Background
	}

	return params, nil
}

func runForegroundCommand(ctx context.Context, cmd *exec.Cmd, timeout int) (int, string, string) {
	output := &bytes.Buffer{}
	outputMu := sync.Mutex{}
	cmd.Stdout = &outputWriter{buf: output, mu: &outputMu}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return -1, "", fmt.Sprintf("failed to start command: %v", err)
	}
	done := make(chan int, 1)
	go func() {
		_ = cmd.Wait()
		done <- cmd.ProcessState.ExitCode()
	}()

	timer := time.NewTimer(time.Duration(timeout) * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		exitCode := terminateCommand(cmd, done)
		return exitCode, truncateExecOutput(safeOutput(output, &outputMu)), "canceled"
	case <-timer.C:
		exitCode := terminateCommand(cmd, done)
		return exitCode, truncateExecOutput(safeOutput(output, &outputMu)), "timeout"
	case exitCode := <-done:
		return exitCode, truncateExecOutput(safeOutput(output, &outputMu)), ""
	}
}

func terminateCommand(cmd *exec.Cmd, done <-chan int) int {
	pgid := cmd.Process.Pid
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	grace := time.NewTimer(execKillGraceTimeout)
	defer grace.Stop()

	select {
	case code := <-done:
		return code
	case <-grace.C:
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		return <-done
	}
}

type outputWriter struct {
	buf *bytes.Buffer
	mu  *sync.Mutex
}

func (w *outputWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func safeOutput(output *bytes.Buffer, mu *sync.Mutex) string {
	mu.Lock()
	defer mu.Unlock()
	return output.String()
}

func truncateExecOutput(raw string) string {
	if len(raw) <= execMaxOutputChars {
		return raw
	}
	room := execMaxOutputChars - len(execOutputTruncated) - 1
	if room < 0 {
		return execOutputTruncated
	}
	return raw[:room] + "\n" + execOutputTruncated
}

func formatExecResult(exitCode int, status string, output string) string {
	result := fmt.Sprintf("exit code: %d", exitCode)
	if status != "" {
		result += "; " + status
	}
	if output != "" {
		result += "\n" + output
	}
	return result
}
