package memory

import (
	"database/sql"
	"encoding/binary"
	"math"
	"time"
)

func (s *Store) SetMeta(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = ?`,
		key, value, value,
	)
	return err
}

func (s *Store) GetMeta(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

func (s *Store) PutFile(f File) error {
	_, err := s.db.Exec(
		`INSERT INTO files (path, hash, mtime, size) VALUES (?, ?, ?, ?)
		 ON CONFLICT(path) DO UPDATE SET hash = ?, mtime = ?, size = ?`,
		f.Path, f.Hash, f.Mtime.UTC().Format(time.RFC3339Nano), f.Size,
		f.Hash, f.Mtime.UTC().Format(time.RFC3339Nano), f.Size,
	)
	return err
}

func (s *Store) GetFile(path string) (*File, error) {
	var f File
	var mtime string
	err := s.db.QueryRow(
		`SELECT path, hash, mtime, size FROM files WHERE path = ?`, path,
	).Scan(&f.Path, &f.Hash, &mtime, &f.Size)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t, err := time.Parse(time.RFC3339Nano, mtime)
	if err != nil {
		return nil, err
	}
	f.Mtime = t
	return &f, nil
}

func (s *Store) DeleteFile(path string) error {
	_, err := s.db.Exec(`DELETE FROM files WHERE path = ?`, path)
	return err
}

func (s *Store) ListFiles() ([]File, error) {
	rows, err := s.db.Query(`SELECT path, hash, mtime, size FROM files ORDER BY path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []File
	for rows.Next() {
		var f File
		var mtime string
		if err := rows.Scan(&f.Path, &f.Hash, &mtime, &f.Size); err != nil {
			return nil, err
		}
		t, err := time.Parse(time.RFC3339Nano, mtime)
		if err != nil {
			return nil, err
		}
		f.Mtime = t
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) PutChunk(c Chunk) error {
	_, err := s.db.Exec(
		`INSERT INTO chunks (id, path, start_line, end_line, hash, text, embedding, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   path = ?, start_line = ?, end_line = ?, hash = ?, text = ?, embedding = ?, updated_at = ?`,
		c.ID, c.Path, c.StartLine, c.EndLine, c.Hash, c.Text, encodeEmbedding(c.Embedding), time.Now().UTC().Format(time.RFC3339Nano),
		c.Path, c.StartLine, c.EndLine, c.Hash, c.Text, encodeEmbedding(c.Embedding), time.Now().UTC().Format(time.RFC3339Nano),
	)
	return err
}

func (s *Store) GetChunk(id string) (*Chunk, error) {
	var c Chunk
	var emb []byte
	err := s.db.QueryRow(
		`SELECT id, path, start_line, end_line, hash, text, embedding FROM chunks WHERE id = ?`, id,
	).Scan(&c.ID, &c.Path, &c.StartLine, &c.EndLine, &c.Hash, &c.Text, &emb)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c.Embedding = decodeEmbedding(emb)
	return &c, nil
}

func (s *Store) DeleteChunksByPath(path string) error {
	_, err := s.db.Exec(`DELETE FROM chunks WHERE path = ?`, path)
	return err
}

func (s *Store) ListChunksByPath(path string) ([]Chunk, error) {
	rows, err := s.db.Query(
		`SELECT id, path, start_line, end_line, hash, text, embedding FROM chunks WHERE path = ? ORDER BY start_line`,
		path,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Chunk
	for rows.Next() {
		var c Chunk
		var emb []byte
		if err := rows.Scan(&c.ID, &c.Path, &c.StartLine, &c.EndLine, &c.Hash, &c.Text, &emb); err != nil {
			return nil, err
		}
		c.Embedding = decodeEmbedding(emb)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) SearchFTS(query string, limit int) ([]SearchResult, error) {
	rows, err := s.db.Query(
		`SELECT c.id, c.path, c.start_line, c.end_line, c.hash, c.text, c.embedding, rank
		 FROM fts f JOIN chunks c ON f.id = c.id
		 WHERE fts MATCH ? ORDER BY rank LIMIT ?`,
		query, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SearchResult
	for rows.Next() {
		var r SearchResult
		var emb []byte
		var rank float64
		if err := rows.Scan(&r.ID, &r.Path, &r.StartLine, &r.EndLine, &r.Hash, &r.Text, &emb, &rank); err != nil {
			return nil, err
		}
		r.Embedding = decodeEmbedding(emb)
		r.Score = -rank // FTS5 rank is negative (lower = better)
		out = append(out, r)
	}
	return out, rows.Err()
}

func encodeEmbedding(v []float32) []byte {
	if len(v) == 0 {
		return nil
	}
	b := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

func decodeEmbedding(b []byte) []float32 {
	if len(b) == 0 {
		return nil
	}
	n := len(b) / 4
	v := make([]float32, n)
	for i := range n {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}
