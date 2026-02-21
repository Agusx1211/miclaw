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
	Sessions SessionStore
	Messages MessageStore
}

type sqliteSessionStore struct {
	db *sql.DB
}

type sqliteMessageStore struct {
	db *sql.DB
}

var _ SessionStore = (*sqliteSessionStore)(nil)
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
	s.Sessions = &sqliteSessionStore{db: db}
	s.Messages = &sqliteMessageStore{db: db}

	return s, nil
}

func (s *SQLiteStore) Close() error {

	return s.db.Close()
}

func (s *SQLiteStore) SessionStore() SessionStore {

	return s.Sessions
}

func (s *SQLiteStore) MessageStore() MessageStore {

	return s.Messages
}

func initSchema(db *sql.DB) error {

	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return err
	}
	if _, err := db.Exec(schemaSessions); err != nil {
		return err
	}
	if _, err := db.Exec(schemaMessages); err != nil {
		return err
	}
	if _, err := db.Exec(schemaMessagesIndex); err != nil {
		return err
	}

	return nil
}

const schemaSessions = `
CREATE TABLE IF NOT EXISTS sessions (
	id TEXT PRIMARY KEY,
	parent_session_id TEXT,
	title TEXT,
	message_count INTEGER,
	prompt_tokens INTEGER,
	completion_tokens INTEGER,
	summary_message_id TEXT,
	cost REAL,
	created_at DATETIME,
	updated_at DATETIME
)`

const schemaMessages = `
CREATE TABLE IF NOT EXISTS messages (
	id TEXT PRIMARY KEY,
	session_id TEXT,
	role TEXT,
	parts_json TEXT,
	created_at DATETIME,
	FOREIGN KEY(session_id) REFERENCES sessions(id) ON DELETE CASCADE
)`

const schemaMessagesIndex = `
CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id, created_at)`

func (s *sqliteSessionStore) Create(session *model.Session) error {

	_, err := s.db.Exec(
		`INSERT INTO sessions (
			id, parent_session_id, title, message_count, prompt_tokens,
			completion_tokens, summary_message_id, cost, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		session.ID,
		session.ParentSessionID,
		session.Title,
		session.MessageCount,
		session.PromptTokens,
		session.CompletionTokens,
		session.SummaryMessageID,
		session.Cost,
		timeToDB(session.CreatedAt),
		timeToDB(session.UpdatedAt),
	)
	return err
}

func (s *sqliteSessionStore) Get(id string) (*model.Session, error) {

	row := s.db.QueryRow(
		`SELECT id, parent_session_id, title, message_count, prompt_tokens,
		 completion_tokens, summary_message_id, cost, created_at, updated_at
		 FROM sessions WHERE id = ?`,
		id,
	)
	return scanSession(row)
}

func (s *sqliteSessionStore) Update(session *model.Session) error {

	_, err := s.db.Exec(
		`UPDATE sessions SET
			parent_session_id = ?,
			title = ?,
			message_count = ?,
			prompt_tokens = ?,
			completion_tokens = ?,
			summary_message_id = ?,
			cost = ?,
			created_at = ?,
			updated_at = ?
		 WHERE id = ?`,
		session.ParentSessionID,
		session.Title,
		session.MessageCount,
		session.PromptTokens,
		session.CompletionTokens,
		session.SummaryMessageID,
		session.Cost,
		timeToDB(session.CreatedAt),
		timeToDB(session.UpdatedAt),
		session.ID,
	)
	return err
}

func (s *sqliteSessionStore) List(limit, offset int) ([]*model.Session, error) {

	rows, err := s.db.Query(
		`SELECT id, parent_session_id, title, message_count, prompt_tokens,
		 completion_tokens, summary_message_id, cost, created_at, updated_at
		 FROM sessions ORDER BY created_at, id LIMIT ? OFFSET ?`,
		limit,
		offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*model.Session, 0)
	for rows.Next() {
		v, err := scanSession(rows)
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

func (s *sqliteSessionStore) Delete(id string) error {

	_, err := s.db.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	return err
}

func (s *sqliteMessageStore) Create(msg *model.Message) error {

	raw, err := encodeMessage(msg)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT INTO messages (id, session_id, role, parts_json, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		msg.ID,
		msg.SessionID,
		string(msg.Role),
		raw,
		timeToDB(msg.CreatedAt),
	)
	return err
}

func (s *sqliteMessageStore) Get(id string) (*model.Message, error) {

	row := s.db.QueryRow(
		`SELECT id, session_id, role, parts_json, created_at
		 FROM messages WHERE id = ?`,
		id,
	)
	return scanMessage(row)
}

func (s *sqliteMessageStore) ListBySession(sessionID string, limit, offset int) ([]*model.Message, error) {

	rows, err := s.db.Query(
		`SELECT id, session_id, role, parts_json, created_at
		 FROM messages WHERE session_id = ?
		 ORDER BY created_at, id LIMIT ? OFFSET ?`,
		sessionID,
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

func (s *sqliteMessageStore) DeleteBySession(sessionID string) error {

	_, err := s.db.Exec(`DELETE FROM messages WHERE session_id = ?`, sessionID)
	return err
}

func (s *sqliteMessageStore) CountBySession(sessionID string) (int, error) {

	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE session_id = ?`, sessionID).Scan(&n)
	if err != nil {
		return 0, err
	}

	return n, nil
}

func (s *sqliteMessageStore) ReplaceSessionMessages(sessionID string, msgs []*model.Message) error {

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM messages WHERE session_id = ?`, sessionID); err != nil {
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
			`INSERT INTO messages (id, session_id, role, parts_json, created_at)
			 VALUES (?, ?, ?, ?, ?)`,
			msg.ID,
			msg.SessionID,
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

func scanSession(r rowScanner) (*model.Session, error) {

	v := &model.Session{}
	var createdAt string
	var updatedAt string
	err := r.Scan(
		&v.ID,
		&v.ParentSessionID,
		&v.Title,
		&v.MessageCount,
		&v.PromptTokens,
		&v.CompletionTokens,
		&v.SummaryMessageID,
		&v.Cost,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return nil, err
	}
	created, err := timeFromDB(createdAt)
	if err != nil {
		return nil, err
	}
	updated, err := timeFromDB(updatedAt)
	if err != nil {
		return nil, err
	}
	v.CreatedAt = created
	v.UpdatedAt = updated

	return v, nil
}

func scanMessage(r rowScanner) (*model.Message, error) {

	var id string
	var sessionID string
	var role string
	var raw string
	var createdAt string
	if err := r.Scan(&id, &sessionID, &role, &raw, &createdAt); err != nil {
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
