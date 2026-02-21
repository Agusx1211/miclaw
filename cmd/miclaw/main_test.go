package main

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/agusx1211/miclaw/agent"
	"github.com/agusx1211/miclaw/model"
	"github.com/agusx1211/miclaw/provider"
	"github.com/agusx1211/miclaw/store"
	"github.com/agusx1211/miclaw/tools"
)

func TestVersionFlag(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run([]string{"--version"}, &stdout, &stderr); err != nil {
		t.Fatalf("run version: %v", err)
	}
	got := strings.TrimSpace(stdout.String())
	if !strings.HasPrefix(got, "miclaw ") {
		t.Fatalf("version output = %q", got)
	}
}

func TestConfigFlagParsing(t *testing.T) {
	t.Parallel()

	flags, err := parseFlags([]string{"--config", "/tmp/custom.json"})
	if err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	if flags.configPath != "/tmp/custom.json" {
		t.Fatalf("config path = %q", flags.configPath)
	}
	if flags.showVersion {
		t.Fatalf("showVersion = true")
	}
	if flags.setup {
		t.Fatalf("setup = true")
	}
}

func TestSetupFlagParsing(t *testing.T) {
	t.Parallel()

	flags, err := parseFlags([]string{"--setup"})
	if err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	if !flags.setup {
		t.Fatal("setup = false")
	}
}

func TestConfigureFlagParsing(t *testing.T) {
	t.Parallel()

	flags, err := parseFlags([]string{"--configure"})
	if err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	if !flags.setup {
		t.Fatal("setup = false")
	}
}

func TestBuild(t *testing.T) {
	t.Parallel()

	cmd := exec.Command("go", "build", "./cmd/miclaw")
	cmd.Dir = filepath.Join("..", "..")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build ./cmd/miclaw failed: %v\n%s", err, out)
	}
}

func TestStartSchedulerFiresRuntimeAddedJob(t *testing.T) {
	root := t.TempDir()
	scheduler, err := tools.NewScheduler(filepath.Join(root, "cron.db"))
	if err != nil {
		t.Fatalf("new scheduler: %v", err)
	}
	t.Cleanup(func() {
		if err := scheduler.Close(); err != nil {
			t.Fatalf("close scheduler: %v", err)
		}
	})
	jobs, err := scheduler.ListJobs()
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("expected no startup jobs, got %d", len(jobs))
	}

	sqlStore, err := store.OpenSQLite(filepath.Join(root, "sessions.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlStore.Close(); err != nil {
			t.Fatalf("close sqlite: %v", err)
		}
	})

	var now atomic.Int64
	base := time.Date(2026, 2, 21, 10, 0, 0, 0, time.UTC)
	now.Store(base.UnixNano())
	setSchedulerField(scheduler, "tick", 10*time.Millisecond)
	setSchedulerField(scheduler, "now", func() time.Time { return time.Unix(0, now.Load()).UTC() })

	ag := agent.NewAgent(sqlStore.SessionStore(), sqlStore.MessageStore(), nil, cronStubProvider{})
	t.Cleanup(ag.Cancel)
	events, unsubscribe := ag.Events().Subscribe()
	defer unsubscribe()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deps := &runtimeDeps{scheduler: scheduler, agent: ag}
	if err := startScheduler(ctx, deps); err != nil {
		t.Fatalf("start scheduler: %v", err)
	}
	t.Cleanup(scheduler.Stop)

	if _, err := scheduler.AddJob("*/1 * * * *", "ping"); err != nil {
		t.Fatalf("add job: %v", err)
	}
	now.Store(base.Add(time.Minute).UnixNano())

	waitCronResponse(t, events, 2*time.Second)
}

type cronStubProvider struct{}

func (cronStubProvider) Stream(context.Context, []model.Message, []provider.ToolDef) <-chan provider.ProviderEvent {
	ch := make(chan provider.ProviderEvent, 2)
	ch <- provider.ProviderEvent{Type: provider.EventContentDelta, Delta: "ok"}
	ch <- provider.ProviderEvent{Type: provider.EventComplete}
	close(ch)
	return ch
}

func (cronStubProvider) Model() provider.ModelInfo {
	return provider.ModelInfo{}
}

func setSchedulerField(s *tools.Scheduler, field string, value any) {
	v := reflect.ValueOf(s).Elem().FieldByName(field)
	reflect.NewAt(v.Type(), unsafe.Pointer(v.UnsafeAddr())).Elem().Set(reflect.ValueOf(value))
}

func waitCronResponse(t *testing.T, events <-chan agent.AgentEvent, timeout time.Duration) {
	t.Helper()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case ev := <-events:
			if ev.SessionID != "cron" {
				continue
			}
			if ev.Type == agent.EventError {
				t.Fatalf("agent error: %v", ev.Error)
			}
			if ev.Type == agent.EventResponse {
				return
			}
		case <-timer.C:
			t.Fatal("timed out waiting for cron response event")
		}
	}
}
