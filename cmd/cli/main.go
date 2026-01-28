package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/ryan/ralph-o-matic/internal/cli"
)

var (
	cfg    *cli.Config
	client *cli.Client
)

func main() {
	var err error
	cfg, err = cli.LoadConfig(cli.ConfigPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load config: %v\n", err)
		cfg = cli.DefaultConfig()
	}

	client = cli.NewClient(cfg.Server)

	rootCmd := &cobra.Command{
		Use:   "ralph-o-matic",
		Short: "Ralph-o-matic CLI - submit and manage ralph loop jobs",
	}

	rootCmd.AddCommand(
		submitCmd(),
		statusCmd(),
		logsCmd(),
		cancelCmd(),
		pauseCmd(),
		resumeCmd(),
		moveCmd(),
		configCmd(),
		serverConfigCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
