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
	must(err == nil, "failed to marshal read parameters")
	result, err := ReadTool().Run(context.Background(), model.ToolCallPart{Parameters: raw})
	must(err == nil, "read tool run returned error")
	must(result.Content != "" || !result.IsError, "error result should provide message")
	return result.Content, result.IsError
}

func TestReadToolReadsSmallFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "small.txt")
	must(os.WriteFile(path, []byte("alpha\nbeta\ngamma"), 0o644) == nil, "failed to write test file")

	got, isErr := runReadTool(t, path, nil, nil)
	must(!isErr, "small read should succeed")
	must(got == "     1\talpha\n     2\tbeta\n     3\tgamma\n", "line numbering mismatch")
}

func TestReadToolSupportsOffsetAndLimitPagination(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "paginated.txt")
	must(
		os.WriteFile(path, []byte("one\ntwo\nthree\nfour\nfive\n"), 0o644) == nil,
		"failed to write test file",
	)
	off := 1
	lim := 2
	got, isErr := runReadTool(t, path, &off, &lim)
	must(!isErr, "pagination read should succeed")
	must(got == "     2\ttwo\n     3\tthree\n", "pagination output incorrect")
}

func TestReadToolReturnsErrorForMissingFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "missing.txt")

	got, isErr := runReadTool(t, path, nil, nil)
	must(isErr, "missing file should be error")
	must(strings.Contains(got, "no such file"), "missing-file message mismatch")
}

func TestReadToolReturnsErrorForBinaryFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "binary.bin")
	must(os.WriteFile(path, []byte{0x00, 0x01, 0x7f}, 0o644) == nil, "failed to write binary fixture")

	got, isErr := runReadTool(t, path, nil, nil)
	must(isErr, "binary read should be error")
	must(strings.Contains(strings.ToLower(got), "binary"), "binary error message mismatch")
}

func TestReadToolTruncatesLargeOutputs(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "large.txt")
	var b strings.Builder
	for i := 0; i < 900; i++ {
		b.WriteString(strings.Repeat("x", 700))
		b.WriteByte('\n')
	}
	must(os.WriteFile(path, []byte(b.String()), 0o644) == nil, "failed to write large file")

	got, isErr := runReadTool(t, path, nil, nil)
	must(!isErr, "large read should not error")
	must(len(got) <= 512*1024, "large read should be truncated")
	must(strings.Contains(got, "truncated"), "truncation message missing")
}

func TestReadToolUsesLineNumbersAfterOffset(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "offset.txt")
	must(
		os.WriteFile(path, []byte("a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\n"), 0o644) == nil,
		"failed to write fixture",
	)
	off := 5
	lim := 3
	got, isErr := runReadTool(t, path, &off, &lim)
	must(!isErr, "offset read should succeed")
	must(got == "     6\tf\n     7\tg\n     8\th\n", "line numbering after offset mismatch")
}

func TestReadToolReadsEmptyFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "empty.txt")
	must(os.WriteFile(path, []byte{}, 0o644) == nil, "failed to write empty file")

	got, isErr := runReadTool(t, path, nil, nil)
	must(!isErr, "empty file read should not error")
	must(got == "", "empty file should return empty content")
}
