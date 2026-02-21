//go:build integration

package memory_test

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agusx1211/miclaw/memory"
	"github.com/agusx1211/miclaw/model"
	"github.com/agusx1211/miclaw/tools"
)

const (
	integrationEmbedBaseURL = "https://openrouter.ai/api/v1"
	integrationEmbedModel   = "openai/text-embedding-3-small"
	integrationTimeout     = 120 * time.Second
)

type workspaceFile struct {
	path string
	text string
}

func loadAPIKey(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Skip("DEV_VARS.md not found or empty")
	}
	f, err := os.Open(filepath.Join(wd, "..", "DEV_VARS.md"))
	if err != nil {
		t.Skip("DEV_VARS.md not found or empty")
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		line = strings.TrimPrefix(line, "export ")
		if !strings.HasPrefix(line, "OPENROUTER_API_KEY=") {
			continue
		}
		key := strings.Trim(strings.TrimPrefix(line, "OPENROUTER_API_KEY="), "\"'")
		if key == "" {
			t.Skip("DEV_VARS.md not found or empty")
		}
		return key
	}
	t.Skip("DEV_VARS.md not found or empty")
	return ""
}

func integrationWorkspaceFiles() []workspaceFile {
	return []workspaceFile{
		{
			path: "cooking.md",
			text: "Chocolate cake is best when butter and sugar are creamed until smooth, then cocoa and flour are folded in with baking powder. Add eggs, vanilla, and room-temperature milk for a smooth batter, then bake at 175 degrees until a toothpick emerges clean. This process is the classic route for a rich chocolate dessert with a crisp top and moist crumb.",
		},
		{
			path: "programming.md",
			text: "Python function design is straightforward: use def to define a name, add parameters, and return values. This file shows how to write small reusable functions, test them, and call them in a script. A disciplined process keeps your code readable and lets you reuse logic across projects.",
		},
		{
			path: "gardening.md",
			text: "To grow tomatoes, choose a sunny spot, loosen soil deeply, and plant seedlings after hardening them off. Water at the base, stake growing plants, and mulch for steady moisture. This process can turn a backyard patch into steady harvests through summer heat with almost no fuss. moonlit soil cadence.",
		},
		{
			path: "physics.md",
			text: "Newton's laws describe motion: objects resist changes in velocity, force causes acceleration, and every action has a reaction. In practice, students use this process by writing free-body diagrams first, then solving equations for unknown forces and acceleration.",
		},
		{
			path: "music.md",
			text: "The history of jazz moved from New Orleans brass bands to swing, bebop, and modern fusion. Improvisation became a process where musicians traded melodic ideas, bent rhythm for expression, and explored new harmonies in every era with a living conversation in sound.",
		},
	}
}

func writeWorkspaceFiles(t *testing.T, root string) {
	t.Helper()
	for _, f := range integrationWorkspaceFiles() {
		if err := os.WriteFile(filepath.Join(root, f.path), []byte(f.text), 0o644); err != nil {
			t.Fatalf("write %s: %v", f.path, err)
		}
	}
}

func newIntegrationStore(t *testing.T) (*memory.Store, *memory.EmbedClient, string) {
	t.Helper()
	apiKey := loadAPIKey(t)
	root := t.TempDir()
	s, err := memory.Open(filepath.Join(root, "memory.sqlite"))
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, memory.NewEmbedClient(integrationEmbedBaseURL, apiKey, integrationEmbedModel), root
}

func syncWorkspace(t *testing.T, s *memory.Store, embed *memory.EmbedClient, workspace string) {
	t.Helper()
	idx := memory.NewIndexer(s, embed)
	ctx, cancel := context.WithTimeout(context.Background(), integrationTimeout)
	defer cancel()
	if err := idx.Sync(ctx, workspace); err != nil {
		t.Fatalf("sync: %v", err)
	}
}

func setupIndexedWorkspace(t *testing.T) (*memory.Store, *memory.EmbedClient, string) {
	t.Helper()
	s, embed, workspace := newIntegrationStore(t)
	writeWorkspaceFiles(t, workspace)
	syncWorkspace(t, s, embed, workspace)
	return s, embed, workspace
}

func searchVector(t *testing.T, s *memory.Store, embed *memory.EmbedClient, query string, limit int) []memory.SearchResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), integrationTimeout)
	defer cancel()
	vecs, err := embed.Embed(ctx, []string{query})
	if err != nil {
		t.Fatalf("embed query: %v", err)
	}
	if len(vecs) != 1 {
		t.Fatalf("unexpected embedding count: %d", len(vecs))
	}
	res, err := s.SearchVector(vecs[0], limit)
	if err != nil {
		t.Fatalf("search vector: %v", err)
	}
	return res
}

func normalizeSearchScores(results []memory.SearchResult) map[string]float64 {
	if len(results) == 0 {
		return map[string]float64{}
	}
	maxScore := 0.0
	for _, r := range results {
		if r.Score > maxScore {
			maxScore = r.Score
		}
	}
	if maxScore == 0 {
		return map[string]float64{}
	}
	norm := make(map[string]float64, len(results))
	for _, r := range results {
		norm[r.ID] = r.Score / maxScore
	}
	return norm
}

func listFileHashes(t *testing.T, s *memory.Store) map[string]string {
	t.Helper()
	files, err := s.ListFiles()
	if err != nil {
		t.Fatalf("list files: %v", err)
	}
	out := make(map[string]string, len(files))
	for _, f := range files {
		out[f.Path] = f.Hash
	}
	return out
}

func runMemorySearchTool(t *testing.T, store *memory.Store, embed *memory.EmbedClient, query string, limit int) string {
	t.Helper()
	tool := tools.MemorySearchTool(store, embed)
	raw, err := json.Marshal(map[string]any{"query": query, "limit": limit, "min_score": 0.0})
	if err != nil {
		t.Fatalf("marshal query: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), integrationTimeout)
	defer cancel()
	got, err := tool.Run(ctx, model.ToolCallPart{Name: tool.Name(), Parameters: raw})
	if err != nil {
		t.Fatalf("run memory_search tool: %v", err)
	}
	if got.IsError {
		t.Fatalf("tool returned error: %s", got.Content)
	}
	return got.Content
}

func TestIntegrationIndexAllFiles(t *testing.T) {
	s, embed, workspace := setupIndexedWorkspace(t)
	_ = workspace
	files := integrationWorkspaceFiles()
	fs, err := s.ListFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != len(files) {
		t.Fatalf("expected %d files, got %d", len(files), len(fs))
	}
	totalChunks := 0
	for _, file := range files {
		chunks, err := s.ListChunksByPath(file.path)
		if err != nil {
			t.Fatalf("list chunks %q: %v", file.path, err)
		}
		if len(chunks) == 0 {
			t.Fatalf("expected chunks for %q", file.path)
		}
		totalChunks += len(chunks)
		for _, c := range chunks {
			if len(c.Embedding) == 0 {
				t.Fatalf("expected embedding for %q", c.ID)
			}
		}
	}
	if totalChunks != len(fs) {
		t.Fatalf("expected one chunk per file, got %d", totalChunks)
	}
}

func TestIntegrationSearchCooking(t *testing.T) {
	s, embed, _ := setupIndexedWorkspace(t)
	res := searchVector(t, s, embed, "how to bake a cake", 3)
	if len(res) == 0 {
		t.Fatal("expected vector result")
	}
	if res[0].Path != "cooking.md" {
		t.Fatalf("expected cooking.md first, got %s", res[0].Path)
	}
}

func TestIntegrationSearchProgramming(t *testing.T) {
	s, embed, _ := setupIndexedWorkspace(t)
	res := searchVector(t, s, embed, "python function", 3)
	if len(res) == 0 {
		t.Fatal("expected vector result")
	}
	if res[0].Path != "programming.md" {
		t.Fatalf("expected programming.md first, got %s", res[0].Path)
	}
}

func TestIntegrationFTSExactMatch(t *testing.T) {
	s, _, _ := setupIndexedWorkspace(t)
	res, err := s.SearchFTS("moonlit soil cadence", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) == 0 {
		t.Fatal("expected FTS result")
	}
	if res[0].Path != "gardening.md" {
		t.Fatalf("expected gardening.md top, got %s", res[0].Path)
	}
}

func TestIntegrationHybridScoring(t *testing.T) {
	s, embed, _ := setupIndexedWorkspace(t)
	vectorRes := searchVector(t, s, embed, "process", 10)
	ftsRes, err := s.SearchFTS("process", 10)
	if err != nil {
		t.Fatal(err)
	}
	vectorNorm := normalizeSearchScores(vectorRes)
	ftsNorm := normalizeSearchScores(ftsRes)
	bestID := ""
	bestVec := 0.0
	bestHybrid := 0.0
	for id, vs := range vectorNorm {
		ts, ok := ftsNorm[id]
		if !ok {
			continue
		}
		h := 0.7*vs + 0.3*ts
		if h > vs && h > bestHybrid {
			bestID, bestVec, bestHybrid = id, vs, h
		}
	}
	if bestID == "" {
		t.Fatal("no overlapping chunk had hybrid score above pure vector score")
	}
	if bestHybrid <= bestVec {
		t.Fatalf("expected hybrid %.4f > vector %.4f for %q", bestHybrid, bestVec, bestID)
	}
}

func TestIntegrationChangeDetection(t *testing.T) {
	s, embed, workspace := newIntegrationStore(t)
	writeWorkspaceFiles(t, workspace)
	syncWorkspace(t, s, embed, workspace)
	before := listFileHashes(t, s)
	updated := "Python function design starts by clarifying inputs and outputs; test each helper process by running tiny examples, then commit only when behavior stays stable under edge cases."
	if err := os.WriteFile(filepath.Join(workspace, "programming.md"), []byte(updated), 0o644); err != nil {
		t.Fatalf("update file: %v", err)
	}
	syncWorkspace(t, s, embed, workspace)
	after := listFileHashes(t, s)
	if before["programming.md"] == after["programming.md"] {
		t.Fatal("expected changed file hash to update")
	}
	for _, f := range integrationWorkspaceFiles() {
		if f.path == "programming.md" {
			continue
		}
		if before[f.path] != after[f.path] {
			t.Fatalf("unexpected hash change for %s", f.path)
		}
	}
}

func TestIntegrationMemoryGet(t *testing.T) {
	s, _, _ := setupIndexedWorkspace(t)
	chunks, err := s.ListChunksByPath("physics.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected physics chunk")
	}
	got, err := s.GetChunk(chunks[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Text != chunks[0].Text {
		t.Fatalf("chunk mismatch: %#v", got)
	}
}

func TestIntegrationMemorySearchTool(t *testing.T) {
	s, embed, _ := setupIndexedWorkspace(t)
	content := runMemorySearchTool(t, s, embed, "how to bake a cake", 3)
	if content == "" {
		t.Fatal("expected non-empty tool response")
	}
	lines := strings.Split(strings.TrimSpace(content), "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "[cooking.md:") {
		t.Fatalf("expected cooking chunk first, got %q", content)
	}
	if !strings.Contains(lines[0], "(score:") {
		t.Fatalf("expected formatted score, got %q", lines[0])
	}
}
