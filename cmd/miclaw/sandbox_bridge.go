package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/agusx1211/miclaw/config"
	"github.com/agusx1211/miclaw/model"
	"github.com/agusx1211/miclaw/tools"
)

type sandboxBridge struct {
	containerID string
	hostServer  *hostCommandServer
}

type sandboxProxyTool struct {
	base   tools.Tool
	bridge *sandboxBridge
}

func wrapToolsWithSandboxBridge(toolList []tools.Tool, bridge *sandboxBridge) []tools.Tool {
	bridgeable := tools.BridgeableToolNames()
	out := make([]tools.Tool, 0, len(toolList))
	for _, t := range toolList {
		if t.Name() == "process" {
			continue
		}
		if !bridgeable[t.Name()] {
			out = append(out, t)
			continue
		}
		out = append(out, sandboxProxyTool{base: t, bridge: bridge})
	}
	return out
}

func (t sandboxProxyTool) Name() string {
	return t.base.Name()
}

func (t sandboxProxyTool) Description() string {
	return t.base.Description()
}

func (t sandboxProxyTool) Parameters() tools.JSONSchema {
	return t.base.Parameters()
}

func (t sandboxProxyTool) Run(ctx context.Context, call model.ToolCallPart) (tools.ToolResult, error) {
	return t.bridge.RunTool(ctx, call)
}

func startSandboxBridge(cfg *config.Config) (*sandboxBridge, error) {
	if err := os.MkdirAll(cfg.Workspace, 0o755); err != nil {
		return nil, fmt.Errorf("create workspace %q: %v", cfg.Workspace, err)
	}
	if err := os.MkdirAll(cfg.StatePath, 0o755); err != nil {
		return nil, fmt.Errorf("create state path %q: %v", cfg.StatePath, err)
	}
	exePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve executable: %v", err)
	}
	args, err := buildSandboxBridgeRunArgs(exePath, cfg)
	if err != nil {
		return nil, err
	}
	hostServer, err := startSandboxHostCommandServer(cfg)
	if err != nil {
		return nil, err
	}
	closeHostServer := hostServer != nil
	if hostServer != nil {
		defer func() {
			if closeHostServer {
				_ = hostServer.Close()
			}
		}()
	}
	log.Printf(
		"[sandbox] tool bridge enabled network=%s mounts=%d host_commands=%d image=%s",
		sandboxNetwork(cfg), len(cfg.Sandbox.Mounts), len(cfg.Sandbox.HostCommands), sandboxRuntimeImage,
	)
	cmd := exec.Command("docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("start sandbox bridge container: %v\n%s", err, strings.TrimSpace(string(out)))
	}
	id, err := dockerRunContainerID(out)
	if err != nil {
		return nil, fmt.Errorf("sandbox bridge returned invalid container id: %v\n%s", err, strings.TrimSpace(string(out)))
	}
	log.Printf("[sandbox] bridge started id=%s", shortContainerID(id))
	closeHostServer = false
	return &sandboxBridge{containerID: id, hostServer: hostServer}, nil
}

func startSandboxHostCommandServer(cfg *config.Config) (*hostCommandServer, error) {
	if len(cfg.Sandbox.HostCommands) == 0 {
		return nil, nil
	}
	workspaceHostPath, err := resolveBindPath(cfg.Workspace)
	if err != nil {
		return nil, fmt.Errorf("sandbox workspace path: %v", err)
	}
	stateHostPath, err := resolveBindPath(cfg.StatePath)
	if err != nil {
		return nil, fmt.Errorf("sandbox state path: %v", err)
	}
	mounts := []config.Mount{{Host: workspaceHostPath, Container: workspaceHostPath, Mode: "rw"}}
	for _, m := range cfg.Sandbox.Mounts {
		hostPath, err := resolveBindPath(m.Host)
		if err != nil {
			return nil, fmt.Errorf("sandbox mount host %q: %v", m.Host, err)
		}
		mounts = append(mounts, config.Mount{
			Host:      hostPath,
			Container: m.Container,
			Mode:      m.Mode,
		})
	}
	return startHostCommandServer(hostCommandServerConfig{
		SocketPath: sandboxHostExecutorSocketPath(stateHostPath),
		Workspace:  workspaceHostPath,
		Allowed:    cfg.Sandbox.HostCommands,
		Mounts:     mounts,
		HostUser:   cfg.Sandbox.HostUser,
	})
}

func buildSandboxBridgeRunArgs(exePath string, cfg *config.Config) ([]string, error) {
	exeHostPath, err := resolveBindPath(exePath)
	if err != nil {
		return nil, fmt.Errorf("sandbox executable path: %v", err)
	}
	workspaceHostPath, err := resolveBindPath(cfg.Workspace)
	if err != nil {
		return nil, fmt.Errorf("sandbox workspace path: %v", err)
	}
	stateHostPath, err := resolveBindPath(cfg.StatePath)
	if err != nil {
		return nil, fmt.Errorf("sandbox state path: %v", err)
	}
	mounts := []config.Mount{
		{Host: exeHostPath, Container: sandboxEntrypoint, Mode: "ro"},
		{Host: workspaceHostPath, Container: workspaceHostPath, Mode: "rw"},
	}
	bridgeMounts, bridgeEnv, err := sandboxHostBridge(stateHostPath, cfg.Sandbox)
	if err != nil {
		return nil, err
	}
	mounts = append(mounts, bridgeMounts...)
	for _, m := range cfg.Sandbox.Mounts {
		hostPath, err := resolveBindPath(m.Host)
		if err != nil {
			return nil, fmt.Errorf("sandbox mount host %q: %v", m.Host, err)
		}
		mounts = append(mounts, config.Mount{
			Host:      hostPath,
			Container: m.Container,
			Mode:      m.Mode,
		})
	}
	u := strconv.Itoa(os.Getuid()) + ":" + strconv.Itoa(os.Getgid())
	args := []string{
		"run",
		"-d",
		"--rm",
		"--init",
		"--network=" + sandboxNetwork(cfg),
		"--user", u,
		"--workdir", workspaceHostPath,
		"-e", sandboxChildEnv + "=1",
		"--label", "miclaw.sandbox_bridge=1",
	}
	for _, env := range bridgeEnv {
		args = append(args, "-e", env)
	}
	seen := map[string]bool{}
	for _, m := range mounts {
		key := m.Host + "\x00" + m.Container + "\x00" + m.Mode
		if seen[key] {
			continue
		}
		seen[key] = true
		args = append(args, "--mount", dockerBindMount(m))
	}
	args = append(
		args,
		"--entrypoint", "sh",
		sandboxRuntimeImage,
		"-c",
		"trap 'exit 0' TERM INT; while :; do sleep 3600; done",
	)
	return args, nil
}

func (b *sandboxBridge) RunTool(ctx context.Context, call model.ToolCallPart) (tools.ToolResult, error) {
	if b == nil || b.containerID == "" {
		return tools.ToolResult{IsError: true, Content: "sandbox bridge is not running"}, nil
	}
	if call.Name == "exec" && execBackgroundRequested(call.Parameters) {
		return tools.ToolResult{
			IsError: true,
			Content: "sandbox bridge does not support exec background mode",
		}, nil
	}
	log.Printf("[sandbox] tool dispatch name=%s container=%s", call.Name, shortContainerID(b.containerID))
	raw, err := json.Marshal(call)
	if err != nil {
		return tools.ToolResult{IsError: true, Content: fmt.Sprintf("marshal tool call: %v", err)}, nil
	}
	encoded := base64.StdEncoding.EncodeToString(raw)
	cmd := exec.CommandContext(
		ctx,
		"docker",
		"exec",
		"-i",
		b.containerID,
		sandboxEntrypoint,
		"--tool-call",
		encoded,
	)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := fmt.Sprintf("sandbox tool %s failed: %v", call.Name, err)
		if strings.TrimSpace(stderr.String()) != "" {
			msg += "\n" + strings.TrimSpace(stderr.String())
		}
		return tools.ToolResult{IsError: true, Content: msg}, nil
	}
	var result tools.ToolResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		msg := fmt.Sprintf("sandbox tool %s returned invalid response: %v", call.Name, err)
		if strings.TrimSpace(stdout.String()) != "" {
			msg += "\n" + strings.TrimSpace(stdout.String())
		}
		return tools.ToolResult{IsError: true, Content: msg}, nil
	}
	return result, nil
}

func (b *sandboxBridge) Close() error {
	if b == nil {
		return nil
	}
	var firstErr error
	if b.containerID != "" {
		id := b.containerID
		b.containerID = ""
		cmd := exec.Command("docker", "stop", "--time", "2", id)
		out, err := cmd.CombinedOutput()
		if err != nil {
			text := strings.TrimSpace(string(out))
			if !strings.Contains(text, "No such container") && !strings.Contains(text, "is not running") {
				firstErr = fmt.Errorf("stop sandbox bridge %s: %v\n%s", shortContainerID(id), err, text)
			}
		} else {
			log.Printf("[sandbox] bridge stopped id=%s", shortContainerID(id))
		}
	}
	if b.hostServer != nil {
		if err := b.hostServer.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("stop host command server: %v", err)
		}
		b.hostServer = nil
	}
	return firstErr
}

func execBackgroundRequested(raw json.RawMessage) bool {
	var params struct {
		Background *bool `json:"background"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return false
	}
	return params.Background != nil && *params.Background
}

func shortContainerID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12]
}

func dockerRunContainerID(out []byte) (string, error) {
	lines := strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		id := strings.TrimSpace(lines[i])
		if id == "" {
			continue
		}
		if strings.ContainsAny(id, " \t") {
			return "", fmt.Errorf("last docker output line is not a container id")
		}
		return id, nil
	}
	return "", fmt.Errorf("empty docker output")
}
