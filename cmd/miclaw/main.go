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

type runtimeDeps struct {
	cfg         *config.Config
	sqlStore    *store.SQLiteStore
	memStore    *memory.Store
	embedClient *memory.EmbedClient
	scheduler   *tools.Scheduler
	agent       *agent.Agent
}

type cliFlags struct {
	configPath  string
	showVersion bool
	setup       bool
}

func main() {

	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
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
	if err := fs.Parse(args); err != nil {
		return cliFlags{}, err
	}
	if fs.NArg() != 0 {
		return cliFlags{}, fmt.Errorf("unexpected positional arguments")
	}
	return cliFlags{
		configPath:  *configPath,
		showVersion: *showVersion,
		setup:       *setupRun || *configureRun,
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
	var ag *agent.Agent
	toolList := tools.MainAgentTools(tools.MainToolDeps{
		Sessions:  sqlStore.Sessions,
		Messages:  sqlStore.Messages,
		Provider:  prov,
		Sandbox:   cfg.Sandbox,
		Memory:    memStore,
		Embed:     embedClient,
		Scheduler: scheduler,
		Model:     cfg.Provider.Model,
		IsActive: func() bool {
			if ag == nil {
				return false
			}
			return ag.IsActive()
		},
	})
	ag = agent.NewAgent(sqlStore.Sessions, sqlStore.Messages, toolList, prov)
	ag.SetWorkspace(workspace)
	ag.SetSkills(skills)

	return &runtimeDeps{
		cfg:         cfg,
		sqlStore:    sqlStore,
		memStore:    memStore,
		embedClient: embedClient,
		scheduler:   scheduler,
		agent:       ag,
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
	deps.scheduler.Start(ctx, func(sessionID, content string) {
		deps.agent.Enqueue(agent.Input{SessionID: sessionID, Content: content, Source: agent.SourceCron})
	})
	return nil
}

func startSignalPipeline(ctx context.Context, deps *runtimeDeps, wg *sync.WaitGroup, errCh chan<- error) {

	if !deps.cfg.Signal.Enabled {
		return
	}
	baseURL := fmt.Sprintf("http://%s:%d", deps.cfg.Signal.HTTPHost, deps.cfg.Signal.HTTPPort)
	client := signalpipe.NewClient(baseURL, deps.cfg.Signal.Account)
	pipeline := signalpipe.NewPipeline(
		client,
		deps.cfg.Signal,
		func(sessionID, content string, metadata map[string]string) {
			deps.agent.Enqueue(agent.Input{SessionID: sessionID, Content: content, Source: agent.SourceSignal, Metadata: metadata})
		},
		func() (<-chan signalpipe.Event, func()) {
			return subscribeSignalEvents(deps.agent)
		},
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := pipeline.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
			errCh <- fmt.Errorf("signal pipeline: %v", err)
		}
	}()
}

func subscribeSignalEvents(ag *agent.Agent) (<-chan signalpipe.Event, func()) {

	events, unsubscribe := ag.Events().Subscribe()
	out := make(chan signalpipe.Event, 64)
	go func() {
		defer close(out)
		for ev := range events {
			if !strings.HasPrefix(ev.SessionID, "signal:") {
				continue
			}
			if ev.Type == agent.EventResponse {
				if ev.Message == nil {
					continue
				}
				text := messageText(ev.Message)
				if text == "" {
					continue
				}
				out <- signalpipe.Event{SessionID: ev.SessionID, Text: text}
				continue
			}
			if ev.Type == agent.EventError && ev.Error != nil {
				out <- signalpipe.Event{SessionID: ev.SessionID, Text: "Error: " + ev.Error.Error()}
			}
		}
	}()
	return out, unsubscribe
}

func startWebhookServer(ctx context.Context, deps *runtimeDeps, wg *sync.WaitGroup, errCh chan<- error) {

	if !deps.cfg.Webhook.Enabled {
		return
	}
	srv := webhook.New(deps.cfg.Webhook, func(source, content string, metadata map[string]string) {
		sessionID := source
		if id := metadata["id"]; id != "" {
			sessionID = source + ":" + id
		}
		deps.agent.Enqueue(agent.Input{SessionID: sessionID, Content: content, Source: agent.SourceWebhook, Metadata: metadata})
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := srv.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
			errCh <- fmt.Errorf("webhook server: %v", err)
		}
	}()
}

func messageText(msg *agent.Message) string {

	parts := make([]string, 0, len(msg.Parts))
	for _, part := range msg.Parts {
		p, ok := part.(agent.TextPart)
		if ok && p.Text != "" {
			parts = append(parts, p.Text)
		}
	}
	return strings.Join(parts, "")
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
