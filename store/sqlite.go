package store

import (
	"database/sql"
	"encoding/json"
	"strings"
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

func must(ok bool, msg string) {
	if msg == "" {
		panic("assertion message must not be empty")
	}
	if !ok {
		panic(msg)
	}
}

func OpenSQLite(path string) (*SQLiteStore, error) {
	must(path != "", "sqlite path must not be empty")
	must(strings.TrimSpace(path) == path, "sqlite path must be trimmed")

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if err := initSchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	s := &SQLiteStore{db: db}
	s.Sessions = &sqliteSessionStore{db: db}
	s.Messages = &sqliteMessageStore{db: db}

	must(s.db != nil, "sqlite db must not be nil")
	must(s.Sessions != nil && s.Messages != nil, "sqlite stores must not be nil")
	return s, nil
}

func (s *SQLiteStore) Close() error {
	must(s != nil, "sqlite store must not be nil")
	must(s.db != nil, "sqlite db must not be nil")

	return s.db.Close()
}

func (s *SQLiteStore) SessionStore() SessionStore {
	must(s != nil, "sqlite store must not be nil")
	must(s.Sessions != nil, "session store must not be nil")
	return s.Sessions
}

func (s *SQLiteStore) MessageStore() MessageStore {
	must(s != nil, "sqlite store must not be nil")
	must(s.Messages != nil, "message store must not be nil")
	return s.Messages
}

func initSchema(db *sql.DB) error {
	must(db != nil, "sqlite db must not be nil")
	must(schemaSessions != "" && schemaMessages != "", "schema statements must not be empty")

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

	must(schemaMessagesIndex != "", "messages index statement must not be empty")
	must(db.Stats().OpenConnections >= 0, "db stats must be readable")
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
	must(s != nil, "session store must not be nil")
	must(session != nil, "session must not be nil")

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
	must(s != nil, "session store must not be nil")
	must(id != "", "session id must not be empty")

	row := s.db.QueryRow(
		`SELECT id, parent_session_id, title, message_count, prompt_tokens,
		 completion_tokens, summary_message_id, cost, created_at, updated_at
		 FROM sessions WHERE id = ?`,
		id,
	)
	return scanSession(row)
}

func (s *sqliteSessionStore) Update(session *model.Session) error {
	must(s != nil, "session store must not be nil")
	must(session != nil, "session must not be nil")

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
	must(s != nil, "session store must not be nil")
	must(limit >= 0 && offset >= 0, "limit and offset must be non-negative")

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
	must(out != nil, "sessions list must not be nil")
	must(len(out) >= 0, "sessions length must be non-negative")
	return out, nil
}

func (s *sqliteSessionStore) Delete(id string) error {
	must(s != nil, "session store must not be nil")
	must(id != "", "session id must not be empty")

	_, err := s.db.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	return err
}

func (s *sqliteMessageStore) Create(msg *model.Message) error {
	must(s != nil, "message store must not be nil")
	must(msg != nil, "message must not be nil")

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
	must(s != nil, "message store must not be nil")
	must(id != "", "message id must not be empty")

	row := s.db.QueryRow(
		`SELECT id, session_id, role, parts_json, created_at
		 FROM messages WHERE id = ?`,
		id,
	)
	return scanMessage(row)
}

func (s *sqliteMessageStore) ListBySession(sessionID string, limit, offset int) ([]*model.Message, error) {
	must(s != nil, "message store must not be nil")
	must(sessionID != "", "session id must not be empty")
	must(limit >= 0 && offset >= 0, "limit and offset must be non-negative")

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
	must(out != nil, "message list must not be nil")
	must(cap(out) >= len(out), "message list capacity must match length")
	return out, nil
}

func (s *sqliteMessageStore) DeleteBySession(sessionID string) error {
	must(s != nil, "message store must not be nil")
	must(sessionID != "", "session id must not be empty")

	_, err := s.db.Exec(`DELETE FROM messages WHERE session_id = ?`, sessionID)
	return err
}

func (s *sqliteMessageStore) CountBySession(sessionID string) (int, error) {
	must(s != nil, "message store must not be nil")
	must(sessionID != "", "session id must not be empty")

	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE session_id = ?`, sessionID).Scan(&n)
	if err != nil {
		return 0, err
	}
	must(n >= 0, "message count must be non-negative")
	must(n < 1<<30, "message count too large")
	return n, nil
}

func (s *sqliteMessageStore) ReplaceSessionMessages(sessionID string, msgs []*model.Message) error {
	must(s != nil, "message store must not be nil")
	must(sessionID != "", "session id must not be empty")

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM messages WHERE session_id = ?`, sessionID); err != nil {
		_ = tx.Rollback()
		return err
	}
	for _, msg := range msgs {
		must(msg != nil, "message must not be nil")
		must(msg.SessionID == sessionID, "message session id mismatch")
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
	must(len(msgs) >= 0, "messages length must be non-negative")
	must(tx != nil, "transaction must not be nil")
	return nil
}

func scanSession(r rowScanner) (*model.Session, error) {
	must(r != nil, "row scanner must not be nil")
	must(schemaSessions != "", "session schema must not be empty")

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
	must(v.ID != "", "session id must not be empty")
	must(!v.CreatedAt.IsZero() && !v.UpdatedAt.IsZero(), "session timestamps must not be zero")
	return v, nil
}

func scanMessage(r rowScanner) (*model.Message, error) {
	must(r != nil, "row scanner must not be nil")
	must(schemaMessages != "", "message schema must not be empty")

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
	must(v.ID == id, "message id mismatch")
	must(v.SessionID == sessionID && string(v.Role) == role, "message metadata mismatch")
	return v, nil
}

func encodeMessage(msg *model.Message) (string, error) {
	must(msg != nil, "message must not be nil")
	must(msg.ID != "", "message id must not be empty")

	b, err := json.Marshal(msg)
	if err != nil {
		return "", err
	}
	must(len(b) > 0, "message JSON must not be empty")
	must(b[0] == '{', "message JSON must start with object")
	return string(b), nil
}

func decodeMessage(raw string) (*model.Message, error) {
	must(raw != "", "message JSON must not be empty")
	must(strings.TrimSpace(raw) == raw, "message JSON must be trimmed")

	var v model.Message
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return nil, err
	}
	must(v.ID != "", "decoded message id must not be empty")
	must(v.SessionID != "", "decoded message session id must not be empty")
	return &v, nil
}

func timeToDB(v time.Time) string {
	must(!v.IsZero(), "time value must not be zero")
	must(v.Location() != nil, "time location must not be nil")

	return v.UTC().Format(time.RFC3339Nano)
}

func timeFromDB(v string) (time.Time, error) {
	must(v != "", "time string must not be empty")
	must(strings.TrimSpace(v) == v, "time string must be trimmed")

	t, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		return time.Time{}, err
	}
	must(!t.IsZero(), "parsed time must not be zero")
	must(t.Location() != nil, "parsed time location must not be nil")
	return t, nil
}
