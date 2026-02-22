package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/agusx1211/miclaw/config"
)

const (
	sandboxChildEnv               = "MICLAW_SANDBOX_CHILD"
	sandboxEntrypoint             = "/usr/local/bin/miclaw"
	sandboxRuntimeImage           = "alpine:3.21"
	sandboxContainerNetNone       = "none"
	sandboxDefaultPATH            = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	sandboxHostBinHostDir         = "sandbox/host-bin"
	sandboxHostBinContDir         = "/opt/miclaw-host-bin"
	sandboxHostExecHostDir        = "sandbox/host-executor"
	sandboxHostExecSocketFile     = "host-executor.sock"
	sandboxHostExecSocketEnv      = "MICLAW_HOST_EXECUTOR_SOCK"
	sandboxHostExecSocketContPath = "/run/miclaw/host-executor.sock"
	sandboxHostExecClientName     = "host-executor-client"
)

func isSandboxChild() bool {
	return os.Getenv(sandboxChildEnv) == "1" && runningInDocker()
}

func runningInDocker() bool {
	_, err := os.Stat("/.dockerenv")
	return err == nil
}

func sandboxHostBridge(stateHostPath string, s config.SandboxConfig) ([]config.Mount, []string, error) {
	if len(s.HostCommands) == 0 {
		return nil, nil, nil
	}
	hostBinPath := filepath.Join(stateHostPath, sandboxHostBinHostDir)
	if err := writeHostCommandShims(hostBinPath, s.HostCommands); err != nil {
		return nil, nil, err
	}
	socketHostPath := sandboxHostExecutorSocketPath(stateHostPath)
	if err := ensureHostExecutorSocketPath(socketHostPath); err != nil {
		return nil, nil, err
	}
	mounts := []config.Mount{
		{Host: hostBinPath, Container: sandboxHostBinContDir, Mode: "ro"},
		{Host: socketHostPath, Container: sandboxHostExecSocketContPath, Mode: "rw"},
	}
	env := []string{
		sandboxHostExecSocketEnv + "=" + sandboxHostExecSocketContPath,
		"PATH=" + sandboxHostBinContDir + ":" + sandboxDefaultPATH,
	}
	return mounts, env, nil
}

func sandboxHostExecutorSocketPath(stateHostPath string) string {
	return filepath.Join(stateHostPath, sandboxHostExecHostDir, sandboxHostExecSocketFile)
}

func ensureHostExecutorSocketPath(socketPath string) error {
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		return fmt.Errorf("create host executor socket dir %q: %v", filepath.Dir(socketPath), err)
	}
	info, err := os.Stat(socketPath)
	if err == nil {
		if info.IsDir() {
			return fmt.Errorf("host executor socket path %q is a directory", socketPath)
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return fmt.Errorf("stat host executor socket %q: %v", socketPath, err)
	}
	file, err := os.OpenFile(socketPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create host executor socket placeholder %q: %v", socketPath, err)
	}
	return file.Close()
}

func writeHostCommandShims(dir string, commands []string) error {
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("reset host command shim dir %q: %v", dir, err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create host command shim dir %q: %v", dir, err)
	}
	clientPath := filepath.Join(dir, sandboxHostExecClientName)
	script := hostExecutorClientScript()
	if err := os.WriteFile(clientPath, []byte(script), 0o755); err != nil {
		return fmt.Errorf("write host command client %q: %v", clientPath, err)
	}
	seen := map[string]bool{}
	for _, command := range commands {
		command = strings.TrimSpace(command)
		if command == "" || seen[command] {
			continue
		}
		seen[command] = true
		path := filepath.Join(dir, command)
		if err := os.Symlink(sandboxHostExecClientName, path); err != nil {
			return fmt.Errorf("write host command shim %q: %v", path, err)
		}
	}
	return nil
}

func hostExecutorClientScript() string {
	return `#!/bin/sh
set -eu
cmd="$(basename "$0")"
if [ "$cmd" = "` + sandboxHostExecClientName + `" ]; then
  cmd="${1:-}"
  if [ -z "$cmd" ]; then
    echo "missing command" >&2
    exit 2
  fi
  shift
fi
exec /usr/local/bin/miclaw --host-exec-client "$cmd" "$@"
`
}

func resolveBindPath(path string) (string, error) {
	p, err := expandHome(path)
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(p) {
		return p, nil
	}
	return filepath.Abs(p)
}

func sandboxNetwork(cfg *config.Config) string {
	if cfg.Sandbox.Network == "" {
		return sandboxContainerNetNone
	}
	return cfg.Sandbox.Network
}

func dockerBindMount(m config.Mount) string {
	spec := "type=bind,source=" + m.Host + ",target=" + m.Container
	if m.Mode == "ro" {
		spec += ",readonly"
	}
	return spec
}
