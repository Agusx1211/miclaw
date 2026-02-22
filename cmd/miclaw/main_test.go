package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/agusx1211/miclaw/agent"
	"github.com/agusx1211/miclaw/config"
	"github.com/agusx1211/miclaw/model"
	"github.com/agusx1211/miclaw/provider"
	"github.com/agusx1211/miclaw/signal"
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

func TestHostExecClientFlagParsing(t *testing.T) {
	t.Parallel()

	flags, err := parseFlags([]string{"--host-exec-client", "git", "status"})
	if err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	if !flags.hostExecClient {
		t.Fatal("hostExecClient = false")
	}
	if len(flags.hostExecArgs) != 2 {
		t.Fatalf("hostExecArgs len = %d", len(flags.hostExecArgs))
	}
	if flags.hostExecArgs[0] != "git" || flags.hostExecArgs[1] != "status" {
		t.Fatalf("hostExecArgs = %#v", flags.hostExecArgs)
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

func TestStartSchedulerInjectsCronMessage(t *testing.T) {
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

	sqlStore, err := store.OpenSQLite(filepath.Join(root, "messages.db"))
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

	ag := agent.NewAgent(sqlStore.MessageStore(), nil, cronStubProvider{})
	t.Cleanup(ag.Cancel)

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

	waitMessageCount(t, sqlStore.MessageStore(), 3, 2*time.Second)
	msgs, err := sqlStore.MessageStore().List(10, 0)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if textPart(msgs[0]) != "[cron] ping" {
		t.Fatalf("unexpected first message: %#v", msgs[0])
	}
}

func TestInitRuntimeCreatesMissingWorkspace(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	statePath := filepath.Join(root, "state")
	cfgPath := filepath.Join(root, "config.json")
	cfg := config.Default()
	cfg.Provider = config.ProviderConfig{
		Backend: "lmstudio",
		Model:   "test-model",
	}
	cfg.Workspace = workspace
	cfg.StatePath = statePath
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	if _, err := os.Stat(workspace); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected missing workspace before init, got: %v", err)
	}
	deps, err := initRuntime(cfgPath)
	if err != nil {
		t.Fatalf("init runtime: %v", err)
	}
	t.Cleanup(func() {
		deps.agent.Cancel()
		_ = deps.scheduler.Close()
		if deps.memStore != nil {
			_ = deps.memStore.Close()
		}
		_ = deps.sqlStore.Close()
	})
	info, err := os.Stat(workspace)
	if err != nil {
		t.Fatalf("stat workspace: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("workspace is not a directory: %s", workspace)
	}
}

func TestBuildSandboxBridgeRunArgsIncludesWorkspaceAndCustomMounts(t *testing.T) {
	root := t.TempDir()
	exePath := filepath.Join(root, "miclaw")
	workspace := filepath.Join(root, "workspace")
	statePath := filepath.Join(root, "state")
	customHost := filepath.Join(root, "ref")
	cfg := config.Default()
	cfg.Provider = config.ProviderConfig{
		Backend: "lmstudio",
		Model:   "test-model",
	}
	cfg.Sandbox.Enabled = true
	cfg.Sandbox.Network = "bridge"
	cfg.Workspace = workspace
	cfg.StatePath = statePath
	cfg.Sandbox.Mounts = []config.Mount{
		{Host: customHost, Container: "/ref", Mode: "ro"},
	}
	args, err := buildSandboxBridgeRunArgs(exePath, &cfg)
	if err != nil {
		t.Fatalf("build sandbox bridge args: %v", err)
	}
	absExe, err := filepath.Abs(exePath)
	if err != nil {
		t.Fatalf("abs exe path: %v", err)
	}
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		t.Fatalf("abs workspace path: %v", err)
	}
	absState, err := filepath.Abs(statePath)
	if err != nil {
		t.Fatalf("abs state path: %v", err)
	}
	absCustom, err := filepath.Abs(customHost)
	if err != nil {
		t.Fatalf("abs custom path: %v", err)
	}
	if len(args) == 0 || args[0] != "run" {
		t.Fatalf("missing docker run command in %q", args)
	}
	if !containsArg(args, "-d") {
		t.Fatalf("missing detached mode in %q", args)
	}
	if !containsArg(args, "--rm") {
		t.Fatalf("missing auto-remove in %q", args)
	}
	if !containsArg(args, "--network=bridge") {
		t.Fatalf("missing network arg in %q", args)
	}
	if !containsArgPair(args, "--workdir", absWorkspace) {
		t.Fatalf("missing workdir arg in %q", args)
	}
	if !containsArgPair(args, "-e", sandboxChildEnv+"=1") {
		t.Fatalf("missing sandbox child env in %q", args)
	}
	if !containsArgPair(args, "--entrypoint", "sh") {
		t.Fatalf("missing shell entrypoint in %q", args)
	}
	if !containsArg(args, sandboxRuntimeImage) {
		t.Fatalf("missing image arg in %q", args)
	}
	if containsArg(args, "-config") {
		t.Fatalf("unexpected config arg in %q", args)
	}
	if !containsArgPair(args, "--mount", "type=bind,source="+absExe+",target="+sandboxEntrypoint+",readonly") {
		t.Fatalf("missing executable mount in %q", args)
	}
	if !containsArgPair(args, "--mount", "type=bind,source="+absWorkspace+",target="+absWorkspace) {
		t.Fatalf("missing workspace mount in %q", args)
	}
	if !containsArgPair(args, "--mount", "type=bind,source="+absCustom+",target=/ref,readonly") {
		t.Fatalf("missing custom mount in %q", args)
	}
	if containsArgPair(args, "--mount", "type=bind,source="+absState+",target="+absState) {
		t.Fatalf("unexpected state mount in %q", args)
	}
}

func TestBuildSandboxBridgeRunArgsDeduplicatesIdenticalMounts(t *testing.T) {
	root := t.TempDir()
	exePath := filepath.Join(root, "miclaw")
	workspace := filepath.Join(root, "workspace")
	cfg := config.Default()
	cfg.Provider = config.ProviderConfig{
		Backend: "lmstudio",
		Model:   "test-model",
	}
	cfg.Sandbox.Enabled = true
	cfg.Workspace = workspace
	cfg.StatePath = filepath.Join(root, "state")
	cfg.Sandbox.Mounts = []config.Mount{
		{Host: workspace, Container: workspace, Mode: "rw"},
	}
	args, err := buildSandboxBridgeRunArgs(exePath, &cfg)
	if err != nil {
		t.Fatalf("build sandbox bridge args: %v", err)
	}
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		t.Fatalf("abs workspace path: %v", err)
	}
	spec := "type=bind,source=" + absWorkspace + ",target=" + absWorkspace
	if countArgPair(args, "--mount", spec) != 1 {
		t.Fatalf("expected one workspace mount, got args=%q", args)
	}
}

func TestBuildSandboxBridgeRunArgsAddsHostCommandBridge(t *testing.T) {
	root := t.TempDir()
	exePath := filepath.Join(root, "miclaw")
	workspace := filepath.Join(root, "workspace")
	statePath := filepath.Join(root, "state")
	cfg := config.Default()
	cfg.Provider = config.ProviderConfig{
		Backend: "lmstudio",
		Model:   "test-model",
	}
	cfg.Sandbox.Enabled = true
	cfg.Sandbox.Network = "none"
	cfg.Sandbox.HostCommands = []string{"git", "docker"}
	cfg.Workspace = workspace
	cfg.StatePath = statePath
	args, err := buildSandboxBridgeRunArgs(exePath, &cfg)
	if err != nil {
		t.Fatalf("build sandbox bridge args: %v", err)
	}
	absState, err := filepath.Abs(statePath)
	if err != nil {
		t.Fatalf("abs state path: %v", err)
	}
	shimHostPath := filepath.Join(absState, sandboxHostBinHostDir)
	socketHostPath := filepath.Join(absState, sandboxHostExecHostDir, sandboxHostExecSocketFile)
	if !containsArgPair(args, "-e", "PATH="+sandboxHostBinContDir+":"+sandboxDefaultPATH) {
		t.Fatalf("missing PATH shim env in %q", args)
	}
	if !containsArgPair(args, "-e", sandboxHostExecSocketEnv+"="+sandboxHostExecSocketContPath) {
		t.Fatalf("missing host executor socket env in %q", args)
	}
	if !containsArgPair(args, "--mount", "type=bind,source="+shimHostPath+",target="+sandboxHostBinContDir+",readonly") {
		t.Fatalf("missing shim mount in %q", args)
	}
	if !containsArgPair(args, "--mount", "type=bind,source="+socketHostPath+",target="+sandboxHostExecSocketContPath) {
		t.Fatalf("missing host executor socket mount in %q", args)
	}
	clientPath := filepath.Join(shimHostPath, sandboxHostExecClientName)
	clientRaw, err := os.ReadFile(clientPath)
	if err != nil {
		t.Fatalf("read client launcher: %v", err)
	}
	clientContent := string(clientRaw)
	if !strings.Contains(clientContent, "--host-exec-client") {
		t.Fatalf("unexpected client launcher content: %q", clientContent)
	}
	for _, command := range cfg.Sandbox.HostCommands {
		path := filepath.Join(shimHostPath, command)
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatalf("stat shim %q: %v", command, err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("expected shim %q to be symlink", command)
		}
		target, err := os.Readlink(path)
		if err != nil {
			t.Fatalf("readlink %q: %v", command, err)
		}
		if target != sandboxHostExecClientName {
			t.Fatalf("unexpected shim target for %q: %q", command, target)
		}
	}
}

func TestSendSignalMessageRoutesDMAndGroup(t *testing.T) {
	cfg := config.SignalConfig{TextChunkLimit: 0}
	c := signal.NewClient("http://127.0.0.1:1", "+10000000000")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := sendSignalMessage(ctx, c, cfg, "signal:dm:user-1", "hello"); err == nil {
		t.Fatal("expected send failure on fake client")
	}
	if err := sendSignalMessage(ctx, c, cfg, "signal:group:group-1", "hello"); err == nil {
		t.Fatal("expected send failure on fake client")
	}
	if err := sendSignalMessage(ctx, c, cfg, "invalid", "hello"); err == nil {
		t.Fatal("expected invalid target error")
	}
}

func TestParseSignalTarget(t *testing.T) {
	tests := []struct {
		in      string
		wantK   string
		wantID  string
		wantErr bool
	}{
		{in: "signal:dm:user-1", wantK: "dm", wantID: "user-1"},
		{in: "signal:group:group-1", wantK: "group", wantID: "group-1"},
		{in: "signal:dm:", wantErr: true},
		{in: "signal:foo:bar", wantErr: true},
		{in: "bad", wantErr: true},
	}
	for _, tt := range tests {
		kind, id, err := parseSignalTarget(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("parseSignalTarget(%q): expected error", tt.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("parseSignalTarget(%q): %v", tt.in, err)
		}
		if kind != tt.wantK || id != tt.wantID {
			t.Fatalf("parseSignalTarget(%q) = (%q, %q), want (%q, %q)", tt.in, kind, id, tt.wantK, tt.wantID)
		}
	}
}

func TestParseSignalCommand(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "/new", want: "/new"},
		{in: "  /compact  ", want: "/compact"},
		{in: "/NEW", want: "/new"},
		{in: "/noop", want: ""},
		{in: "hello", want: ""},
	}
	for _, tt := range tests {
		got := parseSignalCommand(tt.in)
		if got != tt.want {
			t.Fatalf("parseSignalCommand(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestTypingStateClearStopsTypingLoop(t *testing.T) {
	st := newTypingState()
	var mu sync.Mutex
	calls := 0
	send := func(context.Context, string) error {
		mu.Lock()
		calls++
		mu.Unlock()
		return nil
	}
	if err := st.Start("signal:dm:user-1", 500*time.Millisecond, send); err != nil {
		t.Fatalf("start typing: %v", err)
	}
	deadline := time.Now().Add(200 * time.Millisecond)
	for {
		mu.Lock()
		n := calls
		mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("typing did not send initial indicator")
		}
		time.Sleep(5 * time.Millisecond)
	}
	st.Clear("signal:dm:user-1")
	mu.Lock()
	afterClear := calls
	mu.Unlock()
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	afterWait := calls
	mu.Unlock()
	if afterWait != afterClear {
		t.Fatalf("typing calls increased after clear: %d -> %d", afterClear, afterWait)
	}
}

func TestTypingStateTimeoutRemovesActiveEntry(t *testing.T) {
	st := newTypingState()
	send := func(context.Context, string) error { return nil }
	if err := st.Start("signal:dm:user-1", 60*time.Millisecond, send); err != nil {
		t.Fatalf("start typing: %v", err)
	}
	time.Sleep(120 * time.Millisecond)
	st.mu.Lock()
	_, ok := st.active["signal:dm:user-1"]
	st.mu.Unlock()
	if ok {
		t.Fatal("typing entry should expire after timeout")
	}
}

func TestTypingStateNoTimeoutKeepsEntryUntilClear(t *testing.T) {
	st := newTypingState()
	send := func(context.Context, string) error { return nil }
	if err := st.Start("signal:dm:user-1", 0, send); err != nil {
		t.Fatalf("start typing: %v", err)
	}
	time.Sleep(120 * time.Millisecond)
	st.mu.Lock()
	_, ok := st.active["signal:dm:user-1"]
	st.mu.Unlock()
	if !ok {
		t.Fatal("typing entry should stay active without timeout")
	}
	st.Clear("signal:dm:user-1")
}

func TestTypingStateAutoStartUsesLatestSignalDMSource(t *testing.T) {
	st := newTypingState()
	st.SetAutoTarget("signal:group:abc")
	st.SetAutoTarget("signal:dm:user-1")
	var mu sync.Mutex
	calls := 0
	lastTo := ""
	send := func(_ context.Context, to string) error {
		mu.Lock()
		calls++
		lastTo = to
		mu.Unlock()
		return nil
	}
	if err := st.StartAuto(send); err != nil {
		t.Fatalf("start auto typing: %v", err)
	}
	deadline := time.Now().Add(200 * time.Millisecond)
	for {
		mu.Lock()
		n := calls
		to := lastTo
		mu.Unlock()
		if n > 0 {
			if to != "signal:dm:user-1" {
				t.Fatalf("auto typing target = %q", to)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("auto typing did not start")
		}
		time.Sleep(5 * time.Millisecond)
	}
	st.ClearAll()
}

func TestTypingStateAutoStartRunsOncePerSignalTargetSet(t *testing.T) {
	st := newTypingState()
	st.SetAutoTarget("signal:dm:user-1")
	calls := 0
	send := func(context.Context, string) error {
		calls++
		return nil
	}
	if err := st.StartAuto(send); err != nil {
		t.Fatalf("start auto typing: %v", err)
	}
	if err := st.StartAuto(send); err != nil {
		t.Fatalf("start auto typing second call: %v", err)
	}
	if calls != 1 {
		t.Fatalf("auto typing calls = %d", calls)
	}
	st.SetAutoTarget("signal:dm:user-1")
	if err := st.StartAuto(send); err != nil {
		t.Fatalf("start auto typing after reset: %v", err)
	}
	if calls != 2 {
		t.Fatalf("auto typing calls after reset = %d", calls)
	}
	st.ClearAll()
}

func TestTypingStateClearAllStopsAllTargets(t *testing.T) {
	st := newTypingState()
	var mu sync.Mutex
	calls := 0
	send := func(context.Context, string) error {
		mu.Lock()
		calls++
		mu.Unlock()
		return nil
	}
	if err := st.Start("signal:dm:user-1", 0, send); err != nil {
		t.Fatalf("start typing user-1: %v", err)
	}
	if err := st.Start("signal:dm:user-2", 0, send); err != nil {
		t.Fatalf("start typing user-2: %v", err)
	}
	st.ClearAll()
	mu.Lock()
	afterClear := calls
	mu.Unlock()
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	afterWait := calls
	mu.Unlock()
	if afterWait != afterClear {
		t.Fatalf("typing calls increased after clear all: %d -> %d", afterClear, afterWait)
	}
	st.mu.Lock()
	n := len(st.active)
	st.mu.Unlock()
	if n != 0 {
		t.Fatalf("active entries = %d", n)
	}
}

func TestTypingStateStopCallsStopCallback(t *testing.T) {
	st := newTypingState()
	send := func(context.Context, string) error { return nil }
	stopped := ""
	stop := func(_ context.Context, to string) error {
		stopped = to
		return nil
	}
	if err := st.Start("signal:dm:user-1", 0, send); err != nil {
		t.Fatalf("start typing: %v", err)
	}
	if err := st.Stop("signal:dm:user-1", stop); err != nil {
		t.Fatalf("stop typing: %v", err)
	}
	if stopped != "signal:dm:user-1" {
		t.Fatalf("stopped target = %q", stopped)
	}
	st.mu.Lock()
	_, ok := st.active["signal:dm:user-1"]
	st.mu.Unlock()
	if ok {
		t.Fatal("typing entry should be removed after stop")
	}
}

func TestTypingStateStopAllCallsStopCallback(t *testing.T) {
	st := newTypingState()
	send := func(context.Context, string) error { return nil }
	stopped := map[string]int{}
	stop := func(_ context.Context, to string) error {
		stopped[to]++
		return nil
	}
	if err := st.Start("signal:dm:user-1", 0, send); err != nil {
		t.Fatalf("start typing user-1: %v", err)
	}
	if err := st.Start("signal:dm:user-2", 0, send); err != nil {
		t.Fatalf("start typing user-2: %v", err)
	}
	if err := st.StopAll(stop); err != nil {
		t.Fatalf("stop all typing: %v", err)
	}
	if stopped["signal:dm:user-1"] != 1 || stopped["signal:dm:user-2"] != 1 {
		t.Fatalf("stop callbacks = %#v", stopped)
	}
	st.mu.Lock()
	n := len(st.active)
	st.mu.Unlock()
	if n != 0 {
		t.Fatalf("active entries = %d", n)
	}
}

func TestIsHeartbeatPrompt(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"health check", true},
		{"HEARTBEAT please", true},
		{"daily report", false},
	}
	for _, c := range cases {
		if got := isHeartbeatPrompt(c.in); got != c.want {
			t.Fatalf("isHeartbeatPrompt(%q)=%v want %v", c.in, got, c.want)
		}
	}
}

type cronStubProvider struct{}

func (cronStubProvider) Stream(context.Context, []model.Message, []provider.ToolDef) <-chan provider.ProviderEvent {
	ch := make(chan provider.ProviderEvent, 4)
	ch <- provider.ProviderEvent{Type: provider.EventContentDelta, Delta: "ok"}
	ch <- provider.ProviderEvent{Type: provider.EventToolUseStart, ToolCallID: "sleep-1", ToolName: "sleep"}
	ch <- provider.ProviderEvent{Type: provider.EventToolUseStop, ToolCallID: "sleep-1"}
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

func waitMessageCount(t *testing.T, ms store.MessageStore, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		n, err := ms.Count()
		if err != nil {
			t.Fatalf("count messages: %v", err)
		}
		if n >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	n, err := ms.Count()
	if err != nil {
		t.Fatalf("count messages: %v", err)
	}
	t.Fatalf("timed out waiting for %d messages (got %d)", want, n)
}

func textPart(msg *model.Message) string {
	for _, part := range msg.Parts {
		if txt, ok := part.(model.TextPart); ok {
			return txt.Text
		}
	}
	return ""
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func containsArgPair(args []string, key, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == key && args[i+1] == value {
			return true
		}
	}
	return false
}

func countArgPair(args []string, key, value string) int {
	count := 0
	for i := 0; i+1 < len(args); i++ {
		if args[i] == key && args[i+1] == value {
			count++
		}
	}
	return count
}
