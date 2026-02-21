package memory

import (
	"database/sql"
	"time"

	_ "modernc.org/sqlite"
)

type File struct {
	Path  string
	Hash  string
	Mtime time.Time
	Size  int64
}

type Chunk struct {
	ID        string
	Path      string
	StartLine int
	EndLine   int
	Hash      string
	Text      string
	Embedding []float32
}

type SearchResult struct {
	Chunk
	Score float64
}

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := initMemorySchema(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func initMemorySchema(db *sql.DB) error {
	for _, q := range memorySchemaSQL {
		if _, err := db.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

var memorySchemaSQL = []string{
	`CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT)`,
	`CREATE TABLE IF NOT EXISTS files (
		path TEXT PRIMARY KEY,
		hash TEXT,
		mtime DATETIME,
		size INTEGER
	)`,
	`CREATE TABLE IF NOT EXISTS chunks (
		id TEXT PRIMARY KEY,
		path TEXT,
		start_line INTEGER,
		end_line INTEGER,
		hash TEXT,
		text TEXT,
		embedding BLOB,
		updated_at DATETIME
	)`,
	`CREATE INDEX IF NOT EXISTS idx_chunks_path ON chunks(path)`,
	`CREATE VIRTUAL TABLE IF NOT EXISTS fts USING fts5(id UNINDEXED, text, content=chunks, content_rowid=rowid)`,
	ftsTriggerInsert,
	ftsTriggerDelete,
	ftsTriggerUpdate,
}

const ftsTriggerInsert = `CREATE TRIGGER IF NOT EXISTS chunks_ai AFTER INSERT ON chunks BEGIN
	INSERT INTO fts(rowid, id, text) VALUES (new.rowid, new.id, new.text);
END`

const ftsTriggerDelete = `CREATE TRIGGER IF NOT EXISTS chunks_ad AFTER DELETE ON chunks BEGIN
	INSERT INTO fts(fts, rowid, id, text) VALUES ('delete', old.rowid, old.id, old.text);
END`

const ftsTriggerUpdate = `CREATE TRIGGER IF NOT EXISTS chunks_au AFTER UPDATE ON chunks BEGIN
	INSERT INTO fts(fts, rowid, id, text) VALUES ('delete', old.rowid, old.id, old.text);
	INSERT INTO fts(rowid, id, text) VALUES (new.rowid, new.id, new.text);
END`
