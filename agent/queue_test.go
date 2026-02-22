package agent

import (
	"sync"
	"testing"
)

func TestInputQueueDrainOrder(t *testing.T) {
	q := &InputQueue{}
	q.Push(Input{Source: "a", Content: "one"})
	q.Push(Input{Source: "b", Content: "two"})
	q.Push(Input{Source: "c", Content: "three"})
	items := q.Drain()
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	if items[0].Source != "a" || items[1].Source != "b" || items[2].Source != "c" {
		t.Fatalf("unexpected order: %#v", items)
	}
}

func TestInputQueueDrainClearsQueue(t *testing.T) {
	q := &InputQueue{}
	q.Push(Input{Source: "a", Content: "one"})
	if q.Len() != 1 {
		t.Fatalf("expected len 1, got %d", q.Len())
	}
	_ = q.Drain()
	if q.Len() != 0 {
		t.Fatalf("expected len 0, got %d", q.Len())
	}
	if len(q.Drain()) != 0 {
		t.Fatal("expected empty drain")
	}
}

func TestInputQueueConcurrentPushAndDrain(t *testing.T) {
	q := &InputQueue{}
	var wg sync.WaitGroup
	wg.Add(8)
	for i := 0; i < 8; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				q.Push(Input{Source: "cron", Content: "tick"})
			}
		}()
	}
	wg.Wait()
	items := q.Drain()
	if len(items) != 800 {
		t.Fatalf("expected 800 items, got %d", len(items))
	}
}
