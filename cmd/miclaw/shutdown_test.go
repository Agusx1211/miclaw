package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/agusx1211/miclaw/agent"
	"github.com/agusx1211/miclaw/memory"
	"github.com/agusx1211/miclaw/store"
	"github.com/agusx1211/miclaw/tools"
)

func TestShutdownSequenceOrder(t *testing.T) {
	reset := setShutdownHooksForTest()
	defer reset()
	var mu sync.Mutex
	order := []string{}
	add := func(v string) { mu.Lock(); order = append(order, v); mu.Unlock() }

	release := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { <-release; add("wg.Wait"); wg.Done() }()

	shutdownAgentCancel = func(*agent.Agent) { add("agent.Cancel") }
	shutdownSchedulerStop = func(*tools.Scheduler) { add("scheduler.Stop") }
	shutdownAgentIsActive = func(*agent.Agent) bool { return false }
	shutdownSchedulerClose = func(*tools.Scheduler) error { add("scheduler.Close"); return nil }
	shutdownMemStoreClose = func(*memory.Store) error { add("memStore.Close"); return nil }
	shutdownSQLStoreClose = func(*store.SQLiteStore) error { add("sqlStore.Close"); return nil }
	shutdownTimeout = time.Second
	cancel := func() { add("cancel"); close(release) }

	deps := &runtimeDeps{agent: new(agent.Agent), scheduler: new(tools.Scheduler), memStore: new(memory.Store), sqlStore: new(store.SQLiteStore)}
	shutdown(deps, cancel, &wg, io.Discard)
	got := strings.Join(order, ",")
	want := "agent.Cancel,scheduler.Stop,cancel,wg.Wait,scheduler.Close,memStore.Close,sqlStore.Close"
	if got != want {
		t.Fatalf("order mismatch\nwant: %s\ngot:  %s", want, got)
	}
}

func TestShutdownTimeout(t *testing.T) {
	reset := setShutdownHooksForTest()
	defer reset()
	exitCh := make(chan int, 1)
	release := make(chan struct{})

	shutdownTimeout = 20 * time.Millisecond
	shutdownAgentCancel = func(*agent.Agent) {}
	shutdownSchedulerStop = func(*tools.Scheduler) {}
	shutdownAgentIsActive = func(*agent.Agent) bool { return false }
	shutdownSchedulerClose = func(*tools.Scheduler) error { <-release; return nil }
	shutdownMemStoreClose = func(*memory.Store) error { return nil }
	shutdownSQLStoreClose = func(*store.SQLiteStore) error { return nil }
	shutdownExit = func(code int) {
		select {
		case exitCh <- code:
		default:
		}
		close(release)
	}

	deps := &runtimeDeps{agent: new(agent.Agent), scheduler: new(tools.Scheduler), sqlStore: new(store.SQLiteStore)}
	var wg sync.WaitGroup
	done := make(chan struct{})
	go func() { shutdown(deps, func() {}, &wg, io.Discard); close(done) }()

	select {
	case code := <-exitCh:
		if code != 1 {
			t.Fatalf("exit code = %d", code)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout did not force exit")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not finish after timeout release")
	}
}

func TestDoubleSignalForcesExit(t *testing.T) {
	reset := setShutdownHooksForTest()
	defer reset()
	sigCh := make(chan os.Signal, 2)
	sigCh <- syscall.SIGINT
	sigCh <- syscall.SIGTERM
	<-sigCh

	exitCh := make(chan int, 1)
	shutdownExit = func(code int) {
		select {
		case exitCh <- code:
		default:
		}
	}
	var stderr bytes.Buffer
	stop := watchSecondSignal(sigCh, &stderr)
	defer stop()

	select {
	case code := <-exitCh:
		if code != 1 {
			t.Fatalf("exit code = %d", code)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("second signal did not force exit")
	}
	if !strings.Contains(stderr.String(), "forced exit") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func setShutdownHooksForTest() func() {
	oldTimeout, oldExit := shutdownTimeout, shutdownExit
	oldAgentCancel, oldAgentIsActive := shutdownAgentCancel, shutdownAgentIsActive
	oldSchedulerStop, oldSchedulerClose := shutdownSchedulerStop, shutdownSchedulerClose
	oldMemClose, oldSQLClose := shutdownMemStoreClose, shutdownSQLStoreClose
	return func() {
		shutdownTimeout, shutdownExit = oldTimeout, oldExit
		shutdownAgentCancel, shutdownAgentIsActive = oldAgentCancel, oldAgentIsActive
		shutdownSchedulerStop, shutdownSchedulerClose = oldSchedulerStop, oldSchedulerClose
		shutdownMemStoreClose, shutdownSQLStoreClose = oldMemClose, oldSQLClose
	}
}
