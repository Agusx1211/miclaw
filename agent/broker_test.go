package agent

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBrokerPublishNoSubscribers(t *testing.T) {
	b := NewBroker[string]()
	for i := range 100 {
		b.Publish("event-" + string(rune('a'+(i%26))))
	}
}

func TestBrokerSingleSubscriber(t *testing.T) {
	b := NewBroker[string]()
	events, unsub := b.Subscribe()
	defer unsub()
	b.Publish("alpha")
	select {
	case got := <-events:
		if got != "alpha" {
			t.Fatalf("unexpected event: want %q got %q", "alpha", got)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("did not receive event")
	}
	select {
	case got := <-events:
		t.Fatalf("unexpected extra event: %q", got)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestBrokerMultipleSubscribers(t *testing.T) {
	b := NewBroker[int]()
	a, unsubA := b.Subscribe()
	bSub, unsubB := b.Subscribe()
	defer unsubA()
	defer unsubB()

	values := []int{10, 20, 30}
	for _, v := range values {
		b.Publish(v)
	}

	for _, got := range values {
		select {
		case gotA := <-a:
			if gotA != got {
				t.Fatalf("subscriber A mismatch: want %d got %d", got, gotA)
			}
		case <-time.After(200 * time.Millisecond):
			t.Fatalf("subscriber A timed out waiting for event")
		}
	}
	for _, got := range values {
		select {
		case gotB := <-bSub:
			if gotB != got {
				t.Fatalf("subscriber B mismatch: want %d got %d", got, gotB)
			}
		case <-time.After(200 * time.Millisecond):
			t.Fatalf("subscriber B timed out waiting for event")
		}
	}
}

func TestBrokerUnsubscribeStopsDelivery(t *testing.T) {
	b := NewBroker[int]()
	ch, unsub := b.Subscribe()
	b.Publish(1)
	select {
	case got := <-ch:
		if got != 1 {
			t.Fatalf("expected first event, got %d", got)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("did not receive initial event")
	}
	unsub()
	b.Publish(2)
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatalf("received event after unsubscribe")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("unsubscribe did not stop delivery")
	}
}

func TestBrokerPublishNonBlockingOnFullBuffer(t *testing.T) {
	b := NewBroker[int]()
	ch, unsub := b.Subscribe()
	defer unsub()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 128; i++ {
			b.Publish(i)
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("publish blocked when subscriber buffer was full")
	}

	seen := 0
	for seen < 64 {
		select {
		case <-ch:
			seen++
		case <-time.After(200 * time.Millisecond):
			t.Fatalf("timed out waiting for buffered events")
		}
	}
	select {
	case <-ch:
		t.Fatalf("expected buffer to hold exactly 64 events")
	case <-time.After(20 * time.Millisecond):
	}
}

func TestBrokerConcurrentPublishSubscribe(t *testing.T) {
	b := NewBroker[int]()
	const (
		subscribers = 8
		publishers  = 4
		perPublisher = 500
	)

	start := make(chan struct{})
	stop := make(chan struct{})
	var readyWG sync.WaitGroup
	var recvWG sync.WaitGroup
	var readWG sync.WaitGroup
	var delivered int64

	readyWG.Add(subscribers)
	for i := 0; i < subscribers; i++ {
		readWG.Add(1)
		go func() {
			defer readWG.Done()
			ch, unsub := b.Subscribe()
			readyWG.Done()
			<-start
			defer unsub()
			for {
				select {
				case _, ok := <-ch:
					if !ok {
						return
					}
					atomic.AddInt64(&delivered, 1)
				case <-stop:
					return
				}
			}
		}()
	}

	readyWG.Wait()
	close(start)

	recvWG.Add(publishers)
	for i := 0; i < publishers; i++ {
		base := i * perPublisher
		go func() {
			defer recvWG.Done()
			for j := 0; j < perPublisher; j++ {
				b.Publish(base + j)
			}
		}()
	}
	recvWG.Wait()
	close(stop)
	readWG.Wait()

	if atomic.LoadInt64(&delivered) == 0 {
		t.Fatalf("expected at least one delivered event")
	}
}
