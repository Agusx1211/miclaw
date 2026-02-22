package agent

import "sync"

type Input struct {
	Source   string
	Content  string
	Metadata map[string]string
}

type InputQueue struct {
	mu    sync.Mutex
	items []Input
}

func (q *InputQueue) Push(input Input) {

	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = append(q.items, input)
}

func (q *InputQueue) Drain() []Input {

	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]Input, len(q.items))
	copy(out, q.items)
	q.items = q.items[:0]

	return out
}

func (q *InputQueue) Len() int {

	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}
