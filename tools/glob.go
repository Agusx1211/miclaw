package tools

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/agusx1211/miclaw/model"
)

var errGlobResultLimit = errors.New("glob result limit reached")

type globParams struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
}

func globTool() Tool {

	return tool{
		name: "glob",
		desc: "Find file paths using glob pattern",
		params: JSONSchema{
			Type: "object",
			Properties: map[string]JSONSchema{
				"pattern": {Type: "string", Desc: "glob pattern to match"},
				"path":    {Type: "string", Desc: "directory to search"},
			},
			Required: []string{"pattern"},
		},
		runFn: runGlob,
	}
}

func runGlob(ctx context.Context, call model.ToolCallPart) (ToolResult, error) {

	params, err := parseGlobParams(call.Parameters)
	if err != nil {
		return ToolResult{Content: err.Error(), IsError: true}, nil
	}
	root := filepath.Clean(params.Path)

	if _, err := os.Stat(root); err != nil {
		return ToolResult{Content: err.Error(), IsError: true}, nil
	}
	paths := make([]string, 0)
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		relative := filepath.ToSlash(path)
		if root != "." {
			relative = strings.TrimPrefix(relative, filepath.ToSlash(root)+"/")
		}
		if matchPathPattern(params.Pattern, relative) {
			paths = append(paths, relative)
			if len(paths) >= 1000 {
				return errGlobResultLimit
			}
		}
		return nil
	})
	if errors.Is(walkErr, errGlobResultLimit) {
		walkErr = nil
	}
	if walkErr != nil {
		return ToolResult{Content: walkErr.Error(), IsError: true}, nil
	}
	sort.Strings(paths)
	return ToolResult{Content: strings.Join(paths, "\n"), IsError: false}, nil
}

func parseGlobParams(raw json.RawMessage) (globParams, error) {

	var params globParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return globParams{}, err
	}

	if params.Path == "" {
		params.Path = "."
	}
	return params, nil
}
