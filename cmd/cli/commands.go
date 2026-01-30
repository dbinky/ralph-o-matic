package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/ryan/ralph-o-matic/internal/cli"
	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/spf13/cobra"
)

func submitCmd() *cobra.Command {
	var prompt, priority, workingDir string
	var maxIterations int
	var openEnded bool

	cmd := &cobra.Command{
		Use:   "submit",
		Short: "Submit a new job to the queue",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Get repo info from git
			repoURL, branch, err := getGitInfo()
			if err != nil {
				return fmt.Errorf("failed to get git info: %w", err)
			}

			// Resolve prompt
			if prompt == "" {
				prompt, err = readPromptFile(workingDir)
				if err != nil {
					return fmt.Errorf("no prompt provided and RALPH.md not found")
				}
			}

			if priority == "" {
				priority = cfg.DefaultPriority
			}
			if maxIterations == 0 {
				maxIterations = cfg.DefaultMaxIterations
			}

			req := &cli.CreateJobRequest{
				RepoURL:       repoURL,
				Branch:        branch,
				Prompt:        prompt,
				MaxIterations: maxIterations,
				Priority:      priority,
				WorkingDir:    workingDir,
			}

			fmt.Println("Submitting job...")
			fmt.Printf("  Repository:    %s\n", repoURL)
			fmt.Printf("  Branch:        %s\n", branch)
			fmt.Printf("  Max iterations: %d\n", maxIterations)
			fmt.Printf("  Priority:      %s\n", priority)

			job, err := client.CreateJob(req)
			if err != nil {
				return err
			}

			fmt.Printf("\nJob #%d queued (position: %d)\n", job.ID, job.Position)
			fmt.Printf("\nDashboard: %s/jobs/%d\n", cfg.Server, job.ID)
			return nil
		},
	}

	cmd.Flags().StringVar(&prompt, "prompt", "", "Prompt text (overrides RALPH.md)")
	cmd.Flags().StringVar(&priority, "priority", "", "Priority: high, normal, low")
	cmd.Flags().IntVar(&maxIterations, "max-iterations", 0, "Max iterations")
	cmd.Flags().StringVar(&workingDir, "working-dir", "", "Working directory")
	cmd.Flags().BoolVar(&openEnded, "open-ended", false, "Use open-ended prompt")

	return cmd
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status [job-id]",
		Short: "Show queue status or specific job details",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				// Show specific job
				id, err := strconv.ParseInt(args[0], 10, 64)
				if err != nil {
					return fmt.Errorf("invalid job ID")
				}

				job, err := client.GetJob(id)
				if err != nil {
					return err
				}

				printJobDetail(job)
				return nil
			}

			// Show queue overview
			jobs, _, err := client.GetJobs(nil)
			if err != nil {
				return err
			}

			printQueueOverview(jobs)
			return nil
		},
	}
}

func logsCmd() *cobra.Command {
	var follow bool

	cmd := &cobra.Command{
		Use:   "logs <job-id>",
		Short: "View job logs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid job ID")
			}

			logs, err := client.GetLogs(id)
			if err != nil {
				return err
			}

			for _, log := range logs {
				fmt.Printf("[iter %v] %v\n", log["iteration"], log["message"])
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Stream logs in real-time")
	return cmd
}

func cancelCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <job-id>",
		Short: "Cancel a job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid job ID")
			}

			job, err := client.CancelJob(id)
			if err != nil {
				return err
			}

			fmt.Printf("Job #%d cancelled\n", job.ID)
			return nil
		},
	}
}

func pauseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pause <job-id>",
		Short: "Pause a running job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid job ID")
			}

			job, err := client.PauseJob(id)
			if err != nil {
				return err
			}

			fmt.Printf("Job #%d paused at iteration %d\n", job.ID, job.Iteration)
			return nil
		},
	}
}

func resumeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resume <job-id>",
		Short: "Resume a paused job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid job ID")
			}

			job, err := client.ResumeJob(id)
			if err != nil {
				return err
			}

			fmt.Printf("Job #%d resumed\n", job.ID)
			return nil
		},
	}
}

func moveCmd() *cobra.Command {
	var position int
	var after int64
	var first bool

	cmd := &cobra.Command{
		Use:   "move <job-id>",
		Short: "Move job in queue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid job ID")
			}

			// Get current queue
			jobs, _, err := client.GetJobs([]string{"queued"})
			if err != nil {
				return err
			}

			// Build new order
			var newOrder []int64
			for _, j := range jobs {
				if j.ID != id {
					newOrder = append(newOrder, j.ID)
				}
			}

			// Insert at new position
			switch {
			case first:
				newOrder = append([]int64{id}, newOrder...)
			case position > 0:
				pos := position - 1
				if pos > len(newOrder) {
					pos = len(newOrder)
				}
				newOrder = append(newOrder[:pos], append([]int64{id}, newOrder[pos:]...)...)
			default:
				newOrder = append(newOrder, id)
			}

			if err := client.ReorderJobs(newOrder); err != nil {
				return err
			}

			fmt.Printf("Job #%d moved\n", id)
			return nil
		},
	}

	cmd.Flags().IntVar(&position, "position", 0, "Move to specific position")
	cmd.Flags().Int64Var(&after, "after", 0, "Move after another job")
	cmd.Flags().BoolVar(&first, "first", false, "Move to front of queue")
	return cmd
}

func configCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config [set <key> <value>]",
		Short: "Show or set CLI configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				fmt.Printf("server: %s\n", cfg.Server)
				fmt.Printf("default_priority: %s\n", cfg.DefaultPriority)
				fmt.Printf("default_max_iterations: %d\n", cfg.DefaultMaxIterations)
				return nil
			}

			if len(args) >= 3 && args[0] == "set" {
				key := args[1]
				value := args[2]

				switch key {
				case "server":
					cfg.Server = value
				case "default_priority":
					cfg.DefaultPriority = value
				case "default_max_iterations":
					v, _ := strconv.Atoi(value)
					cfg.DefaultMaxIterations = v
				default:
					return fmt.Errorf("unknown config key: %s", key)
				}

				return cli.SaveConfig(cli.ConfigPath(), cfg)
			}

			return nil
		},
	}
	return cmd
}

func serverConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server-config [set <key> <value>]",
		Short: "Show or set server configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				serverCfg, err := client.GetConfig()
				if err != nil {
					return err
				}

				fmt.Printf("ollama_host: %s (remote: %v)\n", serverCfg.Ollama.Host, serverCfg.Ollama.IsRemote)
				fmt.Printf("large_model: %s (device: %s, %.1fGB)\n", serverCfg.LargeModel.Name, serverCfg.LargeModel.Device, serverCfg.LargeModel.MemoryGB)
				fmt.Printf("small_model: %s (device: %s, %.1fGB)\n", serverCfg.SmallModel.Name, serverCfg.SmallModel.Device, serverCfg.SmallModel.MemoryGB)
				fmt.Printf("default_max_iterations: %d\n", serverCfg.DefaultMaxIterations)
				fmt.Printf("concurrent_jobs: %d\n", serverCfg.ConcurrentJobs)
				return nil
			}

			if len(args) >= 3 && args[0] == "set" {
				updates := map[string]interface{}{
					args[1]: args[2],
				}

				_, err := client.UpdateConfig(updates)
				return err
			}

			return nil
		},
	}
	return cmd
}

// Helper functions

func getGitInfo() (string, string, error) {
	repoURL, err := execGit("remote", "get-url", "origin")
	if err != nil {
		return "", "", fmt.Errorf("get remote URL: %w", err)
	}
	branch, err := execGit("branch", "--show-current")
	if err != nil {
		return "", "", fmt.Errorf("get branch: %w", err)
	}
	return strings.TrimSpace(repoURL), strings.TrimSpace(branch), nil
}

func execGit(args ...string) (string, error) {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func readPromptFile(workingDir string) (string, error) {
	paths := []string{
		"RALPH.md",
	}
	if workingDir != "" {
		paths = append([]string{workingDir + "/RALPH.md"}, paths...)
	}

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err == nil {
			return string(data), nil
		}
	}

	return "", fmt.Errorf("RALPH.md not found")
}

func printQueueOverview(jobs []*models.Job) {
	fmt.Println("Ralph-o-matic Queue")
	fmt.Println(strings.Repeat("=", 40))

	// Group by status
	var running, paused, queued []*models.Job
	for _, j := range jobs {
		switch j.Status {
		case "running":
			running = append(running, j)
		case "paused":
			paused = append(paused, j)
		case "queued":
			queued = append(queued, j)
		}
	}

	if len(running) > 0 {
		fmt.Println("\nRUNNING")
		for _, j := range running {
			fmt.Printf("  #%d %s    iter %d/%d\n", j.ID, j.Branch, j.Iteration, j.MaxIterations)
		}
	}

	if len(paused) > 0 {
		fmt.Println("\nPAUSED")
		for _, j := range paused {
			fmt.Printf("  #%d %s    iter %d/%d\n", j.ID, j.Branch, j.Iteration, j.MaxIterations)
		}
	}

	if len(queued) > 0 {
		fmt.Printf("\nQUEUED (%d)\n", len(queued))
		for _, j := range queued {
			fmt.Printf("  #%d %s    %s\n", j.ID, j.Branch, j.Priority)
		}
	}

	fmt.Printf("\nDashboard: %s\n", cfg.Server)
}

func printJobDetail(job *models.Job) {
	fmt.Printf("Job #%d\n", job.ID)
	fmt.Printf("  Branch:     %s\n", job.Branch)
	fmt.Printf("  Status:     %s\n", job.Status)
	fmt.Printf("  Iteration:  %d/%d\n", job.Iteration, job.MaxIterations)
	fmt.Printf("  Priority:   %s\n", job.Priority)
	if job.PRURL != "" {
		fmt.Printf("  PR:         %s\n", job.PRURL)
	}
}
