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

	return tool{
		name:   "write",
		desc:   "Write content to a file, replacing existing content",
		params: params,
		runFn:  runWrite,
	}
}

func runWrite(ctx context.Context, call model.ToolCallPart) (ToolResult, error) {

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

	msg := fmt.Sprintf("wrote %d bytes to %s", n, args.Path)

	return ToolResult{Content: msg}, nil
}

func parseWriteParams(raw json.RawMessage) (writeParams, error) {

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

	return out, nil
}

func ensureWriteParent(path string, createDirs bool) error {

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

	return nil
}

func writeContent(path, content string) (int, error) {

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return 0, fmt.Errorf("write file %q: %v", path, err)
	}
	n := len([]byte(content))

	return n, nil
}
