package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/agusx1211/miclaw/agent"
	"github.com/agusx1211/miclaw/memory"
	"github.com/agusx1211/miclaw/store"
	"github.com/agusx1211/miclaw/tools"
)

var (
	shutdownTimeout      = 30 * time.Second
	shutdownPollInterval = 10 * time.Millisecond
	shutdownExit         = os.Exit

	shutdownAgentCancel    = func(a *agent.Agent) { a.Cancel() }
	shutdownAgentIsActive  = func(a *agent.Agent) bool { return a.IsActive() }
	shutdownSchedulerStop  = func(s *tools.Scheduler) { s.Stop() }
	shutdownSchedulerClose = func(s *tools.Scheduler) error {
		return s.Close()
	}
	shutdownMemStoreClose = func(s *memory.Store) error {
		return s.Close()
	}
	shutdownSQLStoreClose = func(s *store.SQLiteStore) error {
		return s.Close()
	}
)

func shutdown(deps *runtimeDeps, cancel context.CancelFunc, wg *sync.WaitGroup, stderr io.Writer) {

	done := make(chan struct{})
	timer := time.AfterFunc(shutdownTimeout, func() {
		fmt.Fprintln(stderr, "shutdown timeout, forcing exit")
		shutdownExit(1)
	})
	go func() {
		shutdownRun(deps, cancel, wg, stderr)
		close(done)
	}()
	<-done
	timer.Stop()
	fmt.Fprintln(stderr, "shutdown complete")
}

func shutdownRun(deps *runtimeDeps, cancel context.CancelFunc, wg *sync.WaitGroup, stderr io.Writer) {

	shutdownAgentCancel(deps.agent)
	shutdownSchedulerStop(deps.scheduler)
	cancel()
	wg.Wait()
	for shutdownAgentIsActive(deps.agent) {
		time.Sleep(shutdownPollInterval)
	}
	_ = shutdownSchedulerClose(deps.scheduler)
	if deps.memStore != nil {
		_ = shutdownMemStoreClose(deps.memStore)
	}
	_ = shutdownSQLStoreClose(deps.sqlStore)
	if deps.bridge != nil {
		_ = deps.bridge.Close()
	}
}

func watchSecondSignal(sigCh <-chan os.Signal, stderr io.Writer) func() {

	done := make(chan struct{})
	go func() {
		select {
		case <-sigCh:
			fmt.Fprintln(stderr, "forced exit")
			shutdownExit(1)
		case <-done:
		}
	}()
	return func() { close(done) }
}
