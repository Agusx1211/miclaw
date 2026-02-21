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

func TestWriteCreatesNewFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.txt")
	got, err := runWriteCall(t, writeArgs{Path: p, Content: "hello"})
	if err != nil {
		t.Fatalf("run write: %v", err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(b) != "hello" {
		t.Fatalf("want hello, got %q", string(b))
	}
	if !strings.Contains(got.Content, "5 bytes") {
		t.Fatalf("missing byte count: %q", got.Content)
	}
}

func TestWriteOverwritesExistingFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(p, []byte("old"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	_, err := runWriteCall(t, writeArgs{Path: p, Content: "new-content"})
	if err != nil {
		t.Fatalf("run write: %v", err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(b) != "new-content" {
		t.Fatalf("want new-content, got %q", string(b))
	}
}

func TestWriteCreatesNestedDirs(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a", "b", "c.txt")
	_, err := runWriteCall(t, writeArgs{Path: p, Content: "ok"})
	if err != nil {
		t.Fatalf("run write: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(p)); err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(b) != "ok" {
		t.Fatalf("want ok, got %q", string(b))
	}
}

func TestWriteFailsWhenParentMissingAndCreateDirsFalse(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a", "b", "c.txt")
	_, err := runWriteCall(t, writeArgs{Path: p, Content: "x", CreateDirs: boolPtr(false)})
	if err == nil {
		t.Fatal("want error for missing parent directory")
	}
	if _, statErr := os.Stat(p); statErr == nil {
		t.Fatal("file should not exist on failure")
	}
}

type writeArgs struct {
	Path       string `json:"path"`
	Content    string `json:"content"`
	CreateDirs *bool  `json:"create_dirs,omitempty"`
}

func runWriteCall(t *testing.T, args writeArgs) (ToolResult, error) {
	t.Helper()
	b, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	call := model.ToolCallPart{Name: "write", Parameters: b}
	return writeTool().Run(context.Background(), call)
}

func boolPtr(v bool) *bool {
	return &v
}
