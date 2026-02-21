package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	chunkSize    = 2000
	chunkOverlap = 100
)

type Indexer struct {
	store       *Store
	embedClient *EmbedClient
}

func NewIndexer(store *Store, embedClient *EmbedClient) *Indexer {
	return &Indexer{store: store, embedClient: embedClient}
}

func (i *Indexer) Sync(ctx context.Context, workspacePath string) error {
	seen, err := i.syncWorkspace(ctx, workspacePath)
	if err != nil {
		return err
	}
	return i.removeMissing(seen)
}

func (i *Indexer) syncWorkspace(ctx context.Context, root string) (map[string]bool, error) {
	seen := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !isTextFile(path) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := i.syncFile(ctx, root, path, info)
		if err != nil {
			return err
		}
		seen[rel] = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	return seen, nil
}

func (i *Indexer) syncFile(ctx context.Context, root, absPath string, info fs.FileInfo) (string, error) {
	rel, err := filepath.Rel(root, absPath)
	if err != nil {
		return "", err
	}
	rel = filepath.ToSlash(rel)
	content, err := os.ReadFile(absPath)
	if err != nil {
		return "", err
	}
	hash := sha256Hex(content)
	f, err := i.store.GetFile(rel)
	if err != nil {
		return "", err
	}
	if f != nil && f.Hash == hash {
		return rel, nil
	}
	if err := i.reindexFile(ctx, rel, content); err != nil {
		return "", err
	}
	return rel, i.store.PutFile(File{Path: rel, Hash: hash, Mtime: info.ModTime(), Size: info.Size()})
}

func (i *Indexer) reindexFile(ctx context.Context, path string, content []byte) error {
	if err := i.store.DeleteChunksByPath(path); err != nil {
		return err
	}
	texts := ChunkText(string(content))
	vecs := make([][]float32, len(texts))
	if len(texts) > 0 {
		var err error
		vecs, err = i.embedClient.Embed(ctx, texts)
		if err != nil {
			return err
		}
		if len(vecs) != len(texts) {
			return fmt.Errorf("embedding count mismatch")
		}
	}
	for n := range texts {
		c := Chunk{ID: fmt.Sprintf("%s:%d", path, n), Path: path, StartLine: n, EndLine: n, Hash: sha256Hex([]byte(texts[n])), Text: texts[n], Embedding: vecs[n]}
		if err := i.store.PutChunk(c); err != nil {
			return err
		}
	}
	return nil
}

func (i *Indexer) removeMissing(seen map[string]bool) error {
	files, err := i.store.ListFiles()
	if err != nil {
		return err
	}
	for _, f := range files {
		if seen[f.Path] {
			continue
		}
		if err := i.store.DeleteChunksByPath(f.Path); err != nil {
			return err
		}
		if err := i.store.DeleteFile(f.Path); err != nil {
			return err
		}
	}
	return nil
}

func ChunkText(content string) []string {
	if content == "" {
		return nil
	}
	return addChunkOverlap(splitByParagraph(content, chunkSize), chunkOverlap)
}

func splitByParagraph(content string, limit int) []string {
	paras := strings.Split(content, "\n\n")
	var out []string
	cur := ""
	for _, p := range paras {
		if cur == "" {
			if len(p) <= limit {
				cur = p
				continue
			}
			out = append(out, splitFixed(p, limit)...)
			continue
		}
		next := cur + "\n\n" + p
		if len(next) <= limit {
			cur = next
			continue
		}
		out = append(out, cur)
		if len(p) <= limit {
			cur = p
			continue
		}
		out = append(out, splitFixed(p, limit)...)
		cur = ""
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

func splitFixed(s string, limit int) []string {
	if s == "" {
		return nil
	}
	var out []string
	for i := 0; i < len(s); i += limit {
		j := i + limit
		if j > len(s) {
			j = len(s)
		}
		out = append(out, s[i:j])
	}
	return out
}

func addChunkOverlap(chunks []string, overlap int) []string {
	if len(chunks) == 0 {
		return nil
	}
	out := make([]string, len(chunks))
	out[0] = chunks[0]
	for i := 1; i < len(chunks); i++ {
		out[i] = suffix(chunks[i-1], overlap) + chunks[i]
	}
	return out
}

func suffix(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func isTextFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".txt", ".go", ".py", ".js", ".ts", ".jsx", ".tsx", ".json", ".yaml", ".yml", ".toml", ".sh", ".rs", ".c", ".h", ".java", ".rb":
		return true
	}
	return false
}

func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
