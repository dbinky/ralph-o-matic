package main

import (
	"fmt"
	"os"

	"github.com/ryan/ralph-o-matic/internal/cli"
	"github.com/spf13/cobra"
)

// version is set via -ldflags at build time.
var version = "dev"

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
		Use:     "ralph-o-matic",
		Short:   "Ralph-o-matic CLI - submit and manage ralph loop jobs",
		Version: version,
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
		testNotifyCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
