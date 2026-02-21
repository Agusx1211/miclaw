package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/agusx1211/miclaw/model"
)

type lsParams struct {
	Path       string `json:"path"`
	Depth      int    `json:"depth"`
	ShowHidden bool   `json:"show_hidden"`
}

func lsTool() Tool {

	return tool{
		name: "ls",
		desc: "List directory entries with type and size",
		params: JSONSchema{
			Type: "object",
			Properties: map[string]JSONSchema{
				"path":        {Type: "string", Desc: "directory to list"},
				"depth":       {Type: "integer", Desc: "max directory depth"},
				"show_hidden": {Type: "boolean", Desc: "include files that begin with dot"},
			},
			Required: []string{"path"},
		},
		runFn: runLS,
	}
}

func runLS(ctx context.Context, call model.ToolCallPart) (ToolResult, error) {

	params, err := parseLSParams(call.Parameters)
	if err != nil {
		return ToolResult{Content: err.Error(), IsError: true}, nil
	}
	root := filepath.Clean(params.Path)

	var lines []string
	if params.Depth == 1 {
		entries, err := os.ReadDir(root)
		if err != nil {
			return ToolResult{Content: err.Error(), IsError: true}, nil
		}
		for _, entry := range entries {
			if !params.ShowHidden && strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				return ToolResult{Content: err.Error(), IsError: true}, nil
			}
			lines = append(lines, formatEntry(entry.Name(), info))
		}
		sort.Strings(lines)
		return ToolResult{Content: strings.Join(lines, "\n"), IsError: false}, nil
	}
	if err := listTreeEntries(root, params.Depth, params.ShowHidden, "", 1, &lines); err != nil {
		return ToolResult{Content: err.Error(), IsError: true}, nil
	}
	return ToolResult{Content: strings.Join(lines, "\n"), IsError: false}, nil
}

func parseLSParams(raw json.RawMessage) (lsParams, error) {

	var params lsParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return lsParams{}, err
	}

	if params.Depth == 0 {
		params.Depth = 1
	}
	if params.Depth < 1 || params.Depth > 5 {
		return lsParams{}, fmt.Errorf("ls depth must be between 1 and 5")
	}
	return params, nil
}

func listTreeEntries(base string, maxDepth int, showHidden bool, prefix string, level int, out *[]string) error {

	entries, err := os.ReadDir(base)
	if err != nil {
		return err
	}

	filtered := make([]os.DirEntry, 0, len(entries))
	for _, entry := range entries {
		if !showHidden && strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		filtered = append(filtered, entry)
	}
	for idx, entry := range filtered {
		leaf := idx == len(filtered)-1
		info, err := entry.Info()
		if err != nil {
			return err
		}
		branch := "├── "
		nextPrefix := prefix + "│   "
		if leaf {
			branch = "└── "
			nextPrefix = prefix + "    "
		}
		line := fmt.Sprintf("%s%s (%s, %d)", prefix+branch, entry.Name(), entryType(info), info.Size())
		*out = append(*out, line)
		if entry.IsDir() && level < maxDepth {
			if err := listTreeEntries(filepath.Join(base, entry.Name()), maxDepth, showHidden, nextPrefix, level+1, out); err != nil {
				return err
			}
		}
	}
	return nil
}

func formatEntry(name string, info os.FileInfo) string {

	return fmt.Sprintf("%s (%s, %d)", name, entryType(info), info.Size())
}

func entryType(info os.FileInfo) string {

	if info.Mode()&os.ModeSymlink != 0 {
		return "symlink"
	}
	if info.IsDir() {
		return "dir"
	}
	return "file"
}
