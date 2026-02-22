package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agusx1211/miclaw/model"
	"github.com/agusx1211/miclaw/tools"
)

func TestRunToolCallExecutesBridgeTool(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "sample.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write sample: %v", err)
	}
	payload := encodeToolCall(t, model.ToolCallPart{
		ID:         "call-1",
		Name:       "read",
		Parameters: json.RawMessage(`{"path":"` + path + `","offset":0,"limit":1}`),
	})
	var out bytes.Buffer
	if err := runToolCall(payload, &out); err != nil {
		t.Fatalf("run tool call: %v", err)
	}
	var result tools.ToolResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("decode tool result: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error result: %#v", result)
	}
	if !strings.Contains(result.Content, "hello") {
		t.Fatalf("unexpected tool content: %q", result.Content)
	}
}

func TestRunToolCallRejectsUnsupportedBridgeTool(t *testing.T) {
	payload := encodeToolCall(t, model.ToolCallPart{
		ID:         "call-2",
		Name:       "process",
		Parameters: json.RawMessage(`{"action":"status","pid":1}`),
	})
	var out bytes.Buffer
	err := runToolCall(payload, &out)
	if err == nil {
		t.Fatal("expected unsupported bridge tool error")
	}
	if !strings.Contains(err.Error(), `unsupported bridge tool "process"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func encodeToolCall(t *testing.T, call model.ToolCallPart) string {
	t.Helper()
	raw, err := json.Marshal(call)
	if err != nil {
		t.Fatalf("marshal tool call: %v", err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}
