package store

import "github.com/agusx1211/miclaw/model"

type MessageStore interface {
	Create(msg *model.Message) error
	Get(id string) (*model.Message, error)
	List(limit, offset int) ([]*model.Message, error)
	DeleteAll() error
	Count() (int, error)
	ReplaceAll(msgs []*model.Message) error
}
