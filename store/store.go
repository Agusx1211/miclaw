package store

import "github.com/agusx1211/miclaw/model"

type SessionStore interface {
	Create(session *model.Session) error
	Get(id string) (*model.Session, error)
	Update(session *model.Session) error
	List(limit, offset int) ([]*model.Session, error)
	Delete(id string) error
}

type MessageStore interface {
	Create(msg *model.Message) error
	Get(id string) (*model.Message, error)
	ListBySession(sessionID string, limit, offset int) ([]*model.Message, error)
	DeleteBySession(sessionID string) error
	CountBySession(sessionID string) (int, error)
	ReplaceSessionMessages(sessionID string, msgs []*model.Message) error
}
