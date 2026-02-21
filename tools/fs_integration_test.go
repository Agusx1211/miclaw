package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agusx1211/miclaw/model"
)

func callToolRaw(t *testing.T, tl Tool, params map[string]any) (ToolResult, error) {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal %s params: %v", tl.Name(), err)
	}
	return tl.Run(context.Background(), model.ToolCallPart{
		ID:         "1",
		Name:       tl.Name(),
		Parameters: raw,
	})
}

func resultLines(content string) []string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

func TestFSWriteReadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	writeRes := callTool(t, writeTool(), map[string]any{
		"path":    path,
		"content": "alpha\nbeta\n",
	})
	if writeRes.IsError {
		t.Fatalf("write returned error result: %q", writeRes.Content)
	}
	readRes := callTool(t, ReadTool(), map[string]any{"path": path})
	if readRes.IsError {
		t.Fatalf("read returned error result: %q", readRes.Content)
	}
	if readRes.Content != "     1\talpha\n     2\tbeta\n" {
		t.Fatalf("unexpected read content: %q", readRes.Content)
	}
}

func TestFSWriteEditRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.txt")
	callTool(t, writeTool(), map[string]any{
		"path":    path,
		"content": "start\nold value\nend\n",
	})
	editRes := callTool(t, editTool(), map[string]any{
		"path":     path,
		"old_text": "old value",
		"new_text": "new value",
	})
	if editRes.IsError {
		t.Fatalf("edit returned error result: %q", editRes.Content)
	}
	readRes := callTool(t, ReadTool(), map[string]any{"path": path})
	if readRes.IsError {
		t.Fatalf("read returned error result: %q", readRes.Content)
	}
	if readRes.Content != "     1\tstart\n     2\tnew value\n     3\tend\n" {
		t.Fatalf("unexpected content after edit: %q", readRes.Content)
	}
}

func TestFSWriteGrepFindsPattern(t *testing.T) {
	dir := t.TempDir()
	callTool(t, writeTool(), map[string]any{
		"path":    filepath.Join(dir, "a.txt"),
		"content": "alpha needle",
	})
	callTool(t, writeTool(), map[string]any{
		"path":    filepath.Join(dir, "sub", "b.txt"),
		"content": "beta needle",
	})
	grepRes := callTool(t, grepTool(), map[string]any{
		"pattern": "needle",
		"path":    dir,
	})
	if grepRes.IsError {
		t.Fatalf("grep returned error result: %q", grepRes.Content)
	}
	for _, want := range []string{"a.txt:1:alpha needle", "sub/b.txt:1:beta needle"} {
		if !strings.Contains(grepRes.Content, want) {
			t.Fatalf("missing grep match %q in %q", want, grepRes.Content)
		}
	}
}

func TestFSGlobAfterWrites(t *testing.T) {
	dir := t.TempDir()
	callTool(t, writeTool(), map[string]any{"path": filepath.Join(dir, "a.go"), "content": "package main"})
	callTool(t, writeTool(), map[string]any{"path": filepath.Join(dir, "b.go"), "content": "package main"})
	callTool(t, writeTool(), map[string]any{"path": filepath.Join(dir, "c.txt"), "content": "text"})
	globRes := callTool(t, globTool(), map[string]any{
		"pattern": "*.go",
		"path":    dir,
	})
	if globRes.IsError {
		t.Fatalf("glob returned error result: %q", globRes.Content)
	}
	lines := resultLines(globRes.Content)
	if len(lines) != 2 || lines[0] != "a.go" || lines[1] != "b.go" {
		t.Fatalf("unexpected glob results: %q", globRes.Content)
	}
}

func TestFSLsAtDifferentDepths(t *testing.T) {
	dir := t.TempDir()
	callTool(t, writeTool(), map[string]any{
		"path":    filepath.Join(dir, "a", "b", "c", "leaf.txt"),
		"content": "x",
	})
	shallow := callTool(t, lsTool(), map[string]any{"path": dir, "depth": 1})
	if shallow.IsError {
		t.Fatalf("ls depth 1 returned error: %q", shallow.Content)
	}
	if !strings.Contains(shallow.Content, "a (dir,") {
		t.Fatalf("depth 1 should include a: %q", shallow.Content)
	}
	if strings.Contains(shallow.Content, "b (dir,") || strings.Contains(shallow.Content, "c (dir,") {
		t.Fatalf("depth 1 should not include nested dirs: %q", shallow.Content)
	}
	deep := callTool(t, lsTool(), map[string]any{"path": dir, "depth": 3})
	if deep.IsError {
		t.Fatalf("ls depth 3 returned error: %q", deep.Content)
	}
	for _, want := range []string{"a (dir,", "b (dir,", "c (dir,"} {
		if !strings.Contains(deep.Content, want) {
			t.Fatalf("depth 3 missing %q in %q", want, deep.Content)
		}
	}
}

func TestFSApplyPatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	callTool(t, writeTool(), map[string]any{
		"path":    path,
		"content": "one\ntwo\nthree\n",
	})
	patchRes := callTool(t, patchTool(), map[string]any{
		"path": path,
		"patch": "" +
			"@@ -1,3 +1,3 @@\n" +
			" one\n" +
			"-two\n" +
			" three\n" +
			"+four\n",
	})
	if patchRes.IsError {
		t.Fatalf("apply_patch returned error result: %q", patchRes.Content)
	}
	readRes := callTool(t, ReadTool(), map[string]any{"path": path})
	if readRes.IsError {
		t.Fatalf("read returned error result: %q", readRes.Content)
	}
	if readRes.Content != "     1\tone\n     2\tthree\n     3\tfour\n" {
		t.Fatalf("unexpected patched content: %q", readRes.Content)
	}
}

func TestFSEditRejectsNonUniqueMatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dupe.txt")
	callTool(t, writeTool(), map[string]any{
		"path":    path,
		"content": "hello\nhello\n",
	})
	res, err := callToolRaw(t, editTool(), map[string]any{
		"path":     path,
		"old_text": "hello",
		"new_text": "hi",
	})
	if err != nil {
		res = ToolResult{Content: err.Error(), IsError: true}
	}
	if !res.IsError {
		t.Fatalf("expected non-unique edit to be an error")
	}
	if !strings.Contains(res.Content, "must be unique") {
		t.Fatalf("unexpected non-unique error message: %q", res.Content)
	}
}

func TestFSLargeFileReadWithPagination(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.txt")
	var b strings.Builder
	for i := range 2000 {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	callTool(t, writeTool(), map[string]any{
		"path":    path,
		"content": b.String(),
	})
	readRes := callTool(t, ReadTool(), map[string]any{
		"path":   path,
		"offset": 500,
		"limit":  100,
	})
	if readRes.IsError {
		t.Fatalf("paginated read returned error: %q", readRes.Content)
	}
	if !strings.Contains(readRes.Content, fmt.Sprintf("%6d\tline %d\n", 501, 500)) {
		t.Fatalf("missing first paginated line: %q", readRes.Content)
	}
	if !strings.Contains(readRes.Content, fmt.Sprintf("%6d\tline %d\n", 600, 599)) {
		t.Fatalf("missing last paginated line: %q", readRes.Content)
	}
	if strings.Contains(readRes.Content, fmt.Sprintf("%6d\tline %d\n", 500, 499)) {
		t.Fatalf("unexpected line before page start: %q", readRes.Content)
	}
	if strings.Contains(readRes.Content, fmt.Sprintf("%6d\tline %d\n", 601, 600)) {
		t.Fatalf("unexpected line after page end: %q", readRes.Content)
	}
}
