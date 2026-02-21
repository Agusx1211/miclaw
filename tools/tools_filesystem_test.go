package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/agusx1211/miclaw/model"
)

func TestGrepToolFindsPattern(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("alpha\nmatch line\nomega"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.txt"), []byte("nothing here"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	got, err := runTool(t, grepTool(), map[string]any{
		"pattern": "match",
		"path":    root,
	})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if got.IsError {
		t.Fatalf("unexpected tool error: %s", got.Content)
	}
	expected := filepath.Join(root, "a.txt") + ":2:match line"
	if !strings.Contains(got.Content, expected) {
		t.Fatalf("missing expected result: %q in %q", expected, got.Content)
	}
}

func TestGrepToolReturnsContextLines(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("one\ntwo\nmatch\nfour\nfive"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	got, err := runTool(t, grepTool(), map[string]any{
		"pattern":       "match",
		"path":          root,
		"context_lines": 1,
	})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	base := filepath.Join(root, "a.txt")
	for _, want := range []string{
		base + ":2:two",
		base + ":3:match",
		base + ":4:four",
	} {
		if !strings.Contains(got.Content, want) {
			t.Fatalf("context output missing %q: %q", want, got.Content)
		}
	}
}

func TestGrepToolSupportsRegex(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("foo1\nfoo2\nbar"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	got, err := runTool(t, grepTool(), map[string]any{
		"pattern": "foo[0-9]",
		"path":    root,
	})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	base := filepath.Join(root, "a.txt")
	for _, want := range []string{base + ":1:foo1", base + ":2:foo2"} {
		if !strings.Contains(got.Content, want) {
			t.Fatalf("missing regex result %q", want)
		}
	}
}

func TestGrepToolNoMatches(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello\nworld"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	got, err := runTool(t, grepTool(), map[string]any{"pattern": "absent", "path": root})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if got.IsError {
		t.Fatalf("unexpected tool error: %s", got.Content)
	}
	if got.Content != "" {
		t.Fatalf("expected empty output, got %q", got.Content)
	}
}

func TestGrepToolSkipsBinary(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "text.txt"), []byte("binary\nneedle\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "bin.dat"), []byte("needle\x00binary"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	got, err := runTool(t, grepTool(), map[string]any{"pattern": "needle", "path": root})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if !strings.Contains(got.Content, filepath.Join(root, "text.txt")+":2:needle") {
		t.Fatalf("expected text match: %q", got.Content)
	}
	if strings.Contains(got.Content, "bin.dat") {
		t.Fatalf("binary file should be skipped: %q", got.Content)
	}
}

func TestGrepToolRespectsGitignore(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("ignored.txt\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "ignored.txt"), []byte("skip\nneedle"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "kept.txt"), []byte("needle"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	got, err := runTool(t, grepTool(), map[string]any{"pattern": "needle", "path": root})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if strings.Contains(got.Content, "ignored.txt") {
		t.Fatalf("gitignore file should be skipped: %q", got.Content)
	}
	if !strings.Contains(got.Content, filepath.Join(root, "kept.txt")+":1:needle") {
		t.Fatalf("expected kept file match: %q", got.Content)
	}
}

func TestGlobToolSimplePattern(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.md"), []byte("b"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "c.txt"), []byte("c"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	got, err := runTool(t, globTool(), map[string]any{"pattern": "*.txt", "path": root})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if got.Content != "a.txt" {
		t.Fatalf("unexpected glob results: %q", got.Content)
	}
}

func TestGlobToolRecursivePattern(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	got, err := runTool(t, globTool(), map[string]any{"pattern": "**/*.txt", "path": root})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	gotLines := strings.Split(strings.TrimSpace(got.Content), "\n")
	sort.Strings(gotLines)
	want := []string{"a.txt", filepath.ToSlash(filepath.Join("sub", "b.txt"))}
	if len(gotLines) != len(want) {
		t.Fatalf("want %d results, got %d", len(want), len(gotLines))
	}
	for i, w := range want {
		if gotLines[i] != w {
			t.Fatalf("want %q, got %q", w, gotLines[i])
		}
	}
}

func TestGlobToolNoMatches(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	got, err := runTool(t, globTool(), map[string]any{"pattern": "*.go", "path": root})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if got.Content != "" {
		t.Fatalf("expected no glob matches, got %q", got.Content)
	}
}

func TestLSToolFlatListing(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.log"), []byte("b"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	got, err := runTool(t, lsTool(), map[string]any{"path": root, "depth": 1})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	for _, want := range []string{
		"a.txt (file, 1)",
		"b.log (file, 1)",
		"sub (dir,",
	} {
		if !strings.Contains(got.Content, want) {
			t.Fatalf("missing %q in %q", want, got.Content)
		}
	}
}

func TestLSToolNestedDepth(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "inner.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	got, err := runTool(t, lsTool(), map[string]any{"path": root, "depth": 2})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if !strings.Contains(got.Content, "sub (dir,") {
		t.Fatalf("missing nested directory listing: %q", got.Content)
	}
	if !strings.Contains(got.Content, "inner.txt (file, 1)") {
		t.Fatalf("missing nested file listing: %q", got.Content)
	}
}

func TestLSToolHiddenFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".hidden"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "visible"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	gotHidden, err := runTool(t, lsTool(), map[string]any{"path": root, "show_hidden": false})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if strings.Contains(gotHidden.Content, ".hidden") {
		t.Fatalf("hidden file should be omitted: %q", gotHidden.Content)
	}
	gotVisible, err := runTool(t, lsTool(), map[string]any{"path": root, "show_hidden": true})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if !strings.Contains(gotVisible.Content, ".hidden") {
		t.Fatalf("hidden file should be shown: %q", gotVisible.Content)
	}
}

func TestLSToolNonExistentDirError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	missing := filepath.Join(root, "does-not-exist")
	got, err := runTool(t, lsTool(), map[string]any{"path": missing})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if !got.IsError {
		t.Fatalf("expected ls error for missing directory")
	}
}

func runTool(t *testing.T, tool Tool, params map[string]any) (ToolResult, error) {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("json marshal: %v", err)
	}
	return tool.Run(context.Background(), model.ToolCallPart{
		ID:         "1",
		Name:       tool.Name(),
		Parameters: raw,
	})
}
