package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agusx1211/miclaw/memory"
	"github.com/agusx1211/miclaw/model"
)

func openMemoryToolsStore(t *testing.T) *memory.Store {
	t.Helper()
	s, err := memory.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func newMemoryEmbedClient(t *testing.T, vectors map[string][]float32) *memory.EmbedClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		data := make([]map[string]any, len(req.Input))
		for i, q := range req.Input {
			data[i] = map[string]any{"embedding": vectors[q]}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(srv.Close)
	return memory.NewEmbedClient(srv.URL, "", "test-model")
}

func putChunk(t *testing.T, s *memory.Store, id, path string, start, end int, text string, emb []float32) {
	t.Helper()
	if err := s.PutChunk(memory.Chunk{
		ID:        id,
		Path:      path,
		StartLine: start,
		EndLine:   end,
		Text:      text,
		Embedding: emb,
	}); err != nil {
		t.Fatal(err)
	}
}

func runMemoryTool(t *testing.T, tl Tool, params map[string]any) ToolResult {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	got, err := tl.Run(context.Background(), model.ToolCallPart{Name: tl.Name(), Parameters: raw})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestMemorySearchFindsRelevantChunks(t *testing.T) {
	s := openMemoryToolsStore(t)
	putChunk(t, s, "notes.md:0", "notes.md", 1, 4, "fox memory detail", []float32{1, 0, 0})
	putChunk(t, s, "notes.md:1", "notes.md", 5, 8, "database migration", []float32{0, 1, 0})

	tool := MemorySearchTool(s, newMemoryEmbedClient(t, map[string][]float32{"fox": {1, 0, 0}}))
	got := runMemoryTool(t, tool, map[string]any{"query": "fox"})
	if got.IsError {
		t.Fatalf("unexpected error: %s", got.Content)
	}
	if !strings.Contains(got.Content, "[notes.md:1-4]") || !strings.Contains(got.Content, "fox memory detail") {
		t.Fatalf("missing relevant chunk: %q", got.Content)
	}
	if strings.Contains(got.Content, "database migration") {
		t.Fatalf("unexpected irrelevant chunk: %q", got.Content)
	}
}

func TestMemorySearchHybridScoring(t *testing.T) {
	s := openMemoryToolsStore(t)
	putChunk(t, s, "a.md:0", "a.md", 1, 1, "no keyword here", []float32{1, 0})
	putChunk(t, s, "b.md:0", "b.md", 1, 1, "fox keyword", []float32{0.6, 0.8})

	tool := MemorySearchTool(s, newMemoryEmbedClient(t, map[string][]float32{"fox": {1, 0}}))
	got := runMemoryTool(t, tool, map[string]any{"query": "fox", "limit": 2, "min_score": 0.0})
	if got.IsError {
		t.Fatalf("unexpected error: %s", got.Content)
	}
	lines := strings.Split(strings.TrimSpace(got.Content), "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "[b.md:1-1]") {
		t.Fatalf("expected FTS+vector chunk first, got %q", got.Content)
	}
	if !strings.Contains(got.Content, "[a.md:1-1]") {
		t.Fatalf("expected vector-only chunk to remain, got %q", got.Content)
	}
}

func TestMemorySearchMinScoreFilters(t *testing.T) {
	s := openMemoryToolsStore(t)
	putChunk(t, s, "a.md:0", "a.md", 1, 1, "unrelated text", []float32{1, 0})

	tool := MemorySearchTool(s, newMemoryEmbedClient(t, map[string][]float32{"query": {1, 0}}))
	got := runMemoryTool(t, tool, map[string]any{"query": "query", "min_score": 0.9})
	if got.IsError {
		t.Fatalf("unexpected error: %s", got.Content)
	}
	if got.Content != "" {
		t.Fatalf("expected filtered output, got %q", got.Content)
	}
}

func TestMemorySearchLimitRespected(t *testing.T) {
	s := openMemoryToolsStore(t)
	for i := range 5 {
		id := "notes.md:" + string(rune('0'+i))
		putChunk(t, s, id, "notes.md", i+1, i+1, "alpha token", []float32{1, 0})
	}

	tool := MemorySearchTool(s, newMemoryEmbedClient(t, map[string][]float32{"alpha": {1, 0}}))
	got := runMemoryTool(t, tool, map[string]any{"query": "alpha", "limit": 2, "min_score": 0.0})
	if got.IsError {
		t.Fatalf("unexpected error: %s", got.Content)
	}
	if strings.Count(got.Content, "(score:") != 2 {
		t.Fatalf("expected 2 results max, got %q", got.Content)
	}
}

func TestMemoryGetValidChunk(t *testing.T) {
	s := openMemoryToolsStore(t)
	putChunk(t, s, "path.md:0", "path.md", 1, 3, "core snippet", []float32{1, 0})

	got := runMemoryTool(t, MemoryGetTool(s), map[string]any{"chunk_id": "path.md:0"})
	if got.IsError {
		t.Fatalf("unexpected error: %s", got.Content)
	}
	if !strings.Contains(got.Content, "[path.md:1-3]") || !strings.Contains(got.Content, "core snippet") {
		t.Fatalf("missing chunk output: %q", got.Content)
	}
}

func TestMemoryGetInvalidChunk(t *testing.T) {
	s := openMemoryToolsStore(t)
	got := runMemoryTool(t, MemoryGetTool(s), map[string]any{"chunk_id": "missing.md:0"})
	if !got.IsError {
		t.Fatal("expected error for missing chunk")
	}
	if !strings.Contains(got.Content, "chunk not found") {
		t.Fatalf("unexpected message: %q", got.Content)
	}
}

func TestMemoryGetSurroundingContext(t *testing.T) {
	s := openMemoryToolsStore(t)
	putChunk(t, s, "book.md:0", "book.md", 1, 1, "prev text", []float32{1, 0})
	putChunk(t, s, "book.md:1", "book.md", 2, 2, "current text", []float32{1, 0})
	putChunk(t, s, "book.md:2", "book.md", 3, 3, "next text", []float32{1, 0})

	got := runMemoryTool(t, MemoryGetTool(s), map[string]any{"chunk_id": "book.md:1"})
	if got.IsError {
		t.Fatalf("unexpected error: %s", got.Content)
	}
	iPrev := strings.Index(got.Content, "prev text")
	iCur := strings.Index(got.Content, "current text")
	iNext := strings.Index(got.Content, "next text")
	if iPrev < 0 || iCur < 0 || iNext < 0 || !(iPrev < iCur && iCur < iNext) {
		t.Fatalf("surrounding context missing or out of order: %q", got.Content)
	}
}
