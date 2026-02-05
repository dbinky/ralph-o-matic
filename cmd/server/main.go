package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"log/slog"

	"github.com/ryan/ralph-o-matic/internal/api"
	"github.com/ryan/ralph-o-matic/internal/auth"
	"github.com/ryan/ralph-o-matic/internal/broadcast"
	"github.com/ryan/ralph-o-matic/internal/db"
	"github.com/ryan/ralph-o-matic/internal/executor"
	"github.com/ryan/ralph-o-matic/internal/notify"
	"github.com/ryan/ralph-o-matic/internal/queue"
	"github.com/ryan/ralph-o-matic/internal/worker"
)

// version is set via -ldflags at build time.
var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Printf("ralph-o-matic-server %s\n", version)
		os.Exit(0)
	}

	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	addr := ":9090"
	if v := os.Getenv("RALPH_ADDR"); v != "" {
		addr = v
	}

	dbPath := "ralph.db"
	if v := os.Getenv("RALPH_DB"); v != "" {
		dbPath = v
	}

	database, err := db.New(dbPath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer database.Close()

	if err := database.Migrate(); err != nil {
		return fmt.Errorf("failed to migrate database: %w", err)
	}

	q := queue.New(database)

	b := broadcast.New()
	q.SetBroadcaster(b)

	// Recover jobs orphaned by a previous server crash/restart
	recovered, err := q.RecoverOrphaned()
	if err != nil {
		return fmt.Errorf("failed to recover orphaned jobs: %w", err)
	}
	if recovered > 0 {
		slog.Info("recovered orphaned jobs", "count", recovered)
	}

	// Load auth configuration
	authCfg, err := auth.LoadConfig(os.Getenv, "")
	if err != nil {
		return fmt.Errorf("failed to load auth config: %w", err)
	}
	if err := authCfg.Validate(); err != nil {
		return fmt.Errorf("invalid auth config: %w", err)
	}

	var serverOpts *api.ServerOptions
	if authCfg.Mode == auth.AuthModeEntra {
		provider, err := auth.NewEntraProvider(context.Background(), authCfg.Entra, "")
		if err != nil {
			return fmt.Errorf("failed to initialize EntraID provider: %w", err)
		}
		secure := os.Getenv("RALPH_SECURE") == "true"
		if os.Getenv("RALPH_SECURE") == "" {
			log.Println("WARNING: RALPH_SECURE not set — session cookies will not have the Secure flag. Set RALPH_SECURE=true for HTTPS deployments.")
		}
		serverOpts = &api.ServerOptions{
			AuthProvider: provider,
			Sessions:     auth.NewSessionStore(30 * time.Minute),
			Secure:       secure,
		}
		log.Printf("Authentication enabled: EntraID SSO (tenant: %s)", authCfg.Entra.TenantID)
	} else {
		log.Println("WARNING: running without authentication — all endpoints are open")
	}

	if serverOpts == nil {
		serverOpts = &api.ServerOptions{}
	}
	serverOpts.Broadcaster = b

	srv := api.NewServer(database, q, addr, serverOpts)

	// Load config for executor
	configRepo := db.NewConfigRepo(database)
	config, err := configRepo.Get()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	workspaceDir := config.WorkspaceDir
	if workspaceDir == "" {
		workspaceDir = "workspaces"
	}

	handler := executor.NewRalphHandler(database, config, workspaceDir)
	handler.SetLogBroadcaster(b)
	w := worker.New(q, handler, 5*time.Second)

	// Set up notification dispatcher (reads config per-call from DB)
	dispatcher := notify.NewDispatcher(configRepo, slog.Default())
	w.SetNotifier(dispatcher)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := srv.Start(); err != nil {
			log.Printf("Server stopped: %v", err)
		}
	}()

	// Use WaitGroup to ensure worker completes its current iteration before shutdown
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		w.Run(ctx)
	}()

	log.Printf("ralph-o-matic-server %s listening on %s", version, addr)
	<-ctx.Done()

	log.Println("Shutting down...")

	// Wait for worker to complete current iteration (with timeout)
	workerDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(workerDone)
	}()

	select {
	case <-workerDone:
		log.Println("Worker shutdown complete")
	case <-time.After(30 * time.Second):
		log.Println("Warning: worker shutdown timed out after 30s")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
