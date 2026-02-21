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

func TestEditReplacesUniqueMatch(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(p, []byte("alpha beta gamma"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	got, err := runEditCall(t, editArgs{Path: p, OldText: "beta", NewText: "BETA"})
	if err != nil {
		t.Fatalf("run edit: %v", err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(b) != "alpha BETA gamma" {
		t.Fatalf("unexpected content: %q", string(b))
	}
	if !strings.Contains(got.Content, "@@") {
		t.Fatalf("missing diff-style marker: %q", got.Content)
	}
}

func TestEditRejectsNonUniqueMatch(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(p, []byte("x x x"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	_, err := runEditCall(t, editArgs{Path: p, OldText: "x", NewText: "y"})
	if err == nil {
		t.Fatal("want error for non-unique old_text")
	}
}

func TestEditReplaceAllReplacesEveryOccurrence(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(p, []byte("x x x"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	_, err := runEditCall(t, editArgs{
		Path:       p,
		OldText:    "x",
		NewText:    "y",
		ReplaceAll: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("run edit: %v", err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(b) != "y y y" {
		t.Fatalf("unexpected content: %q", string(b))
	}
}

func TestEditFailsWhenOldTextMissing(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(p, []byte("alpha"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	_, err := runEditCall(t, editArgs{Path: p, OldText: "beta", NewText: "BETA"})
	if err == nil {
		t.Fatal("want error when old_text is missing")
	}
}

func TestEditFailsWhenFileMissing(t *testing.T) {
	p := filepath.Join(t.TempDir(), "missing.txt")
	_, err := runEditCall(t, editArgs{Path: p, OldText: "a", NewText: "b"})
	if err == nil {
		t.Fatal("want error for missing file")
	}
}

type editArgs struct {
	Path       string `json:"path"`
	OldText    string `json:"old_text"`
	NewText    string `json:"new_text"`
	ReplaceAll *bool  `json:"replace_all,omitempty"`
}

func runEditCall(t *testing.T, args editArgs) (ToolResult, error) {
	t.Helper()
	b, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	call := model.ToolCallPart{Name: "edit", Parameters: b}
	return editTool().Run(context.Background(), call)
}
