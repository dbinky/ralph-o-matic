package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ryan/ralph-o-matic/internal/api"
	"github.com/ryan/ralph-o-matic/internal/db"
	"github.com/ryan/ralph-o-matic/internal/executor"
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
	srv := api.NewServer(database, q, addr)

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
	w := worker.New(q, handler, 5*time.Second)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := srv.Start(); err != nil {
			log.Printf("Server stopped: %v", err)
		}
	}()

	go w.Run(ctx)

	log.Printf("ralph-o-matic-server %s listening on %s", version, addr)
	<-ctx.Done()

	log.Println("Shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
