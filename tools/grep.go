package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/agusx1211/miclaw/model"
)

var errGrepResultLimit = errors.New("grep result limit reached")

type grepParams struct {
	Pattern      string `json:"pattern"`
	Path         string `json:"path"`
	Include      string `json:"include"`
	Exclude      string `json:"exclude"`
	ContextLines int    `json:"context_lines"`
	MaxResults   int    `json:"max_results"`
}

func grepTool() Tool {

	return tool{
		name: "grep",
		desc: "Search file contents by regex with optional context and .gitignore support",
		params: JSONSchema{
			Type: "object",
			Properties: map[string]JSONSchema{
				"pattern": {Type: "string", Desc: "regular expression pattern to search"},
				"path":    {Type: "string", Desc: "directory to search"},
				"include": {Type: "string", Desc: "glob for files to include"},
				"exclude": {Type: "string", Desc: "glob for files to exclude"},
				"context_lines": {
					Type: "integer",
					Desc: "lines of context before and after each match",
				},
				"max_results": {
					Type: "integer",
					Desc: "maximum number of output lines",
				},
			},
			Required: []string{"pattern"},
		},
		runFn: runGrep,
	}
}

func runGrep(ctx context.Context, call model.ToolCallPart) (ToolResult, error) {

	params, err := parseGrepParams(call.Parameters)
	if err != nil {
		return ToolResult{Content: err.Error(), IsError: true}, nil
	}
	root := filepath.Clean(params.Path)

	if _, err := os.Stat(root); err != nil {
		return ToolResult{Content: err.Error(), IsError: true}, nil
	}
	re, err := regexp.Compile(params.Pattern)
	if err != nil {
		return ToolResult{Content: err.Error(), IsError: true}, nil
	}
	ignorePatterns, err := loadGitignorePatterns(root)
	if err != nil {
		return ToolResult{Content: err.Error(), IsError: true}, nil
	}
	var out []string
	var got int
	walkErr := filepath.WalkDir(root, grepWalkFunc(params, root, re, ignorePatterns, &out, &got))
	if errors.Is(walkErr, errGrepResultLimit) {
		walkErr = nil
	}
	if walkErr != nil {
		return ToolResult{Content: walkErr.Error(), IsError: true}, nil
	}
	return ToolResult{Content: strings.Join(out, "\n"), IsError: false}, nil
}

func parseGrepParams(raw json.RawMessage) (grepParams, error) {

	var params grepParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return grepParams{}, err
	}

	if params.Path == "" {
		params.Path = "."
	}
	if params.ContextLines < 0 {
		return grepParams{}, fmt.Errorf("context_lines must be non-negative")
	}
	if params.MaxResults <= 0 {
		params.MaxResults = 100
	}
	return params, nil
}

func grepWalkFunc(params grepParams, root string, re *regexp.Regexp, ignorePatterns []string, out *[]string, got *int) fs.WalkDirFunc {
	return func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative := filepath.ToSlash(path)
		if root != "." {
			relative = strings.TrimPrefix(relative, filepath.ToSlash(root)+"/")
		}
		if d.IsDir() {
			if relative != "" && isIgnored(relative, ignorePatterns) {
				return filepath.SkipDir
			}
			return nil
		}
		if isIgnored(relative, ignorePatterns) {
			return nil
		}
		if params.Include != "" && !matchPathPattern(params.Include, relative) {
			return nil
		}
		if params.Exclude != "" && matchPathPattern(params.Exclude, relative) {
			return nil
		}
		matches, err := searchMatchesInFile(relative, path, re, params.ContextLines)
		if err != nil {
			return err
		}
		for _, line := range matches {
			if *got >= params.MaxResults {
				return errGrepResultLimit
			}
			*out = append(*out, line)
			*got++
		}
		return nil
	}
}

func searchMatchesInFile(relPath, absPath string, re *regexp.Regexp, contextLines int) ([]string, error) {

	content, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}
	if isBinaryContent(content) {
		return nil, nil
	}
	lines := strings.Split(string(content), "\n")
	results := make([]string, 0)
	for i, line := range lines {
		if !re.MatchString(line) {
			continue
		}
		start := i - contextLines
		if start < 0 {
			start = 0
		}
		end := i + contextLines
		if end >= len(lines) {
			end = len(lines) - 1
		}
		for j := start; j <= end; j++ {
			results = append(results, fmt.Sprintf("%s:%d:%s", relPath, j+1, lines[j]))
		}
	}
	return results, nil
}

func loadGitignorePatterns(root string) ([]string, error) {

	ignorePath := filepath.Join(root, ".gitignore")
	raw, err := os.ReadFile(ignorePath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(raw), "\n")
	patterns := make([]string, 0, len(lines))
	for _, line := range lines {
		pattern := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if pattern == "" || strings.HasPrefix(pattern, "#") {
			continue
		}
		if strings.HasPrefix(pattern, "!") {
			continue
		}
		pattern = strings.TrimPrefix(strings.TrimSuffix(filepath.ToSlash(pattern), "/"), "./")
		patterns = append(patterns, pattern)
	}
	return patterns, nil
}

func isIgnored(path string, patterns []string) bool {

	if len(patterns) == 0 {
		return false
	}
	for _, pattern := range patterns {

		if strings.Contains(pattern, "/") {
			if matchPathPattern(pattern, path) {
				return true
			}
			continue
		}
		if matchPathPattern(pattern, filepath.Base(path)) {
			return true
		}
	}
	return false
}

func isBinaryContent(raw []byte) bool {

	for _, b := range raw {
		if b == 0 {
			return true
		}
	}
	return false
}
