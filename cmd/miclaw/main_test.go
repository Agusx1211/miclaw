package main

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionFlag(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run([]string{"--version"}, &stdout, &stderr); err != nil {
		t.Fatalf("run version: %v", err)
	}
	got := strings.TrimSpace(stdout.String())
	if !strings.HasPrefix(got, "miclaw ") {
		t.Fatalf("version output = %q", got)
	}
}

func TestConfigFlagParsing(t *testing.T) {
	t.Parallel()

	path, showVersion, err := parseFlags([]string{"--config", "/tmp/custom.json"})
	if err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	if path != "/tmp/custom.json" {
		t.Fatalf("config path = %q", path)
	}
	if showVersion {
		t.Fatalf("showVersion = true")
	}
}

func TestBuild(t *testing.T) {
	t.Parallel()

	cmd := exec.Command("go", "build", "./cmd/miclaw")
	cmd.Dir = filepath.Join("..", "..")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build ./cmd/miclaw failed: %v\n%s", err, out)
	}
}
