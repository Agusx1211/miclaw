package agent

import "sync"

type Broker[T any] struct {
	mu   sync.Mutex
	subs []chan T
}

func NewBroker[T any]() *Broker[T] {
	return &Broker[T]{}
}

func (b *Broker[T]) Publish(event T) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, sub := range b.subs {
		select {
		case sub <- event:
		default:
		}
	}
}

func (b *Broker[T]) Subscribe() (<-chan T, func()) {
	ch := make(chan T, 64)
	b.mu.Lock()
	b.subs = append(b.subs, ch)
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		for i, sub := range b.subs {
			if sub != ch {
				continue
			}
			last := len(b.subs) - 1
			b.subs[i] = b.subs[last]
			b.subs[last] = nil
			b.subs = b.subs[:last]
			close(ch)
			break
		}
		b.mu.Unlock()
	}
}
