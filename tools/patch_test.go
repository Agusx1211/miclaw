package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agusx1211/miclaw/model"
)

func TestPatchAppliesSimpleAddition(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(p, []byte("one\nthree\n"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	patch := "" +
		"--- a/f.txt\n" +
		"+++ b/f.txt\n" +
		"@@ -1,2 +1,3 @@\n" +
		" one\n" +
		"+two\n" +
		" three\n"
	got, err := runPatchCall(t, patchArgs{Path: p, Patch: patch})
	if err != nil {
		t.Fatalf("run patch: %v", err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(b) != "one\ntwo\nthree\n" {
		t.Fatalf("unexpected content: %q", string(b))
	}
	if !strings.Contains(got.Content, "hunk 1") {
		t.Fatalf("missing hunk summary: %q", got.Content)
	}
}

func TestPatchAppliesSimpleDeletion(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(p, []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	patch := "" +
		"@@ -1,3 +1,2 @@\n" +
		" one\n" +
		"-two\n" +
		" three\n"
	_, err := runPatchCall(t, patchArgs{Path: p, Patch: patch})
	if err != nil {
		t.Fatalf("run patch: %v", err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(b) != "one\nthree\n" {
		t.Fatalf("unexpected content: %q", string(b))
	}
}

func TestPatchAppliesSimpleModification(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(p, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	patch := "" +
		"@@ -1,2 +1,2 @@\n" +
		" one\n" +
		"-two\n" +
		"+TWO\n"
	_, err := runPatchCall(t, patchArgs{Path: p, Patch: patch})
	if err != nil {
		t.Fatalf("run patch: %v", err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(b) != "one\nTWO\n" {
		t.Fatalf("unexpected content: %q", string(b))
	}
}

func TestPatchRejectsBadFormat(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(p, []byte("one\n"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	_, err := runPatchCall(t, patchArgs{Path: p, Patch: "not-a-patch"})
	if err == nil {
		t.Fatal("want error for invalid patch format")
	}
}

func TestPatchFailsWhenFileMissing(t *testing.T) {
	p := filepath.Join(t.TempDir(), "missing.txt")
	patch := "" +
		"@@ -1,1 +1,1 @@\n" +
		"-x\n" +
		"+y\n"
	_, err := runPatchCall(t, patchArgs{Path: p, Patch: patch})
	if err == nil {
		t.Fatal("want error for missing file")
	}
}

type patchArgs struct {
	Path  string `json:"path"`
	Patch string `json:"patch"`
}

func runPatchCall(t *testing.T, args patchArgs) (ToolResult, error) {
	t.Helper()
	b, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	call := model.ToolCallPart{Name: "apply_patch", Parameters: b}
	return patchTool().Run(context.Background(), call)
}
