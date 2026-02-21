package prompt

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func writeWorkspaceFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write file %s: %v", name, err)
	}
	return p
}

func parseTruncatedChars(t *testing.T, body string) int {
	t.Helper()
	m := truncatedChars.FindStringSubmatch(body)
	if len(m) != 2 {
		t.Fatalf("expected truncated marker, got %q", body)
	}
	v, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatalf("invalid truncated count: %v", err)
	}
	return v
}

func TestLoadWorkspaceAllFilesPresent(t *testing.T) {
	t.Parallel()
	d := t.TempDir()
	writeWorkspaceFile(t, d, "SOUL.md", "soul")
	writeWorkspaceFile(t, d, "AGENTS.md", "agents")
	writeWorkspaceFile(t, d, "IDENTITY.md", "identity")
	writeWorkspaceFile(t, d, "USER.md", "user")
	writeWorkspaceFile(t, d, "MEMORY.md", "memory")
	writeWorkspaceFile(t, d, "HEARTBEAT.md", "heartbeat")

	ws, err := LoadWorkspace(d)
	if err != nil {
		t.Fatalf("LoadWorkspace failed: %v", err)
	}
	if ws.Soul != "soul" || ws.Agents != "agents" || ws.Identity != "identity" || ws.User != "user" || ws.Memory != "memory" || ws.Heartbeat != "heartbeat" {
		t.Fatalf("unexpected workspace content: %+v", ws)
	}
}

func TestLoadWorkspaceMissingFiles(t *testing.T) {
	t.Parallel()
	d := t.TempDir()
	writeWorkspaceFile(t, d, "SOUL.md", "soul")
	writeWorkspaceFile(t, d, "USER.md", "user")

	ws, err := LoadWorkspace(d)
	if err != nil {
		t.Fatalf("LoadWorkspace failed: %v", err)
	}
	if ws.Soul != "soul" {
		t.Fatalf("expected soul to load, got %q", ws.Soul)
	}
	if ws.User != "user" {
		t.Fatalf("expected user to load, got %q", ws.User)
	}
	if ws.Agents != "" || ws.Identity != "" || ws.Memory != "" || ws.Heartbeat != "" {
		t.Fatalf("expected missing files empty: %+v", ws)
	}
}

func TestLoadWorkspaceNonExistentPath(t *testing.T) {
	t.Parallel()
	if _, err := LoadWorkspace(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Fatal("expected error for missing directory")
	}
}

func TestLoadWorkspacePerFileTruncation(t *testing.T) {
	t.Parallel()
	d := t.TempDir()
	long := strings.Repeat("s", maxWorkspaceFileChars+999)
	writeWorkspaceFile(t, d, "SOUL.md", long)
	writeWorkspaceFile(t, d, "USER.md", "short")

	ws, err := LoadWorkspace(d)
	if err != nil {
		t.Fatalf("LoadWorkspace failed: %v", err)
	}
	if len(ws.Soul) != maxWorkspaceFileChars {
		t.Fatalf("expected soul max, got %d", len(ws.Soul))
	}
	if !strings.Contains(ws.Soul, "[truncated 999 chars]") {
		t.Fatalf("expected per-file truncation marker, got %q", ws.Soul)
	}
}

func TestLoadWorkspaceTotalTruncation(t *testing.T) {
	t.Parallel()
	d := t.TempDir()
	long := strings.Repeat("x", 50000)
	writeWorkspaceFile(t, d, "SOUL.md", long)
	writeWorkspaceFile(t, d, "AGENTS.md", long)
	writeWorkspaceFile(t, d, "IDENTITY.md", long)
	writeWorkspaceFile(t, d, "USER.md", long)
	writeWorkspaceFile(t, d, "MEMORY.md", long)
	writeWorkspaceFile(t, d, "HEARTBEAT.md", long)

	ws, err := LoadWorkspace(d)
	if err != nil {
		t.Fatalf("LoadWorkspace failed: %v", err)
	}
	total := len(ws.Soul) + len(ws.Agents) + len(ws.Identity) + len(ws.User) + len(ws.Memory) + len(ws.Heartbeat)
	if total > maxWorkspaceTotalChars {
		t.Fatalf("expected total truncation to apply, got %d", total)
	}

	counts := []int{
		parseTruncatedChars(t, ws.Soul),
		parseTruncatedChars(t, ws.Agents),
		parseTruncatedChars(t, ws.Identity),
		parseTruncatedChars(t, ws.User),
		parseTruncatedChars(t, ws.Memory),
		parseTruncatedChars(t, ws.Heartbeat),
	}
	for _, c := range counts {
		if c >= 50000-maxWorkspaceFileChars {
			t.Fatalf("expected total truncation, got only per-file truncation with %d", c)
		}
	}
}
