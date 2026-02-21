package sandbox

import (
	"path/filepath"
	"testing"
)

func TestIsHostCommandAllowlistedCommandWithArgs(t *testing.T) {
	if !IsHostCommand("git status", []string{"git", "docker"}) {
		t.Fatal("expected git status to be detected as host command")
	}
}

func TestIsHostCommandRejectsNotAllowlistedCommand(t *testing.T) {
	if IsHostCommand("rm -rf /", []string{"git", "docker"}) {
		t.Fatal("expected rm command to be rejected")
	}
}

func TestIsHostCommandMatchesExactCommandWithoutArgs(t *testing.T) {
	if !IsHostCommand("git", []string{"git"}) {
		t.Fatal("expected exact command match")
	}
}

func TestIsHostCommandRejectsEmptyCommand(t *testing.T) {
	if IsHostCommand("", []string{"git"}) {
		t.Fatal("expected empty command to be rejected")
	}
}

func TestIsHostCommandRejectsPrefixOnlyMatch(t *testing.T) {
	if IsHostCommand("gitx push", []string{"git"}) {
		t.Fatal("expected prefix-only command match to be rejected")
	}
}

func TestNewSSHExecutorRejectsMissingKeyFile(t *testing.T) {
	_, err := NewSSHExecutor(filepath.Join(t.TempDir(), "missing-key"), "runner", "host.docker.internal")
	if err == nil {
		t.Fatal("expected missing key file error")
	}
}
