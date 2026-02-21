package memory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestChunkSingleParagraph(t *testing.T) {
	text := "small paragraph"
	chunks := ChunkText(text)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0] != text {
		t.Fatalf("unexpected chunk: %q", chunks[0])
	}
}

func TestChunkMultiParagraph(t *testing.T) {
	p1 := strings.Repeat("a", 900)
	p2 := strings.Repeat("b", 900)
	p3 := strings.Repeat("c", 900)
	chunks := ChunkText(p1 + "\n\n" + p2 + "\n\n" + p3)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	want := p1 + "\n\n" + p2
	if chunks[0] != want {
		t.Fatalf("first chunk should split on paragraph boundary")
	}
	if !strings.Contains(chunks[1], p3) {
		t.Fatalf("second chunk should include third paragraph")
	}
}

func TestChunkLongText(t *testing.T) {
	chunks := ChunkText(strings.Repeat("x", 4500))
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}
	if len(chunks[0]) != 2000 {
		t.Fatalf("expected first chunk length 2000, got %d", len(chunks[0]))
	}
}

func TestChunkEmpty(t *testing.T) {
	chunks := ChunkText("")
	if len(chunks) != 0 {
		t.Fatalf("expected empty chunks, got %d", len(chunks))
	}
}

func TestChunkOverlap(t *testing.T) {
	chunks := ChunkText(strings.Repeat("0123456789", 250))
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}
	a := chunks[0][len(chunks[0])-100:]
	b := chunks[1][:100]
	if a != b {
		t.Fatalf("expected 100 char overlap")
	}
}

func TestSyncNewFiles(t *testing.T) {
	s := openTestStore(t)
	srv, calls := newEmbedServer(t)
	defer srv.Close()

	idx := NewIndexer(s, NewEmbedClient(srv.URL, "", "test-model"))
	dir := t.TempDir()
	writeFile(t, dir, "src/main.go", "package main\nfunc main() {}")
	writeFile(t, dir, "docs/readme.md", "hello\n\nworld")
	if err := idx.Sync(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected 2 embed calls, got %d", calls.Load())
	}
	files, err := s.ListFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
	chunks, err := s.ListChunksByPath("src/main.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected chunks for src/main.go")
	}
}

func TestSyncUnchangedSkipped(t *testing.T) {
	s := openTestStore(t)
	srv, calls := newEmbedServer(t)
	defer srv.Close()

	idx := NewIndexer(s, NewEmbedClient(srv.URL, "", "test-model"))
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "first")
	if err := idx.Sync(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	if err := idx.Sync(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected unchanged file to skip embed, calls=%d", calls.Load())
	}
}

func TestSyncChangedReindexed(t *testing.T) {
	s := openTestStore(t)
	srv, calls := newEmbedServer(t)
	defer srv.Close()

	idx := NewIndexer(s, NewEmbedClient(srv.URL, "", "test-model"))
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", strings.Repeat("x", 4300))
	if err := idx.Sync(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("short"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := idx.Sync(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected 2 embed calls, got %d", calls.Load())
	}
	gone, err := s.GetChunk("a.txt:1")
	if err != nil {
		t.Fatal(err)
	}
	if gone != nil {
		t.Fatal("expected stale chunk to be removed")
	}
	chunks, err := s.ListChunksByPath("a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk after change, got %d", len(chunks))
	}
	if chunks[0].Text != "short" {
		t.Fatalf("unexpected chunk text %q", chunks[0].Text)
	}
}

func TestSyncDeletedRemoved(t *testing.T) {
	s := openTestStore(t)
	srv, _ := newEmbedServer(t)
	defer srv.Close()

	idx := NewIndexer(s, NewEmbedClient(srv.URL, "", "test-model"))
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "hello")
	if err := idx.Sync(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "a.txt")); err != nil {
		t.Fatal(err)
	}
	if err := idx.Sync(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	f, err := s.GetFile("a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if f != nil {
		t.Fatal("expected file record deleted")
	}
	chunks, err := s.ListChunksByPath("a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 0 {
		t.Fatalf("expected no chunks, got %d", len(chunks))
	}
}

func TestEmbedClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/embeddings" {
			t.Fatalf("expected /embeddings, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer key" {
			t.Fatalf("unexpected auth header: %q", r.Header.Get("Authorization"))
		}
		var req struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.Model != "m" {
			t.Fatalf("expected model m, got %q", req.Model)
		}
		if len(req.Input) != 2 || req.Input[0] != "a" || req.Input[1] != "b" {
			t.Fatalf("unexpected input: %#v", req.Input)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"embedding": []float32{1, 2}},
				{"embedding": []float32{3, 4}},
			},
		})
	}))
	defer srv.Close()

	c := NewEmbedClient(srv.URL, "key", "m")
	vecs, err := c.Embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 2 {
		t.Fatalf("expected 2 vectors, got %d", len(vecs))
	}
	if len(vecs[1]) != 2 || vecs[1][1] != 4 {
		t.Fatalf("unexpected vectors: %#v", vecs)
	}
}

func newEmbedServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	calls := &atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var req struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		data := make([]map[string]any, len(req.Input))
		for i, s := range req.Input {
			data[i] = map[string]any{"embedding": []float32{float32(i), float32(len(s))}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	return srv, calls
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
