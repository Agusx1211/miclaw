package agent

import "sync"

type InputSource string

const (
	SourceSignal  InputSource = "signal"
	SourceWebhook InputSource = "webhook"
	SourceAPI     InputSource = "api"
	SourceCron    InputSource = "cron"
)

type Input struct {
	SessionID string
	Content   string
	Source    InputSource
	Metadata  map[string]string
}

type InputQueue struct {
	mu    sync.Mutex
	items []Input
}

func (q *InputQueue) Push(input Input) {
	must(q != nil, "queue must not be nil")
	q.mu.Lock()
	defer q.mu.Unlock()
	before := len(q.items)
	must(before >= 0, "queue length must be non-negative")
	q.items = append(q.items, input)
	must(len(q.items) == before+1, "push must increase queue length by one")
}

func (q *InputQueue) Pop() (Input, bool) {
	must(q != nil, "queue must not be nil")
	q.mu.Lock()
	defer q.mu.Unlock()
	n := len(q.items)
	must(n >= 0, "queue length must be non-negative")
	if n == 0 {
		return Input{}, false
	}
	input := q.items[0]
	q.items[0] = Input{}
	q.items = q.items[1:]
	must(len(q.items) == n-1, "pop must decrease queue length by one")
	return input, true
}

func (q *InputQueue) Drain() []Input {
	must(q != nil, "queue must not be nil")
	q.mu.Lock()
	defer q.mu.Unlock()
	n := len(q.items)
	must(n >= 0, "queue length must be non-negative")
	out := make([]Input, n)
	copy(out, q.items)
	for i := range q.items {
		q.items[i] = Input{}
	}
	q.items = q.items[:0]
	must(len(out) == n, "drain output length must match queue length")
	must(len(q.items) == 0, "drain must empty queue")
	return out
}

func (q *InputQueue) Len() int {
	must(q != nil, "queue must not be nil")
	q.mu.Lock()
	defer q.mu.Unlock()
	n := len(q.items)
	must(n >= 0, "queue length must be non-negative")
	must(n <= cap(q.items), "queue length must not exceed capacity")
	return n
}
