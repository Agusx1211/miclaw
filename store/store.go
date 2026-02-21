package store

import "github.com/agusx1211/miclaw/agent"

type SessionStore interface {
	Create(session *agent.Session) error
	Get(id string) (*agent.Session, error)
	Update(session *agent.Session) error
	List(limit, offset int) ([]*agent.Session, error)
	Delete(id string) error
}

type MessageStore interface {
	Create(msg *agent.Message) error
	Get(id string) (*agent.Message, error)
	ListBySession(sessionID string, limit, offset int) ([]*agent.Message, error)
	DeleteBySession(sessionID string) error
	CountBySession(sessionID string) (int, error)
	ReplaceSessionMessages(sessionID string, msgs []*agent.Message) error
}
