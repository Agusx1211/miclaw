package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/agusx1211/miclaw/model"
)

type editParams struct {
	Path       string
	OldText    string
	NewText    string
	ReplaceAll bool
}

func editTool() Tool {
	params := JSONSchema{
		Type: "object",
		Properties: map[string]JSONSchema{
			"path": {
				Type: "string",
				Desc: "Path to the file to edit",
			},
			"old_text": {
				Type: "string",
				Desc: "Exact text to replace",
			},
			"new_text": {
				Type: "string",
				Desc: "Replacement text",
			},
			"replace_all": {
				Type: "boolean",
				Desc: "Replace all occurrences instead of requiring a unique match (default: false)",
			},
		},
		Required: []string{"path", "old_text", "new_text"},
	}

	return tool{
		name:   "edit",
		desc:   "Replace text in an existing file",
		params: params,
		runFn:  runEdit,
	}
}

func runEdit(ctx context.Context, call model.ToolCallPart) (ToolResult, error) {

	args, err := parseEditParams(call.Parameters)
	if err != nil {
		return ToolResult{}, err
	}
	before, err := readExistingFile(args.Path)
	if err != nil {
		return ToolResult{}, err
	}
	after, count, err := editContent(before, args)
	if err != nil {
		return ToolResult{}, err
	}
	if err := os.WriteFile(args.Path, []byte(after), 0o644); err != nil {
		return ToolResult{}, fmt.Errorf("write file %q: %v", args.Path, err)
	}
	msg := editSummary(args, count)

	return ToolResult{Content: msg}, nil
}

func parseEditParams(raw json.RawMessage) (editParams, error) {

	var input struct {
		Path       *string `json:"path"`
		OldText    *string `json:"old_text"`
		NewText    *string `json:"new_text"`
		ReplaceAll *bool   `json:"replace_all"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return editParams{}, fmt.Errorf("parse edit parameters: %v", err)
	}
	if input.Path == nil || *input.Path == "" {
		return editParams{}, errors.New("edit parameter path is required")
	}
	if input.OldText == nil || *input.OldText == "" {
		return editParams{}, errors.New("edit parameter old_text is required")
	}
	if input.NewText == nil {
		return editParams{}, errors.New("edit parameter new_text is required")
	}
	out := editParams{Path: *input.Path, OldText: *input.OldText, NewText: *input.NewText}
	if input.ReplaceAll != nil {
		out.ReplaceAll = *input.ReplaceAll
	}

	return out, nil
}

func readExistingFile(path string) (string, error) {

	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read file %q: %v", path, err)
	}
	out := string(b)

	return out, nil
}

func editContent(before string, args editParams) (string, int, error) {

	count := strings.Count(before, args.OldText)
	if count == 0 {
		return "", 0, fmt.Errorf("old_text not found in %q", args.Path)
	}
	if !args.ReplaceAll && count > 1 {
		return "", 0, fmt.Errorf("old_text must be unique in %q (found %d matches)", args.Path, count)
	}
	if args.ReplaceAll {
		after := strings.ReplaceAll(before, args.OldText, args.NewText)

		return after, count, nil
	}
	after := strings.Replace(before, args.OldText, args.NewText, 1)

	return after, 1, nil
}

func editSummary(args editParams, count int) string {

	oldText := strings.ReplaceAll(args.OldText, "\n", "\\n")
	newText := strings.ReplaceAll(args.NewText, "\n", "\\n")
	out := fmt.Sprintf(
		"--- %s\n+++ %s\n@@ replaced %d occurrence(s) @@\n-%s\n+%s",
		args.Path, args.Path, count, oldText, newText,
	)

	return out
}
