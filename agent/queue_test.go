package agent

import (
	"sync"
	"testing"
)

func TestQueuePushPopFIFO(t *testing.T) {
	q := &InputQueue{}
	q.Push(Input{SessionID: "s1", Content: "one", Source: SourceAPI})
	q.Push(Input{SessionID: "s2", Content: "two", Source: SourceWebhook})
	q.Push(Input{SessionID: "s3", Content: "three", Source: SourceSignal})

	first, ok := q.Pop()
	if !ok {
		t.Fatal("expected first pop to succeed")
	}
	if first.SessionID != "s1" {
		t.Fatalf("unexpected first item: %q", first.SessionID)
	}

	second, ok := q.Pop()
	if !ok {
		t.Fatal("expected second pop to succeed")
	}
	if second.SessionID != "s2" {
		t.Fatalf("unexpected second item: %q", second.SessionID)
	}

	third, ok := q.Pop()
	if !ok {
		t.Fatal("expected third pop to succeed")
	}
	if third.SessionID != "s3" {
		t.Fatalf("unexpected third item: %q", third.SessionID)
	}
}

func TestQueuePopEmpty(t *testing.T) {
	q := &InputQueue{}
	_, ok := q.Pop()
	if ok {
		t.Fatal("expected pop from empty queue to fail")
	}
}

func TestQueueDrainReturnsAll(t *testing.T) {
	q := &InputQueue{}
	q.Push(Input{SessionID: "s1", Content: "one", Source: SourceAPI})
	q.Push(Input{SessionID: "s2", Content: "two", Source: SourceWebhook})
	q.Push(Input{SessionID: "s3", Content: "three", Source: SourceSignal})

	items := q.Drain()
	if len(items) != 3 {
		t.Fatalf("expected 3 drained items, got %d", len(items))
	}
	if items[0].SessionID != "s1" || items[1].SessionID != "s2" || items[2].SessionID != "s3" {
		t.Fatalf("unexpected drain order: %#v", items)
	}
	if q.Len() != 0 {
		t.Fatalf("expected empty queue after drain, got %d", q.Len())
	}
}

func TestQueueLen(t *testing.T) {
	q := &InputQueue{}
	if q.Len() != 0 {
		t.Fatalf("expected initial len 0, got %d", q.Len())
	}

	q.Push(Input{SessionID: "s1", Content: "one", Source: SourceAPI})
	q.Push(Input{SessionID: "s2", Content: "two", Source: SourceWebhook})
	if q.Len() != 2 {
		t.Fatalf("expected len 2 after pushes, got %d", q.Len())
	}

	_, ok := q.Pop()
	if !ok {
		t.Fatal("expected pop to succeed")
	}
	if q.Len() != 1 {
		t.Fatalf("expected len 1 after pop, got %d", q.Len())
	}
}

func TestQueueConcurrentPushes(t *testing.T) {
	q := &InputQueue{}
	const workers = 8
	const perWorker = 40

	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for range perWorker {
				q.Push(Input{SessionID: "s", Content: "x", Source: SourceCron, Metadata: map[string]string{"w": "x"}})
			}
		}()
	}
	wg.Wait()

	want := workers * perWorker
	if q.Len() != want {
		t.Fatalf("unexpected queue length: want %d got %d", want, q.Len())
	}
}
