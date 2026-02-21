package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/agusx1211/miclaw/model"
)

const (
	readDefaultLimit   = 1000
	readMaxOutputBytes = 512 * 1024
)

const readTruncationMessage = "[read output truncated at 512KB]"

type readParams struct {
	Path   string
	Offset int
	Limit  int
}

type rawReadParams struct {
	Path   string `json:"path"`
	Offset *int   `json:"offset"`
	Limit  *int   `json:"limit"`
}

func ReadTool() Tool {
	name := "read"
	desc := "Read file contents with line numbers"
	must(name != "", "read tool name missing")
	must(desc != "", "read tool description missing")
	return tool{
		name: name,
		desc: desc,
		params: JSONSchema{
			Type:     "object",
			Required: []string{"path"},
			Properties: map[string]JSONSchema{
				"path": {
					Type: "string",
					Desc: "Path to the file to read",
				},
				"offset": {
					Type: "integer",
					Desc: "Line number to start reading from",
				},
				"limit": {
					Type: "integer",
					Desc: "Maximum number of lines to return",
				},
			},
		},
		runFn: func(ctx context.Context, call model.ToolCallPart) (ToolResult, error) {
			must(ctx != nil, "context is required")
			params, err := parseReadParams(call.Parameters)
			if err != nil {
				return ToolResult{IsError: true, Content: err.Error()}, nil
			}
			must(params.Path != "", "path is required")
			must(params.Limit >= 0, "limit must be non-negative")
			content, err := readFileContent(params.Path, params.Offset, params.Limit)
			if err != nil {
				return ToolResult{IsError: true, Content: err.Error()}, nil
			}
			return ToolResult{Content: content}, nil
		},
	}
}

func parseReadParams(raw json.RawMessage) (readParams, error) {
	must(len(raw) > 0, "read tool parameters missing")
	var params rawReadParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return readParams{}, fmt.Errorf("invalid read parameters: %w", err)
	}
	offset := 0
	if params.Offset != nil {
		offset = *params.Offset
	}
	limit := readDefaultLimit
	if params.Limit != nil {
		limit = *params.Limit
	}
	must(offset >= 0, "offset must be non-negative")
	must(limit >= 0, "limit must be non-negative")
	return readParams{Path: params.Path, Offset: offset, Limit: limit}, nil
}

func readFileContent(path string, offset, limit int) (string, error) {
	must(path != "", "path is required")
	must(offset >= 0, "offset must be non-negative")
	must(limit >= 0, "limit must be non-negative")
	if limit == 0 {
		return "", nil
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	output := &bytes.Buffer{}
	readLines := 0
	for lineNo := 0; ; lineNo++ {
		line, readErr := reader.ReadString('\n')
		if len(line) == 0 && errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return "", readErr
		}
		if strings.IndexByte(line, 0) >= 0 {
			return "", fmt.Errorf("binary file")
		}
		if lineNo < offset {
			if errors.Is(readErr, io.EOF) {
				break
			}
			continue
		}
		if readLines >= limit {
			break
		}
		if !appendReadLine(output, lineNo+1, strings.TrimSuffix(line, "\n")) {
			return output.String(), nil
		}
		readLines++
		if errors.Is(readErr, io.EOF) {
			break
		}
	}
	return output.String(), nil
}

func appendReadLine(output *bytes.Buffer, lineNo int, text string) bool {
	must(output != nil, "output buffer required")
	must(lineNo > 0, "line number must be positive")
	line := fmt.Sprintf("%6d\t%s\n", lineNo, text)
	if output.Len()+len(line) <= readMaxOutputBytes {
		output.WriteString(line)
		return true
	}
	if len(readTruncationMessage) >= readMaxOutputBytes {
		return false
	}
	room := readMaxOutputBytes - len(readTruncationMessage)
	if output.Len() > room {
		output.Truncate(room)
	}
	if output.Len() > 0 {
		if output.Len()+1+len(readTruncationMessage) > readMaxOutputBytes {
			output.Truncate(readMaxOutputBytes - 1 - len(readTruncationMessage))
		}
		output.WriteByte('\n')
	}
	output.WriteString(readTruncationMessage)
	return false
}
