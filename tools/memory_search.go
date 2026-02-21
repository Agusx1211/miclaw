package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/agusx1211/miclaw/memory"
	"github.com/agusx1211/miclaw/model"
)

const (
	memorySearchDefaultLimit    = 6
	memorySearchDefaultMinScore = 0.35
	memorySearchVectorWeight    = 0.7
	memorySearchFTSWeight       = 0.3
)

type memorySearchParams struct {
	Query    string
	Limit    int
	MinScore float64
}

type memoryScoredChunk struct {
	chunk memory.Chunk
	score float64
}

func MemorySearchTool(store *memory.Store, embedClient *memory.EmbedClient) Tool {
	return tool{
		name: "memory_search",
		desc: "Search memory chunks with hybrid vector and full-text scoring",
		params: JSONSchema{
			Type:     "object",
			Required: []string{"query"},
			Properties: map[string]JSONSchema{
				"query":     {Type: "string", Desc: "Search query"},
				"limit":     {Type: "integer", Desc: "Maximum number of results (default: 6)"},
				"min_score": {Type: "number", Desc: "Minimum score threshold (default: 0.35)"},
			},
		},
		runFn: func(ctx context.Context, call model.ToolCallPart) (ToolResult, error) {
			return runMemorySearch(ctx, store, embedClient, call)
		},
	}
}

func runMemorySearch(
	ctx context.Context,
	store *memory.Store,
	embedClient *memory.EmbedClient,
	call model.ToolCallPart,
) (ToolResult, error) {
	p, err := parseMemorySearchParams(call.Parameters)
	if err != nil {
		return ToolResult{Content: err.Error(), IsError: true}, nil
	}
	vecs, err := embedClient.Embed(ctx, []string{p.Query})
	if err != nil {
		return ToolResult{Content: err.Error(), IsError: true}, nil
	}
	if len(vecs) != 1 {
		return ToolResult{Content: "embedding count mismatch", IsError: true}, nil
	}
	vectorResults, err := store.SearchVector(vecs[0], p.Limit*2)
	if err != nil {
		return ToolResult{Content: err.Error(), IsError: true}, nil
	}
	ftsResults, err := store.SearchFTS(p.Query, p.Limit*2)
	if err != nil {
		return ToolResult{Content: err.Error(), IsError: true}, nil
	}
	scored := mergeMemorySearchResults(vectorResults, ftsResults, p.MinScore, p.Limit)
	return ToolResult{Content: formatMemorySearchResult(scored)}, nil
}

func parseMemorySearchParams(raw json.RawMessage) (memorySearchParams, error) {
	var input struct {
		Query    *string  `json:"query"`
		Limit    *int     `json:"limit"`
		MinScore *float64 `json:"min_score"`
	}
	if err := unmarshalObject(raw, &input); err != nil {
		return memorySearchParams{}, err
	}
	if input.Query == nil || strings.TrimSpace(*input.Query) == "" {
		return memorySearchParams{}, errors.New("query is required")
	}
	out := memorySearchParams{
		Query:    strings.TrimSpace(*input.Query),
		Limit:    memorySearchDefaultLimit,
		MinScore: memorySearchDefaultMinScore,
	}
	if input.Limit != nil {
		out.Limit = *input.Limit
	}
	if input.MinScore != nil {
		out.MinScore = *input.MinScore
	}
	if out.Limit <= 0 {
		out.Limit = memorySearchDefaultLimit
	}
	return out, nil
}

func mergeMemorySearchResults(
	vectorResults []memory.SearchResult,
	ftsResults []memory.SearchResult,
	minScore float64,
	limit int,
) []memoryScoredChunk {
	vectorScores := normalizeMemoryScores(vectorResults)
	ftsScores := normalizeMemoryScores(ftsResults)
	byID := map[string]memory.Chunk{}
	for _, r := range vectorResults {
		byID[r.ID] = r.Chunk
	}
	for _, r := range ftsResults {
		byID[r.ID] = r.Chunk
	}
	out := make([]memoryScoredChunk, 0, len(byID))
	for id, chunk := range byID {
		score := memorySearchVectorWeight*vectorScores[id] + memorySearchFTSWeight*ftsScores[id]
		if score >= minScore {
			out = append(out, memoryScoredChunk{chunk: chunk, score: score})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].score == out[j].score {
			return out[i].chunk.ID < out[j].chunk.ID
		}
		return out[i].score > out[j].score
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func normalizeMemoryScores(results []memory.SearchResult) map[string]float64 {
	scores := map[string]float64{}
	maxScore := 0.0
	for _, r := range results {
		if r.Score > maxScore {
			maxScore = r.Score
		}
	}
	if maxScore == 0 {
		return scores
	}
	for _, r := range results {
		scores[r.ID] = r.Score / maxScore
	}
	return scores
}

func formatMemorySearchResult(scored []memoryScoredChunk) string {
	var b strings.Builder
	for _, r := range scored {
		b.WriteString(fmt.Sprintf(
			"[%s:%d-%d] (score: %.2f)\n%s\n",
			r.chunk.Path,
			r.chunk.StartLine,
			r.chunk.EndLine,
			r.score,
			r.chunk.Text,
		))
	}
	return b.String()
}
