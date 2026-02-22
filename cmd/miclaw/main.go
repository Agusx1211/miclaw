package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	osSignal "os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/agusx1211/miclaw/agent"
	"github.com/agusx1211/miclaw/config"
	"github.com/agusx1211/miclaw/memory"
	"github.com/agusx1211/miclaw/prompt"
	"github.com/agusx1211/miclaw/provider"
	"github.com/agusx1211/miclaw/setup"
	signalpipe "github.com/agusx1211/miclaw/signal"
	"github.com/agusx1211/miclaw/store"
	"github.com/agusx1211/miclaw/tools"
	"github.com/agusx1211/miclaw/webhook"
)

const runtimeLogTextLimit = 180
const typingKeepaliveInterval = 8 * time.Second

type runtimeDeps struct {
	cfg         *config.Config
	sqlStore    *store.SQLiteStore
	memStore    *memory.Store
	embedClient *memory.EmbedClient
	scheduler   *tools.Scheduler
	agent       *agent.Agent
	signal      *signalpipe.Client
	typing      *typingState
	bridge      *sandboxBridge
}

type cliFlags struct {
	configPath     string
	showVersion    bool
	setup          bool
	toolCall       string
	hostExecClient bool
	hostExecArgs   []string
}

func main() {

	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		var codeErr *exitCodeError
		if errors.As(err, &codeErr) {
			if codeErr.Message != "" {
				fmt.Fprintln(os.Stderr, codeErr.Message)
			}
			os.Exit(codeErr.Code)
		}
		log.Fatalf("%v", err)
	}
}

func run(args []string, stdout, stderr io.Writer) error {

	flags, err := parseFlags(args)
	if err != nil {
		return err
	}
	if flags.showVersion {
		fmt.Fprintln(stdout, versionString())
		return nil
	}
	if flags.toolCall != "" {
		return runToolCall(flags.toolCall, stdout)
	}
	if flags.hostExecClient {
		err := runHostExecClient(flags.hostExecArgs, stdout, stderr)
		if err == nil {
			return nil
		}
		var codeErr *exitCodeError
		if errors.As(err, &codeErr) {
			return codeErr
		}
		return &exitCodeError{Code: 1, Message: err.Error()}
	}
	configPath, err := expandHome(flags.configPath)
	if err != nil {
		return err
	}
	if flags.setup {
		return setup.Run(configPath, os.Stdin, stdout)
	}

	deps, err := initRuntime(configPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) || !stdinIsTTY() {
			return err
		}
		fmt.Fprintf(stderr, "config not found at %s, starting setup\n", configPath)
		if err := setup.Run(configPath, os.Stdin, stdout); err != nil {
			return err
		}
		deps, err = initRuntime(configPath)
		if err != nil {
			return err
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 2)
	var wg sync.WaitGroup

	startMemorySync(ctx, deps, stderr)
	if err := startScheduler(ctx, deps); err != nil {
		return err
	}
	startSignalPipeline(ctx, deps, &wg, errCh)
	startWebhookServer(ctx, deps, &wg, errCh)

	fmt.Fprintf(stderr, "%s\n", versionString())
	fmt.Fprintf(stderr, "workspace=%s state=%s backend=%s model=%s\n", deps.cfg.Workspace, deps.cfg.StatePath, deps.cfg.Provider.Backend, deps.cfg.Provider.Model)
	log.Printf("[trace] compact runtime tracing enabled")

	sigCh := make(chan os.Signal, 2)
	osSignal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer osSignal.Stop(sigCh)
	select {
	case sig := <-sigCh:
		fmt.Fprintf(stderr, "received %s, shutting down\n", sig.String())
		stopForced := watchSecondSignal(sigCh, stderr)
		shutdown(deps, cancel, &wg, stderr)
		stopForced()
		return nil
	case err := <-errCh:
		shutdown(deps, cancel, &wg, stderr)
		return err
	}
}

func parseFlags(args []string) (cliFlags, error) {

	fs := flag.NewFlagSet("miclaw", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", "~/.miclaw/config.json", "path to config file")
	showVersion := fs.Bool("version", false, "print version and exit")
	setupRun := fs.Bool("setup", false, "run setup/configuration TUI and exit")
	configureRun := fs.Bool("configure", false, "run setup/configuration TUI and exit")
	toolCall := fs.String("tool-call", "", "internal: execute one tool call and exit")
	hostExecClient := fs.Bool("host-exec-client", false, "internal: run a host command through sandbox proxy")
	if err := fs.Parse(args); err != nil {
		return cliFlags{}, err
	}
	hostExecArgs := fs.Args()
	if !*hostExecClient && len(hostExecArgs) != 0 {
		return cliFlags{}, fmt.Errorf("unexpected positional arguments")
	}
	return cliFlags{
		configPath:     *configPath,
		showVersion:    *showVersion,
		setup:          *setupRun || *configureRun,
		toolCall:       *toolCall,
		hostExecClient: *hostExecClient,
		hostExecArgs:   hostExecArgs,
	}, nil
}

func initRuntime(configPath string) (*runtimeDeps, error) {

	path, err := expandHome(configPath)
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	var bridge *sandboxBridge
	if cfg.Sandbox.Enabled && !isSandboxChild() {
		bridge, err = startSandboxBridge(cfg)
		if err != nil {
			return nil, err
		}
	}
	cleanupBridge := bridge != nil
	if bridge != nil {
		defer func() {
			if cleanupBridge {
				_ = bridge.Close()
			}
		}()
	}
	if err := ensureRuntimePaths(cfg); err != nil {
		return nil, err
	}
	sqlStore, memStore, embedClient, err := openStores(cfg)
	if err != nil {
		return nil, err
	}
	workspace, skills, err := loadPromptData(cfg.Workspace)
	if err != nil {
		return nil, err
	}
	var prov provider.LLMProvider
	switch cfg.Provider.Backend {
	case "openrouter":
		prov = provider.NewOpenRouter(cfg.Provider)
	case "lmstudio":
		prov = provider.NewLMStudio(cfg.Provider)
	case "codex":
		prov = provider.NewCodex(cfg.Provider)
	default:
		log.Fatalf("unsupported provider backend %q", cfg.Provider.Backend)
	}
	scheduler, err := tools.NewScheduler(filepath.Join(cfg.StatePath, "cron.sqlite"))
	if err != nil {
		return nil, err
	}
	var signalClient *signalpipe.Client
	if cfg.Signal.Enabled {
		baseURL := fmt.Sprintf("http://%s:%d", cfg.Signal.HTTPHost, cfg.Signal.HTTPPort)
		signalClient = signalpipe.NewClient(baseURL, cfg.Signal.Account)
	}
	typing := newTypingState()
	sendMessage := func(ctx context.Context, to, content string) error {
		if signalClient == nil {
			return fmt.Errorf("signal is disabled")
		}
		return sendSignalMessage(ctx, signalClient, cfg.Signal, to, content)
	}
	var startTyping func(context.Context, string, time.Duration) error
	var stopTyping func(context.Context, string) error
	if signalClient != nil {
		startTyping = func(_ context.Context, to string, duration time.Duration) error {
			return typing.Start(to, duration, func(callCtx context.Context, target string) error {
				return sendSignalTyping(callCtx, signalClient, target)
			})
		}
		stopTyping = func(_ context.Context, to string) error {
			return typing.Stop(to, func(callCtx context.Context, target string) error {
				return sendSignalTypingStop(callCtx, signalClient, target)
			})
		}
	}
	var ag *agent.Agent
	toolList := tools.MainAgentTools(tools.MainToolDeps{
		Sandbox:     cfg.Sandbox,
		Memory:      memStore,
		Embed:       embedClient,
		Scheduler:   scheduler,
		SendMessage: sendMessage,
		StartTyping: startTyping,
		StopTyping:  stopTyping,
	})
	if bridge != nil {
		toolList = wrapToolsWithSandboxBridge(toolList, bridge)
	}
	ag = agent.NewAgent(sqlStore.Messages, toolList, prov)
	ag.SetNoToolSleepRounds(cfg.NoToolSleepRounds)
	ag.SetWorkspace(workspace)
	ag.SetSkills(skills)
	ag.SetTrace(func(format string, args ...any) {
		if signalClient != nil {
			switch format {
			case "wake":
				if err := typing.StartAuto(func(callCtx context.Context, target string) error {
					return sendSignalTyping(callCtx, signalClient, target)
				}); err != nil {
					log.Printf("[signal] typing_auto_error err=%v", err)
				}
			case "sleep":
				if err := typing.StopAll(func(callCtx context.Context, target string) error {
					return sendSignalTypingStop(callCtx, signalClient, target)
				}); err != nil {
					log.Printf("[signal] typing_auto_error err=%v", err)
				}
			}
		}
		log.Printf("[agent] "+format, args...)
	})

	cleanupBridge = false
	return &runtimeDeps{
		cfg:         cfg,
		sqlStore:    sqlStore,
		memStore:    memStore,
		embedClient: embedClient,
		scheduler:   scheduler,
		agent:       ag,
		signal:      signalClient,
		typing:      typing,
		bridge:      bridge,
	}, nil
}

func ensureRuntimePaths(cfg *config.Config) error {
	return os.MkdirAll(cfg.Workspace, 0o755)
}

func openStores(cfg *config.Config) (*store.SQLiteStore, *memory.Store, *memory.EmbedClient, error) {

	if err := os.MkdirAll(cfg.StatePath, 0o755); err != nil {
		return nil, nil, nil, err
	}
	sqlStore, err := store.OpenSQLite(filepath.Join(cfg.StatePath, "sessions.sqlite"))
	if err != nil {
		return nil, nil, nil, err
	}
	if !cfg.Memory.Enabled {
		return sqlStore, nil, nil, nil
	}
	if err := os.MkdirAll(filepath.Join(cfg.StatePath, "memory"), 0o755); err != nil {
		return nil, nil, nil, err
	}
	memStore, err := memory.Open(filepath.Join(cfg.StatePath, "memory", "agent.sqlite"))
	if err != nil {
		return nil, nil, nil, err
	}
	embedClient := memory.NewEmbedClient(cfg.Memory.EmbeddingURL, cfg.Memory.EmbeddingAPIKey, cfg.Memory.EmbeddingModel)
	return sqlStore, memStore, embedClient, nil
}

func loadPromptData(workspacePath string) (*prompt.Workspace, []prompt.SkillSummary, error) {

	workspace, err := prompt.LoadWorkspace(workspacePath)
	if err != nil {
		return nil, nil, err
	}
	skills, err := prompt.LoadSkills(workspacePath)
	if err != nil {
		return nil, nil, err
	}
	return workspace, skills, nil
}

func startMemorySync(ctx context.Context, deps *runtimeDeps, stderr io.Writer) {

	if !deps.cfg.Memory.Enabled {
		return
	}
	indexer := memory.NewIndexer(deps.memStore, deps.embedClient)
	go func() {
		if err := indexer.Sync(ctx, deps.cfg.Workspace); err != nil {
			fmt.Fprintf(stderr, "memory sync error: %v\n", err)
		}
	}()
}

func startScheduler(ctx context.Context, deps *runtimeDeps) error {

	if _, err := deps.scheduler.ListJobs(); err != nil {
		return err
	}
	deps.scheduler.Start(ctx, func(source, content string) {
		if isHeartbeatPrompt(content) && deps.agent.IsActive() {
			log.Printf("[cron] skip source=%s active=true msg=%q", source, compactRuntimeText(content))
			return
		}
		log.Printf("[cron] in source=%s msg=%q", source, compactRuntimeText(content))
		deps.agent.Inject(agent.Input{Source: source, Content: content})
	})
	return nil
}

func startSignalPipeline(ctx context.Context, deps *runtimeDeps, wg *sync.WaitGroup, errCh chan<- error) {

	if !deps.cfg.Signal.Enabled {
		return
	}
	pipeline := signalpipe.NewPipeline(
		deps.signal,
		deps.cfg.Signal,
		func(source, content string, metadata map[string]string) {
			log.Printf("[signal] in source=%s msg=%q", source, compactRuntimeText(content))
			if handleSignalCommand(ctx, deps, source, content) {
				return
			}
			deps.typing.SetAutoTarget(source)
			if deps.agent.IsActive() {
				if err := deps.typing.StartAuto(func(callCtx context.Context, target string) error {
					return sendSignalTyping(callCtx, deps.signal, target)
				}); err != nil {
					log.Printf("[signal] typing_auto_error err=%v", err)
				}
			}
			deps.agent.Inject(agent.Input{Source: source, Content: content, Metadata: metadata})
		},
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			err := pipeline.Start(ctx)
			if err == nil || errors.Is(err, context.Canceled) {
				return
			}
			if strings.Contains(err.Error(), "signal events stream closed") {
				log.Printf("[signal] pipeline closed; retrying in 1s")
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Second):
					continue
				}
			}
			errCh <- fmt.Errorf("signal pipeline: %v", err)
			return
		}
	}()
}

func parseSignalCommand(content string) string {
	switch strings.ToLower(strings.TrimSpace(content)) {
	case "/new":
		return "/new"
	case "/compact":
		return "/compact"
	default:
		return ""
	}
}

func handleSignalCommand(ctx context.Context, deps *runtimeDeps, source, content string) bool {
	switch parseSignalCommand(content) {
	case "":
		return false
	case "/new":
		deps.agent.Cancel()
		deadline := time.Now().Add(3 * time.Second)
		for deps.agent.IsActive() && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if deps.agent.IsActive() {
			_ = sendSignalMessage(ctx, deps.signal, deps.cfg.Signal, source, "agent is busy; try /new again in a few seconds")
			return true
		}
		_ = deps.typing.StopAll(func(callCtx context.Context, target string) error {
			return sendSignalTypingStop(callCtx, deps.signal, target)
		})
		if err := deps.sqlStore.MessageStore().DeleteAll(); err != nil {
			log.Printf("[signal] command=/new err=%v", err)
			_ = sendSignalMessage(ctx, deps.signal, deps.cfg.Signal, source, "failed to reset thread")
			return true
		}
		_ = sendSignalMessage(ctx, deps.signal, deps.cfg.Signal, source, "thread reset")
		return true
	case "/compact":
		if deps.agent.IsActive() {
			_ = sendSignalMessage(ctx, deps.signal, deps.cfg.Signal, source, "agent is busy; try /compact again in a few seconds")
			return true
		}
		_ = sendSignalMessage(ctx, deps.signal, deps.cfg.Signal, source, "compacting context")
		go func() {
			compactCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			if err := deps.agent.Compact(compactCtx); err != nil {
				log.Printf("[signal] command=/compact err=%v", err)
				_ = sendSignalMessage(context.Background(), deps.signal, deps.cfg.Signal, source, "compaction failed")
				return
			}
			_ = sendSignalMessage(context.Background(), deps.signal, deps.cfg.Signal, source, "compaction complete")
		}()
		return true
	default:
		return false
	}
}

func startWebhookServer(ctx context.Context, deps *runtimeDeps, wg *sync.WaitGroup, errCh chan<- error) {

	if !deps.cfg.Webhook.Enabled {
		return
	}
	srv := webhook.New(deps.cfg.Webhook, func(source, content string, metadata map[string]string) {
		log.Printf("[webhook] in source=%s msg=%q", source, compactRuntimeText(content))
		deps.agent.Inject(agent.Input{Source: source, Content: content, Metadata: metadata})
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := srv.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
			errCh <- fmt.Errorf("webhook server: %v", err)
		}
	}()
}

func sendSignalMessage(ctx context.Context, client *signalpipe.Client, cfg config.SignalConfig, to, content string) error {
	log.Printf("[signal] out to=%s msg=%q", to, compactRuntimeText(content))
	kind, target, err := parseSignalTarget(to)
	if err != nil {
		return err
	}
	text, styles := signalpipe.MarkdownToSignal(content)
	limit := cfg.TextChunkLimit
	if limit <= 0 {
		limit = len(text)
	}
	if kind == "group" {
		for _, chunk := range signalpipe.ChunkText(text, limit) {
			if err := client.SendGroup(ctx, target, chunk, styles); err != nil {
				log.Printf("[signal] out_error to=%s err=%v", to, err)
				return err
			}
		}
		log.Printf("[signal] out_ok to=%s", to)
		return nil
	}
	for _, chunk := range signalpipe.ChunkText(text, limit) {
		if err := client.Send(ctx, target, chunk, styles); err != nil {
			log.Printf("[signal] out_error to=%s err=%v", to, err)
			return err
		}
	}
	log.Printf("[signal] out_ok to=%s", to)
	return nil
}

func sendSignalTyping(ctx context.Context, client *signalpipe.Client, to string) error {
	log.Printf("[signal] typing to=%s", to)
	kind, target, err := parseSignalTarget(to)
	if err != nil {
		return err
	}
	if kind != "dm" {
		return fmt.Errorf("typing target must be signal:dm:<recipient>")
	}
	if err := client.SendTyping(ctx, target); err != nil {
		log.Printf("[signal] typing_error to=%s err=%v", to, err)
		return err
	}
	log.Printf("[signal] typing_ok to=%s", to)
	return nil
}

func sendSignalTypingStop(ctx context.Context, client *signalpipe.Client, to string) error {
	log.Printf("[signal] typing_stop to=%s", to)
	kind, target, err := parseSignalTarget(to)
	if err != nil {
		return err
	}
	if kind != "dm" {
		return fmt.Errorf("typing target must be signal:dm:<recipient>")
	}
	if err := client.SendTypingStop(ctx, target); err != nil {
		log.Printf("[signal] typing_stop_error to=%s err=%v", to, err)
		return err
	}
	log.Printf("[signal] typing_stop_ok to=%s", to)
	return nil
}

func parseSignalTarget(to string) (string, string, error) {
	parts := strings.SplitN(to, ":", 3)
	if len(parts) != 3 || parts[0] != "signal" || strings.TrimSpace(parts[2]) == "" {
		return "", "", fmt.Errorf("invalid signal target %q", to)
	}
	if parts[1] != "dm" && parts[1] != "group" {
		return "", "", fmt.Errorf("invalid signal target %q", to)
	}
	return parts[1], strings.TrimSpace(parts[2]), nil
}

type typingState struct {
	mu          sync.Mutex
	active      map[string]chan struct{}
	autoTarget  string
	autoPending bool
}

func newTypingState() *typingState {
	return &typingState{active: map[string]chan struct{}{}}
}

func (s *typingState) Start(to string, duration time.Duration, send func(context.Context, string) error) error {
	if err := send(context.Background(), to); err != nil {
		return err
	}
	stop := make(chan struct{})
	s.mu.Lock()
	if prev, ok := s.active[to]; ok {
		close(prev)
	}
	s.active[to] = stop
	s.mu.Unlock()
	go func() {
		ticker := time.NewTicker(typingKeepaliveInterval)
		defer ticker.Stop()
		var timeout *time.Timer
		var timeoutCh <-chan time.Time
		if duration > 0 {
			timeout = time.NewTimer(duration)
			timeoutCh = timeout.C
		}
		if timeout != nil {
			defer timeout.Stop()
		}
		defer func() {
			s.mu.Lock()
			if s.active[to] == stop {
				delete(s.active, to)
			}
			s.mu.Unlock()
		}()
		for {
			select {
			case <-stop:
				return
			case <-timeoutCh:
				return
			case <-ticker.C:
				s.mu.Lock()
				live := s.active[to] == stop
				s.mu.Unlock()
				if !live {
					return
				}
				_ = send(context.Background(), to)
			}
		}
	}()
	return nil
}

func (s *typingState) Clear(to string) {
	stop, ok := s.take(to)
	if ok {
		close(stop)
	}
}

func (s *typingState) ClearAll() {
	stops := s.takeAll()
	for _, stop := range stops {
		close(stop)
	}
}

func (s *typingState) Stop(to string, stopTyping func(context.Context, string) error) error {
	s.Clear(to)
	return stopTyping(context.Background(), to)
}

func (s *typingState) StopAll(stopTyping func(context.Context, string) error) error {
	targets := s.activeTargets()
	s.ClearAll()
	for _, to := range targets {
		if err := stopTyping(context.Background(), to); err != nil {
			return err
		}
	}
	return nil
}

func (s *typingState) take(to string) (chan struct{}, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stop, ok := s.active[to]
	if ok {
		delete(s.active, to)
	}
	return stop, ok
}

func (s *typingState) takeAll() []chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	stops := make([]chan struct{}, 0, len(s.active))
	for to, stop := range s.active {
		delete(s.active, to)
		stops = append(stops, stop)
	}
	return stops
}

func (s *typingState) activeTargets() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	targets := make([]string, 0, len(s.active))
	for to := range s.active {
		targets = append(targets, to)
	}
	return targets
}

func (s *typingState) SetAutoTarget(source string) {
	kind, id, err := parseSignalTarget(source)
	if err != nil || kind != "dm" {
		return
	}
	s.mu.Lock()
	s.autoTarget = "signal:dm:" + id
	s.autoPending = true
	s.mu.Unlock()
}

func (s *typingState) StartAuto(send func(context.Context, string) error) error {
	s.mu.Lock()
	to := s.autoTarget
	pending := s.autoPending
	s.autoPending = false
	s.mu.Unlock()
	if to == "" || !pending {
		return nil
	}
	return s.Start(to, 0, send)
}

func isHeartbeatPrompt(content string) bool {
	v := strings.ToLower(content)
	return strings.Contains(v, "heartbeat") || strings.Contains(v, "health check")
}

func compactRuntimeText(raw string) string {

	clean := strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if len(clean) <= runtimeLogTextLimit {
		return clean
	}
	return clean[:runtimeLogTextLimit-3] + "..."
}

func versionString() string {

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "miclaw unknown"
	}
	version := info.Main.Version
	if version == "" || version == "(devel)" {
		version = "devel"
	}
	rev, modified, ts := "", "", ""
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			modified = s.Value
		case "vcs.time":
			ts = s.Value
		}
	}
	if rev == "" {
		return "miclaw " + version
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	suffix := rev
	if ts != "" {
		suffix += " " + ts
	}
	if modified == "true" {
		suffix += " dirty"
	}
	return "miclaw " + version + " (" + suffix + ")"
}

func expandHome(path string) (string, error) {

	if path == "" {
		return "", fmt.Errorf("config path is required")
	}
	if path[0] != '~' {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if path == "~" {
		return home, nil
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:]), nil
	}
	return "", fmt.Errorf("unsupported home path %q", path)
}

func stdinIsTTY() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}
