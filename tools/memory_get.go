package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/agusx1211/miclaw/memory"
	"github.com/agusx1211/miclaw/model"
)

type memoryGetParams struct {
	ChunkID string
}

func MemoryGetTool(store *memory.Store) Tool {
	return tool{
		name: "memory_get",
		desc: "Get a memory chunk by chunk_id with neighboring context",
		params: JSONSchema{
			Type:     "object",
			Required: []string{"chunk_id"},
			Properties: map[string]JSONSchema{
				"chunk_id": {Type: "string", Desc: "Chunk ID in path:index format"},
			},
		},
		runFn: func(ctx context.Context, call model.ToolCallPart) (ToolResult, error) {
			return runMemoryGet(ctx, store, call)
		},
	}
}

func runMemoryGet(_ context.Context, store *memory.Store, call model.ToolCallPart) (ToolResult, error) {
	p, err := parseMemoryGetParams(call.Parameters)
	if err != nil {
		return ToolResult{Content: err.Error(), IsError: true}, nil
	}
	chunk, err := store.GetChunk(p.ChunkID)
	if err != nil {
		return ToolResult{Content: err.Error(), IsError: true}, nil
	}
	if chunk == nil {
		return ToolResult{Content: "chunk not found", IsError: true}, nil
	}
	prev, next, err := getAdjacentChunks(store, p.ChunkID)
	if err != nil {
		return ToolResult{Content: err.Error(), IsError: true}, nil
	}
	return ToolResult{Content: formatMemoryGetResult(prev, chunk, next)}, nil
}

func parseMemoryGetParams(raw json.RawMessage) (memoryGetParams, error) {
	var input struct {
		ChunkID *string `json:"chunk_id"`
	}
	if err := unmarshalObject(raw, &input); err != nil {
		return memoryGetParams{}, err
	}
	if input.ChunkID == nil || strings.TrimSpace(*input.ChunkID) == "" {
		return memoryGetParams{}, errors.New("chunk_id is required")
	}
	return memoryGetParams{ChunkID: strings.TrimSpace(*input.ChunkID)}, nil
}

func getAdjacentChunks(store *memory.Store, id string) (*memory.Chunk, *memory.Chunk, error) {
	path, index, ok := splitChunkID(id)
	if !ok {
		return nil, nil, nil
	}
	var prev *memory.Chunk
	if index > 0 {
		var err error
		prev, err = store.GetChunk(fmt.Sprintf("%s:%d", path, index-1))
		if err != nil {
			return nil, nil, err
		}
	}
	next, err := store.GetChunk(fmt.Sprintf("%s:%d", path, index+1))
	if err != nil {
		return nil, nil, err
	}
	return prev, next, nil
}

func splitChunkID(id string) (string, int, bool) {
	i := strings.LastIndex(id, ":")
	if i <= 0 || i == len(id)-1 {
		return "", 0, false
	}
	n, err := strconv.Atoi(id[i+1:])
	if err != nil {
		return "", 0, false
	}
	return id[:i], n, true
}

func formatMemoryGetResult(prev, chunk, next *memory.Chunk) string {
	parts := []string{}
	if prev != nil {
		parts = append(parts, "[previous]\n"+formatMemoryChunk(prev))
	}
	parts = append(parts, "[current]\n"+formatMemoryChunk(chunk))
	if next != nil {
		parts = append(parts, "[next]\n"+formatMemoryChunk(next))
	}
	return strings.Join(parts, "\n\n")
}

func formatMemoryChunk(c *memory.Chunk) string {
	return fmt.Sprintf("[%s:%d-%d]\n%s", c.Path, c.StartLine, c.EndLine, c.Text)
}
