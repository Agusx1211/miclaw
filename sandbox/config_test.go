package sandbox

import "testing"

import "github.com/agusx1211/miclaw/config"

func TestBuildDockerRunArgsNetworkModeNone(t *testing.T) {
	got := BuildDockerRunArgs(config.SandboxConfig{Network: "none"})
	want := []string{"--network=none", "--user", "1000:1000", "--restart", "unless-stopped"}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %d want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg[%d] got %q want %q", i, got[i], want[i])
		}
	}
}

func TestBuildDockerRunArgsReadOnlyMount(t *testing.T) {
	got := BuildDockerRunArgs(config.SandboxConfig{
		Network: "bridge",
		Mounts: []config.Mount{
			{Host: "/host/path", Container: "/container/path", Mode: "ro"},
		},
	})
	want := []string{
		"--network=bridge",
		"--mount", "type=bind,source=/host/path,target=/container/path,readonly",
		"--user", "1000:1000",
		"--restart", "unless-stopped",
	}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %d want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg[%d] got %q want %q", i, got[i], want[i])
		}
	}
}

func TestBuildDockerRunArgsReadWriteMount(t *testing.T) {
	got := BuildDockerRunArgs(config.SandboxConfig{
		Network: "none",
		Mounts: []config.Mount{
			{Host: "/host/path", Container: "/container/path", Mode: "rw"},
		},
	})
	want := []string{
		"--network=none",
		"--mount", "type=bind,source=/host/path,target=/container/path",
		"--user", "1000:1000",
		"--restart", "unless-stopped",
	}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %d want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg[%d] got %q want %q", i, got[i], want[i])
		}
	}
}

func TestBuildDockerRunArgsEmptyMounts(t *testing.T) {
	got := BuildDockerRunArgs(config.SandboxConfig{Network: "host"})
	for i := range got {
		if got[i] == "--mount" {
			t.Fatalf("unexpected mount flag at index %d", i)
		}
	}
}

func TestBuildDockerRunArgsMultipleMounts(t *testing.T) {
	got := BuildDockerRunArgs(config.SandboxConfig{
		Network: "none",
		Mounts: []config.Mount{
			{Host: "/host/a", Container: "/container/a", Mode: "ro"},
			{Host: "/host/b", Container: "/container/b", Mode: "rw"},
		},
	})
	want := []string{
		"--network=none",
		"--mount", "type=bind,source=/host/a,target=/container/a,readonly",
		"--mount", "type=bind,source=/host/b,target=/container/b",
		"--user", "1000:1000",
		"--restart", "unless-stopped",
	}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %d want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg[%d] got %q want %q", i, got[i], want[i])
		}
	}
}
