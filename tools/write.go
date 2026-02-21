package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/agusx1211/miclaw/model"
)

type writeParams struct {
	Path       string
	Content    string
	CreateDirs bool
}

func writeTool() Tool {
	params := JSONSchema{
		Type: "object",
		Properties: map[string]JSONSchema{
			"path": {
				Type: "string",
				Desc: "Path to the file to write",
			},
			"content": {
				Type: "string",
				Desc: "Full file content to write",
			},
			"create_dirs": {
				Type: "boolean",
				Desc: "Create parent directories when missing (default: true)",
			},
		},
		Required: []string{"path", "content"},
	}
	must(params.Type == "object", "write schema type must be object")
	must(len(params.Required) == 2, "write schema required fields mismatch")
	return tool{
		name:   "write",
		desc:   "Write content to a file, replacing existing content",
		params: params,
		runFn:  runWrite,
	}
}

func runWrite(ctx context.Context, call model.ToolCallPart) (ToolResult, error) {
	must(ctx != nil, "context must not be nil")
	must(call.Parameters != nil, "write parameters must not be nil")

	args, err := parseWriteParams(call.Parameters)
	if err != nil {
		return ToolResult{}, err
	}
	if err := ensureWriteParent(args.Path, args.CreateDirs); err != nil {
		return ToolResult{}, err
	}
	n, err := writeContent(args.Path, args.Content)
	if err != nil {
		return ToolResult{}, err
	}
	must(n == len([]byte(args.Content)), "write byte count mismatch")
	msg := fmt.Sprintf("wrote %d bytes to %s", n, args.Path)
	must(msg != "", "write result content must not be empty")
	return ToolResult{Content: msg}, nil
}

func parseWriteParams(raw json.RawMessage) (writeParams, error) {
	must(raw != nil, "write raw parameters must not be nil")
	must(len(raw) > 0, "write raw parameters must not be empty")

	var input struct {
		Path       *string `json:"path"`
		Content    *string `json:"content"`
		CreateDirs *bool   `json:"create_dirs"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return writeParams{}, fmt.Errorf("parse write parameters: %v", err)
	}
	if input.Path == nil || *input.Path == "" {
		return writeParams{}, errors.New("write parameter path is required")
	}
	if input.Content == nil {
		return writeParams{}, errors.New("write parameter content is required")
	}
	out := writeParams{Path: *input.Path, Content: *input.Content, CreateDirs: true}
	if input.CreateDirs != nil {
		out.CreateDirs = *input.CreateDirs
	}
	must(out.Path != "", "write path must not be empty")
	must(len([]byte(out.Content)) >= 0, "write content byte length must be non-negative")
	return out, nil
}

func ensureWriteParent(path string, createDirs bool) error {
	must(path != "", "write path must not be empty")
	must(filepath.Base(path) != "", "write path base must not be empty")

	parent := filepath.Dir(path)
	if createDirs {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return fmt.Errorf("create parent directories for %q: %v", path, err)
		}
		return nil
	}
	if _, err := os.Stat(parent); err != nil {
		return fmt.Errorf("parent directory %q: %v", parent, err)
	}
	must(parent != "", "write parent path must not be empty")
	must(filepath.IsAbs(path) || parent != "", "write parent dir state invalid")
	return nil
}

func writeContent(path, content string) (int, error) {
	must(path != "", "write path must not be empty")
	must(len([]byte(content)) >= 0, "write content byte length must be non-negative")

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return 0, fmt.Errorf("write file %q: %v", path, err)
	}
	n := len([]byte(content))
	must(n >= 0, "written byte count must be non-negative")
	must(n < 1<<30, "written byte count is too large")
	return n, nil
}
