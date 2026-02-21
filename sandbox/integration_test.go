//go:build integration && docker

package sandbox

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agusx1211/miclaw/config"
)

func containerName(t *testing.T) string {
	t.Helper()
	base := strings.NewReplacer("/", "-", "_", "-").Replace(strings.ToLower(t.Name()))
	return "miclaw-it-" + base + "-" + randomSuffix(t)
}

func randomSuffix(t *testing.T) string {
	t.Helper()
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("random suffix: %v", err)
	}
	return hex.EncodeToString(b)
}

func requireDocker(t *testing.T) {
	t.Helper()
	out, err := runDockerTimeout(t, 15*time.Second, "info")
	if err != nil {
		t.Skipf("docker not available: %v (%s)", err, strings.TrimSpace(out))
	}
}

func buildTestImage(t *testing.T) string {
	t.Helper()
	requireDocker(t)
	tag := "miclaw-test-" + randomSuffix(t)
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("resolve project root: %v", err)
	}
	out, err := runDockerTimeout(t, 2*time.Minute, "build", "-t", tag, root)
	if err != nil {
		t.Fatalf("docker build failed: %v\n%s", err, out)
	}
	t.Cleanup(func() { cleanupDocker("rmi", "-f", tag) })
	return tag
}

func runDocker(t *testing.T, args ...string) (string, error) {
	t.Helper()
	return runDockerTimeout(t, 60*time.Second, args...)
}

func runDockerTimeout(t *testing.T, timeout time.Duration, args ...string) (string, error) {
	t.Helper()
	out, err := dockerCommand(timeout, args...)
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("docker %s timed out: %s", strings.Join(args, " "), strings.TrimSpace(out))
	}
	return out, err
}

func dockerCommand(timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(out), ctx.Err()
	}
	return string(out), err
}

func cleanupDocker(args ...string) {
	_, _ = dockerCommand(20*time.Second, args...)
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func TestIntegrationDockerImageBuild(t *testing.T) {
	_ = buildTestImage(t)
}

func TestIntegrationContainerRunNetworkNone(t *testing.T) {
	image := buildTestImage(t)
	name := containerName(t)
	t.Cleanup(func() { cleanupDocker("rm", "-f", name) })

	out, err := runDocker(t,
		"run", "--name", name, "--network=none", "--entrypoint", "sh", image,
		"-c", "ping -c1 -W1 8.8.8.8",
	)
	if err == nil || exitCode(err) == 0 {
		t.Fatalf("expected non-zero exit code for ping in network=none container, output: %s", strings.TrimSpace(out))
	}
}

func TestIntegrationMountReadOnly(t *testing.T) {
	image := buildTestImage(t)
	hostDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(hostDir, "file.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	mount := "type=bind,source=" + hostDir + ",target=/mnt/test,readonly"

	readName := containerName(t)
	t.Cleanup(func() { cleanupDocker("rm", "-f", readName) })
	out, err := runDocker(t,
		"run", "--name", readName, "--mount", mount, "--entrypoint", "sh", image,
		"-c", "cat /mnt/test/file.txt",
	)
	if err != nil {
		t.Fatalf("read-only mount should allow reads: %v\n%s", err, out)
	}
	if strings.TrimSpace(out) != "hello" {
		t.Fatalf("unexpected file content: %q", strings.TrimSpace(out))
	}

	writeName := containerName(t)
	t.Cleanup(func() { cleanupDocker("rm", "-f", writeName) })
	out, err = runDocker(t,
		"run", "--name", writeName, "--mount", mount, "--entrypoint", "sh", image,
		"-c", "touch /mnt/test/newfile",
	)
	if err == nil || exitCode(err) == 0 {
		t.Fatalf("expected write to fail for read-only mount, output: %s", strings.TrimSpace(out))
	}
}

func TestIntegrationMountReadWrite(t *testing.T) {
	image := buildTestImage(t)
	hostDir := t.TempDir()
	mount := "type=bind,source=" + hostDir + ",target=/mnt/test"
	name := containerName(t)
	t.Cleanup(func() { cleanupDocker("rm", "-f", name) })

	out, err := runDocker(t,
		"run", "--name", name, "--mount", mount, "--entrypoint", "sh", image,
		"-c", "echo hello > /mnt/test/output.txt",
	)
	if err != nil {
		t.Fatalf("read-write mount should allow writes: %v\n%s", err, out)
	}
	data, err := os.ReadFile(filepath.Join(hostDir, "output.txt"))
	if err != nil {
		t.Fatalf("read output file from host: %v", err)
	}
	if strings.TrimSpace(string(data)) != "hello" {
		t.Fatalf("unexpected host file content: %q", strings.TrimSpace(string(data)))
	}
}

func TestIntegrationBuildDockerRunArgsUsed(t *testing.T) {
	image := buildTestImage(t)
	hostDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(hostDir, "file.txt"), []byte("ok\n"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	args := BuildDockerRunArgs(config.SandboxConfig{
		Network: "none",
		Mounts:  []config.Mount{{Host: hostDir, Container: "/mnt/test", Mode: "ro"}},
	})
	name := containerName(t)
	t.Cleanup(func() { cleanupDocker("rm", "-f", name) })

	runArgs := append([]string{"run", "-d", "--name", name}, args...)
	runArgs = append(runArgs, "--entrypoint", "sh", image, "-c", "sleep 45")
	out, err := runDocker(t, runArgs...)
	if err != nil {
		t.Fatalf("start container with BuildDockerRunArgs failed: %v\n%s", err, out)
	}

	out, err = runDocker(t, "exec", name, "sh", "-c", "cat /mnt/test/file.txt")
	if err != nil || strings.TrimSpace(out) != "ok" {
		t.Fatalf("expected mounted file to be readable: %v\n%s", err, out)
	}
	out, err = runDocker(t, "exec", name, "sh", "-c", "touch /mnt/test/newfile")
	if err == nil || exitCode(err) == 0 {
		t.Fatalf("expected read-only mount from BuildDockerRunArgs to reject writes: %s", strings.TrimSpace(out))
	}
	out, err = runDocker(t, "exec", name, "sh", "-c", "ping -c1 -W1 8.8.8.8")
	if err == nil || exitCode(err) == 0 {
		t.Fatalf("expected network=none from BuildDockerRunArgs to block ping: %s", strings.TrimSpace(out))
	}
}
