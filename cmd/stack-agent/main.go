package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/b0rked-dev/stack-agent/internal/agent"
	"github.com/b0rked-dev/stack-agent/internal/compose"
	"github.com/b0rked-dev/stack-agent/internal/config"
	"github.com/b0rked-dev/stack-agent/internal/git"
	"github.com/b0rked-dev/stack-agent/internal/metrics"
	"github.com/b0rked-dev/stack-agent/internal/server"
	"github.com/b0rked-dev/stack-agent/internal/state"
)

// Version is set at build time via -ldflags "-X main.Version=<tag>".
// It falls back to "dev" for local builds.
var Version = "dev"

// findConfigFile returns the path to the first config file found in dir,
// checking config.yaml then config.yml. Falls back to config.yaml if neither exists
// so the error message is meaningful.
func findConfigFile(dir string) string {
	for _, name := range []string{"config.yaml", "config.yml"} {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return filepath.Join(dir, "config.yaml")
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	startTime := time.Now()

	fs := flag.NewFlagSet("stack-agent", flag.ContinueOnError)
	fs.SetOutput(stderr)

	defaultConfig := findConfigFile("/opt/stack-agent")
	if envPath := os.Getenv("STACK_AGENT_CONFIG"); envPath != "" {
		defaultConfig = envPath
	}

	configPath := fs.String("config", defaultConfig, "path to config file")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	// Configure slog based on STACK_AGENT_LOG_LEVEL.
	level := slog.LevelInfo
	if lvlStr := os.Getenv("STACK_AGENT_LOG_LEVEL"); lvlStr != "" {
		switch strings.ToLower(lvlStr) {
		case "debug":
			level = slog.LevelDebug
		case "info":
			level = slog.LevelInfo
		case "warn":
			level = slog.LevelWarn
		case "error":
			level = slog.LevelError
		}
	}

	logger := slog.New(slog.NewJSONHandler(stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	// Load config.
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(stderr, "stack-agent: failed to load config %q: %v\n", *configPath, err)
		return 1
	}

	httpAddr := os.Getenv("STACK_AGENT_HTTP_ADDR")
	if httpAddr == "" {
		httpAddr = ":2112"
	}

	stackNames := make([]string, len(cfg.Stacks))
	for i, sc := range cfg.Stacks {
		stackNames[i] = sc.Name
	}
	slog.Info("stack-agent starting",
		"version", Version,
		"config", *configPath,
		"stacks", len(cfg.Stacks),
		"stack_names", stackNames,
		"http_addr", httpAddr,
	)

	if len(cfg.Stacks) == 0 {
		slog.Warn("no stacks configured — waiting for shutdown signal")
	}

	// Initialize metrics recorder.
	rec := metrics.NewPrometheusRecorder()

	// Initialize shared dependencies.
	gitClient := git.New()
	composeRunner := compose.NewDockerRunner()

	workDir := "/opt/stack-agent/data"
	if len(cfg.Stacks) > 0 {
		workDir = cfg.Stacks[0].WorkDir
	}
	statePath := filepath.Join(workDir, ".state.json")
	stateStore, err := state.NewFileStore(statePath)
	if err != nil {
		fmt.Fprintf(stderr, "stack-agent: failed to initialize state store: %v\n", err)
		return 1
	}

	// Set up signal-aware context.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// Start HTTP server in background.
	go func() {
		if err := server.New(httpAddr, Version, startTime, rec.Registry()).Run(ctx); err != nil {
			slog.Error("server exited", "err", err)
		}
	}()

	// Spawn one goroutine per stack.
	var wg sync.WaitGroup
	for _, stackCfg := range cfg.Stacks {
		wg.Add(1)
		go func(sc config.StackConfig) {
			defer wg.Done()
			agent.NewStack(sc, gitClient, composeRunner, stateStore, rec).Run(ctx)
		}(stackCfg)
	}

	// Block until signal.
	<-ctx.Done()
	stop()

	// Wait for all goroutines to finish.
	wg.Wait()
	slog.Info("shutdown complete")
	return 0
}
