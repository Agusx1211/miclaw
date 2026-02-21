package memory

import (
	"fmt"
	"math"
	"testing"
	"time"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir() + "/test.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestOpenCreatesSchema(t *testing.T) {
	s := openTestStore(t)
	if err := s.SetMeta("k", "v"); err != nil {
		t.Fatal(err)
	}
	v, err := s.GetMeta("k")
	if err != nil {
		t.Fatal(err)
	}
	if v != "v" {
		t.Fatalf("got %q, want %q", v, "v")
	}
}

func TestMetaUpsert(t *testing.T) {
	s := openTestStore(t)
	s.SetMeta("k", "a")
	s.SetMeta("k", "b")
	v, _ := s.GetMeta("k")
	if v != "b" {
		t.Fatalf("got %q, want %q", v, "b")
	}
}

func TestMetaGetMissing(t *testing.T) {
	s := openTestStore(t)
	v, err := s.GetMeta("nope")
	if err != nil {
		t.Fatal(err)
	}
	if v != "" {
		t.Fatalf("got %q, want empty", v)
	}
}

func TestPutFileAndGet(t *testing.T) {
	s := openTestStore(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	f := File{Path: "/a.md", Hash: "abc", Mtime: now, Size: 100}
	if err := s.PutFile(f); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetFile("/a.md")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected file, got nil")
	}
	if got.Hash != "abc" || got.Size != 100 {
		t.Fatalf("unexpected file: %+v", got)
	}
}

func TestGetFileMissing(t *testing.T) {
	s := openTestStore(t)
	f, err := s.GetFile("/nope")
	if err != nil {
		t.Fatal(err)
	}
	if f != nil {
		t.Fatalf("expected nil, got %+v", f)
	}
}

func TestPutFileUpsert(t *testing.T) {
	s := openTestStore(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	s.PutFile(File{Path: "/a.md", Hash: "old", Mtime: now, Size: 10})
	s.PutFile(File{Path: "/a.md", Hash: "new", Mtime: now, Size: 20})
	f, _ := s.GetFile("/a.md")
	if f.Hash != "new" || f.Size != 20 {
		t.Fatalf("upsert failed: %+v", f)
	}
}

func TestDeleteFile(t *testing.T) {
	s := openTestStore(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	s.PutFile(File{Path: "/a.md", Hash: "h", Mtime: now, Size: 1})
	if err := s.DeleteFile("/a.md"); err != nil {
		t.Fatal(err)
	}
	f, _ := s.GetFile("/a.md")
	if f != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestListFiles(t *testing.T) {
	s := openTestStore(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	s.PutFile(File{Path: "/b.md", Hash: "h1", Mtime: now, Size: 1})
	s.PutFile(File{Path: "/a.md", Hash: "h2", Mtime: now, Size: 2})
	files, err := s.ListFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
	if files[0].Path != "/a.md" {
		t.Fatalf("expected sorted by path, got %s first", files[0].Path)
	}
}

func TestPutChunkAndGet(t *testing.T) {
	s := openTestStore(t)
	emb := []float32{0.1, 0.2, 0.3}
	c := Chunk{
		ID:        "c1",
		Path:      "/a.md",
		StartLine: 1,
		EndLine:   10,
		Hash:      "ch",
		Text:      "hello world",
		Embedding: emb,
	}
	if err := s.PutChunk(c); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetChunk("c1")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected chunk, got nil")
	}
	if got.Text != "hello world" || got.StartLine != 1 || got.EndLine != 10 {
		t.Fatalf("unexpected chunk: %+v", got)
	}
	if len(got.Embedding) != 3 {
		t.Fatalf("expected 3-dim embedding, got %d", len(got.Embedding))
	}
	for i, v := range emb {
		if math.Abs(float64(got.Embedding[i]-v)) > 1e-6 {
			t.Fatalf("embedding[%d] = %f, want %f", i, got.Embedding[i], v)
		}
	}
}

func TestGetChunkMissing(t *testing.T) {
	s := openTestStore(t)
	c, err := s.GetChunk("nope")
	if err != nil {
		t.Fatal(err)
	}
	if c != nil {
		t.Fatalf("expected nil, got %+v", c)
	}
}

func TestDeleteChunksByPath(t *testing.T) {
	s := openTestStore(t)
	s.PutChunk(Chunk{ID: "c1", Path: "/a.md", Text: "one"})
	s.PutChunk(Chunk{ID: "c2", Path: "/a.md", Text: "two"})
	s.PutChunk(Chunk{ID: "c3", Path: "/b.md", Text: "three"})
	if err := s.DeleteChunksByPath("/a.md"); err != nil {
		t.Fatal(err)
	}
	c1, _ := s.GetChunk("c1")
	c3, _ := s.GetChunk("c3")
	if c1 != nil {
		t.Fatal("c1 should be deleted")
	}
	if c3 == nil {
		t.Fatal("c3 should still exist")
	}
}

func TestListChunksByPath(t *testing.T) {
	s := openTestStore(t)
	s.PutChunk(Chunk{ID: "c2", Path: "/a.md", StartLine: 11, Text: "second"})
	s.PutChunk(Chunk{ID: "c1", Path: "/a.md", StartLine: 1, Text: "first"})
	s.PutChunk(Chunk{ID: "c3", Path: "/b.md", StartLine: 1, Text: "other"})
	chunks, err := s.ListChunksByPath("/a.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if chunks[0].ID != "c1" {
		t.Fatalf("expected sorted by start_line, got %s first", chunks[0].ID)
	}
}

func TestSearchFTS(t *testing.T) {
	s := openTestStore(t)
	s.PutChunk(Chunk{ID: "c1", Path: "/a.md", StartLine: 1, EndLine: 5, Text: "the quick brown fox"})
	s.PutChunk(Chunk{ID: "c2", Path: "/a.md", StartLine: 6, EndLine: 10, Text: "lazy dog sleeps"})
	s.PutChunk(Chunk{ID: "c3", Path: "/b.md", StartLine: 1, EndLine: 3, Text: "fox jumps high"})

	results, err := s.SearchFTS("fox", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 FTS results, got %d", len(results))
	}
	ids := map[string]bool{}
	for _, r := range results {
		ids[r.ID] = true
		if r.Score <= 0 {
			t.Fatalf("expected positive score, got %f", r.Score)
		}
	}
	if !ids["c1"] || !ids["c3"] {
		t.Fatalf("expected c1 and c3 in results, got %v", ids)
	}
}

func TestSearchFTSNoResults(t *testing.T) {
	s := openTestStore(t)
	s.PutChunk(Chunk{ID: "c1", Path: "/a.md", Text: "hello world"})
	results, err := s.SearchFTS("zzzznotfound", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestSearchFTSLimit(t *testing.T) {
	s := openTestStore(t)
	for i := range 5 {
		s.PutChunk(Chunk{
			ID:   fmt.Sprintf("c%d", i),
			Path: "/a.md",
			Text: "repeated keyword repeated",
		})
	}
	results, err := s.SearchFTS("repeated", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
}

func TestPutChunkUpsert(t *testing.T) {
	s := openTestStore(t)
	s.PutChunk(Chunk{ID: "c1", Path: "/a.md", Text: "old text"})
	s.PutChunk(Chunk{ID: "c1", Path: "/a.md", Text: "new text"})
	c, _ := s.GetChunk("c1")
	if c.Text != "new text" {
		t.Fatalf("upsert failed: got %q", c.Text)
	}
	// FTS should also be updated
	results, _ := s.SearchFTS("new", 10)
	if len(results) != 1 {
		t.Fatalf("expected 1 FTS result for 'new', got %d", len(results))
	}
	results, _ = s.SearchFTS("old", 10)
	if len(results) != 0 {
		t.Fatalf("expected 0 FTS results for 'old', got %d", len(results))
	}
}

func TestDeleteChunksByPathRemovesFTS(t *testing.T) {
	s := openTestStore(t)
	s.PutChunk(Chunk{ID: "c1", Path: "/a.md", Text: "searchable content"})
	s.DeleteChunksByPath("/a.md")
	results, _ := s.SearchFTS("searchable", 10)
	if len(results) != 0 {
		t.Fatalf("expected 0 FTS results after delete, got %d", len(results))
	}
}
