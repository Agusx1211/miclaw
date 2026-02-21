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

func runReadTool(t *testing.T, path string, offset *int, limit *int) (string, bool) {
	t.Helper()
	params := map[string]any{"path": path}
	if offset != nil {
		params["offset"] = *offset
	}
	if limit != nil {
		params["limit"] = *limit
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("failed to marshal read parameters: %v", err)
	}

	result, err := ReadTool().Run(context.Background(), model.ToolCallPart{Parameters: raw})
	if err != nil {
		t.Fatalf("read tool run returned error: %v", err)
	}
	if result.Content == "" && result.IsError {
		t.Fatalf("error result should provide message")
	}

	return result.Content, result.IsError
}

func TestReadToolReadsSmallFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "small.txt")
	if err := os.WriteFile(path, []byte("alpha\nbeta\ngamma"), 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	got, isErr := runReadTool(t, path, nil, nil)
	if isErr {
		t.Fatalf("small read should succeed")
	}
	if got != "     1\talpha\n     2\tbeta\n     3\tgamma\n" {
		t.Fatalf("line numbering mismatch: %q", got)
	}

}

func TestReadToolSupportsOffsetAndLimitPagination(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "paginated.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\nfour\nfive\n"), 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	off := 1
	lim := 2
	got, isErr := runReadTool(t, path, &off, &lim)
	if isErr {
		t.Fatalf("pagination read should succeed")
	}
	if got != "     2\ttwo\n     3\tthree\n" {
		t.Fatalf("pagination output incorrect: %q", got)
	}

}

func TestReadToolReturnsErrorForMissingFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "missing.txt")

	got, isErr := runReadTool(t, path, nil, nil)
	if !isErr {
		t.Fatalf("missing file should be error")
	}
	if !strings.Contains(got, "no such file") {
		t.Fatalf("missing-file message mismatch: %q", got)
	}

}

func TestReadToolReturnsErrorForBinaryFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "binary.bin")
	if err := os.WriteFile(path, []byte{0x00, 0x01, 0x7f}, 0o644); err != nil {
		t.Fatalf("failed to write binary fixture: %v", err)
	}

	got, isErr := runReadTool(t, path, nil, nil)
	if !isErr {
		t.Fatalf("binary read should be error")
	}
	if !strings.Contains(strings.ToLower(got), "binary") {
		t.Fatalf("binary error message mismatch: %q", got)
	}

}

func TestReadToolTruncatesLargeOutputs(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "large.txt")
	var b strings.Builder
	for i := 0; i < 900; i++ {
		b.WriteString(strings.Repeat("x", 700))
		b.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("failed to write large file: %v", err)
	}

	got, isErr := runReadTool(t, path, nil, nil)
	if isErr {
		t.Fatalf("large read should not error")
	}
	if len(got) > 512*1024 {
		t.Fatalf("large read should be truncated")
	}
	if !strings.Contains(got, "truncated") {
		t.Fatalf("truncation message missing")
	}

}

func TestReadToolUsesLineNumbersAfterOffset(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "offset.txt")
	if err := os.WriteFile(path, []byte("a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\n"), 0o644); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	off := 5
	lim := 3
	got, isErr := runReadTool(t, path, &off, &lim)
	if isErr {
		t.Fatalf("offset read should succeed")
	}
	if got != "     6\tf\n     7\tg\n     8\th\n" {
		t.Fatalf("line numbering after offset mismatch: %q", got)
	}

}

func TestReadToolReadsEmptyFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "empty.txt")
	if err := os.WriteFile(path, []byte{}, 0o644); err != nil {
		t.Fatalf("failed to write empty file: %v", err)
	}

	got, isErr := runReadTool(t, path, nil, nil)
	if isErr {
		t.Fatalf("empty file read should not error")
	}
	if got != "" {
		t.Fatalf("empty file should return empty content: %q", got)
	}

}
