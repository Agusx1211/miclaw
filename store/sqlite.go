package store

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/agusx1211/miclaw/model"
	_ "modernc.org/sqlite"
)

type SQLiteStore struct {
	db       *sql.DB
	Messages MessageStore
}

type sqliteMessageStore struct {
	db *sql.DB
}

var _ MessageStore = (*sqliteMessageStore)(nil)

type rowScanner interface {
	Scan(dest ...any) error
}

func OpenSQLite(path string) (*SQLiteStore, error) {

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := initSchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	s := &SQLiteStore{db: db}
	s.Messages = &sqliteMessageStore{db: db}

	return s, nil
}

func (s *SQLiteStore) Close() error {

	return s.db.Close()
}

func (s *SQLiteStore) MessageStore() MessageStore {

	return s.Messages
}

func initSchema(db *sql.DB) error {

	if _, err := db.Exec(schemaMessages); err != nil {
		return err
	}
	if _, err := db.Exec(schemaMessagesIndex); err != nil {
		return err
	}

	return nil
}

const schemaMessages = `
CREATE TABLE IF NOT EXISTS messages (
	id TEXT PRIMARY KEY,
	role TEXT,
	parts_json TEXT,
	created_at DATETIME
)`

const schemaMessagesIndex = `
CREATE INDEX IF NOT EXISTS idx_messages_created ON messages(created_at, id)`

func (s *sqliteMessageStore) Create(msg *model.Message) error {

	raw, err := encodeMessage(msg)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT INTO messages (id, role, parts_json, created_at)
		 VALUES (?, ?, ?, ?)`,
		msg.ID,
		string(msg.Role),
		raw,
		timeToDB(msg.CreatedAt),
	)
	return err
}

func (s *sqliteMessageStore) Get(id string) (*model.Message, error) {

	row := s.db.QueryRow(
		`SELECT id, role, parts_json, created_at
		 FROM messages WHERE id = ?`,
		id,
	)
	return scanMessage(row)
}

func (s *sqliteMessageStore) List(limit, offset int) ([]*model.Message, error) {

	rows, err := s.db.Query(
		`SELECT id, role, parts_json, created_at
		 FROM messages
		 ORDER BY created_at, id LIMIT ? OFFSET ?`,
		limit,
		offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*model.Message, 0)
	for rows.Next() {
		v, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return out, nil
}

func (s *sqliteMessageStore) DeleteAll() error {

	_, err := s.db.Exec(`DELETE FROM messages`)
	return err
}

func (s *sqliteMessageStore) Count() (int, error) {

	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&n)
	if err != nil {
		return 0, err
	}

	return n, nil
}

func (s *sqliteMessageStore) ReplaceAll(msgs []*model.Message) error {

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM messages`); err != nil {
		_ = tx.Rollback()
		return err
	}
	for _, msg := range msgs {

		raw, err := encodeMessage(msg)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		_, err = tx.Exec(
			`INSERT INTO messages (id, role, parts_json, created_at)
			 VALUES (?, ?, ?, ?)`,
			msg.ID,
			string(msg.Role),
			raw,
			timeToDB(msg.CreatedAt),
		)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return err
	}

	return nil
}

func scanMessage(r rowScanner) (*model.Message, error) {

	var id string
	var role string
	var raw string
	var createdAt string
	if err := r.Scan(&id, &role, &raw, &createdAt); err != nil {
		return nil, err
	}
	v, err := decodeMessage(raw)
	if err != nil {
		return nil, err
	}
	created, err := timeFromDB(createdAt)
	if err != nil {
		return nil, err
	}
	v.CreatedAt = created

	return v, nil
}

func encodeMessage(msg *model.Message) (string, error) {

	b, err := json.Marshal(msg)
	if err != nil {
		return "", err
	}

	return string(b), nil
}

func decodeMessage(raw string) (*model.Message, error) {

	var v model.Message
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return nil, err
	}

	return &v, nil
}

func timeToDB(v time.Time) string {

	return v.UTC().Format(time.RFC3339Nano)
}

func timeFromDB(v string) (time.Time, error) {

	t, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		return time.Time{}, err
	}

	return t, nil
}
