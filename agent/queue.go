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

	q.mu.Lock()
	defer q.mu.Unlock()

	q.items = append(q.items, input)

}

func (q *InputQueue) Pop() (Input, bool) {

	q.mu.Lock()
	defer q.mu.Unlock()
	n := len(q.items)

	if n == 0 {
		return Input{}, false
	}
	input := q.items[0]
	q.items[0] = Input{}
	q.items = q.items[1:]

	return input, true
}

func (q *InputQueue) Drain() []Input {

	q.mu.Lock()
	defer q.mu.Unlock()
	n := len(q.items)

	out := make([]Input, n)
	copy(out, q.items)
	for i := range q.items {
		q.items[i] = Input{}
	}
	q.items = q.items[:0]

	return out
}

func (q *InputQueue) Len() int {

	q.mu.Lock()
	defer q.mu.Unlock()
	n := len(q.items)

	return n
}
