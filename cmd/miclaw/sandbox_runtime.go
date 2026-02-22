package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/agusx1211/miclaw/config"
)

const (
	sandboxChildEnv          = "MICLAW_SANDBOX_CHILD"
	sandboxEntrypoint        = "/usr/local/bin/miclaw"
	sandboxRuntimeImage      = "alpine:3.21"
	sandboxContainerNetNone  = "none"
	sandboxDockerHost        = "host.docker.internal"
	sandboxDockerHostGateway = "host-gateway"
	sandboxDefaultPATH       = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	sandboxHostBinHostDir    = "sandbox/host-bin"
	sandboxHostBinContDir    = "/opt/miclaw-host-bin"
	sandboxSSHKeyContPath    = "/run/miclaw/ssh_key"
	sandboxShimHostEnv       = "MICLAW_HOST_SHIM_HOST"
	sandboxShimUserEnv       = "MICLAW_HOST_SHIM_USER"
	sandboxShimKeyEnv        = "MICLAW_HOST_SHIM_KEY"
)

func isSandboxChild() bool {
	return os.Getenv(sandboxChildEnv) == "1" && runningInDocker()
}

func runningInDocker() bool {
	_, err := os.Stat("/.dockerenv")
	return err == nil
}

func sandboxHostBridge(stateHostPath string, s config.SandboxConfig) ([]config.Mount, []string, bool, error) {
	if len(s.HostCommands) == 0 {
		return nil, nil, false, nil
	}
	keyHostPath, err := resolveBindPath(s.SSHKeyPath)
	if err != nil {
		return nil, nil, false, fmt.Errorf("sandbox ssh key path: %v", err)
	}
	hostBinPath := filepath.Join(stateHostPath, sandboxHostBinHostDir)
	if err := writeHostCommandShims(hostBinPath, s.HostCommands); err != nil {
		return nil, nil, false, err
	}
	mounts := []config.Mount{
		{Host: keyHostPath, Container: sandboxSSHKeyContPath, Mode: "ro"},
		{Host: hostBinPath, Container: sandboxHostBinContDir, Mode: "ro"},
	}
	env := []string{
		sandboxShimHostEnv + "=" + sandboxDockerHost,
		sandboxShimUserEnv + "=" + s.HostUser,
		sandboxShimKeyEnv + "=" + sandboxSSHKeyContPath,
		"PATH=" + sandboxHostBinContDir + ":" + sandboxDefaultPATH,
	}
	return mounts, env, true, nil
}

func writeHostCommandShims(dir string, commands []string) error {
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("reset host command shim dir %q: %v", dir, err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create host command shim dir %q: %v", dir, err)
	}
	script := hostCommandShimScript()
	for _, command := range commands {
		command = strings.TrimSpace(command)
		if command == "" {
			continue
		}
		path := filepath.Join(dir, command)
		if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
			return fmt.Errorf("write host command shim %q: %v", path, err)
		}
	}
	return nil
}

func hostCommandShimScript() string {
	return `#!/bin/sh
set -eu
cmd="$(basename "$0")"
host="${MICLAW_HOST_SHIM_HOST:?}"
user="${MICLAW_HOST_SHIM_USER:?}"
key="${MICLAW_HOST_SHIM_KEY:?}"
if [ -t 0 ] && [ -t 1 ]; then
  exec ssh -tt -i "$key" -o BatchMode=yes -o StrictHostKeyChecking=accept-new -o UserKnownHostsFile=/tmp/miclaw_known_hosts "$user@$host" -- "$cmd" "$@"
fi
exec ssh -T -i "$key" -o BatchMode=yes -o StrictHostKeyChecking=accept-new -o UserKnownHostsFile=/tmp/miclaw_known_hosts "$user@$host" -- "$cmd" "$@"
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
