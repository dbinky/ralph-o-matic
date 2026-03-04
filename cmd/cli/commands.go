package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/ryan/ralph-o-matic/internal/cli"
	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/spf13/cobra"
)

func submitCmd() *cobra.Command {
	var prompt, priority, workingDir, backend, exitPromise string
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

			// Resolve prompt: --open-ended overrides everything, else --prompt, else RALPH.md
			promptSource := "flag"
			if openEnded {
				safeBranch := strings.ReplaceAll(branch, "/", "-")
				progressPath := fmt.Sprintf("docs/plans/%s-ralph-status.md", safeBranch)
				prompt = models.DefaultOpenEndedPrompt(progressPath)
				promptSource = "open-ended"
			} else if prompt == "" {
				prompt, err = readPromptFile(workingDir)
				if err != nil {
					return fmt.Errorf("no prompt provided and RALPH.md not found")
				}
				promptSource = "RALPH.md"
			}

			if priority == "" {
				priority = cfg.DefaultPriority
			}
			if cmd.Flags().Changed("max-iterations") && maxIterations <= 0 {
				return fmt.Errorf("--max-iterations must be a positive integer, got %d", maxIterations)
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

			if exitPromise != "" {
				req.ExitPromise = exitPromise
			}
			if backend != "" {
				req.Backend = backend
			}

			fmt.Println("Submitting job...")
			fmt.Printf("  Repository:     %s\n", repoURL)
			fmt.Printf("  Branch:         %s\n", branch)
			promptDesc := fmt.Sprintf("%s (%d chars)", promptSource, len(prompt))
			fmt.Printf("  Prompt:         %s\n", promptDesc)
			fmt.Printf("  Max iterations: %d\n", maxIterations)
			fmt.Printf("  Priority:       %s\n", priority)

			job, err := client.CreateJob(req)
			if err != nil {
				return err
			}

			fmt.Printf("\n✓ Job #%d queued (position: %d)\n", job.ID, job.Position)
			fmt.Printf("\nDashboard: %s/jobs/%d\n", cfg.Server, job.ID)
			return nil
		},
	}

	cmd.Flags().StringVar(&prompt, "prompt", "", "Prompt text (overrides RALPH.md)")
	cmd.Flags().StringVar(&priority, "priority", "", "Priority: high, normal, low")
	cmd.Flags().IntVar(&maxIterations, "max-iterations", 0, "Max iterations")
	cmd.Flags().StringVar(&workingDir, "working-dir", "", "Working directory")
	cmd.Flags().BoolVar(&openEnded, "open-ended", false, "Use open-ended prompt")
	cmd.Flags().StringVar(&exitPromise, "exit-promise", "", "Promise tag that signals completion (default: FINIT)")
	cmd.Flags().StringVar(&backend, "backend", "", "Backend to use: ollama or anthropic (default: server setting)")

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

			if !follow {
				return nil
			}

			// Check if job is already terminal before streaming
			job, err := client.GetJob(id)
			if err != nil {
				return err
			}
			if job.Status.IsTerminal() {
				return nil
			}

			// Stream via SSE — poll for new logs on each event
			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			events, err := client.StreamJobEvents(ctx, id)
			if err != nil {
				return fmt.Errorf("failed to stream events: %w", err)
			}

			lastCount := len(logs)
			for range events {
				allLogs, err := client.GetLogs(id)
				if err != nil {
					continue
				}
				for i := lastCount; i < len(allLogs); i++ {
					fmt.Printf("[iter %v] %v\n", allLogs[i]["iteration"], allLogs[i]["message"])
				}
				lastCount = len(allLogs)

				// Re-check job status to avoid hanging if the SSE
				// connection stays open after the job finishes.
				if j, err := client.GetJob(id); err == nil && j.Status.IsTerminal() {
					cancel()
					return nil
				}
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

func updateCmd() *cobra.Command {
	var priority string
	var maxIterations int

	cmd := &cobra.Command{
		Use:   "update <job-id>",
		Short: "Update properties of a queued or running job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid job ID")
			}

			updates := make(map[string]interface{})

			if cmd.Flags().Changed("priority") {
				updates["priority"] = priority
			}
			if cmd.Flags().Changed("max-iterations") {
				if maxIterations <= 0 {
					return fmt.Errorf("--max-iterations must be a positive integer, got %d", maxIterations)
				}
				updates["max_iterations"] = maxIterations
			}

			if len(updates) == 0 {
				return fmt.Errorf("no updates specified; use --priority or --max-iterations")
			}

			job, err := client.UpdateJob(id, updates)
			if err != nil {
				return err
			}

			fmt.Printf("Job #%d updated\n", job.ID)
			if cmd.Flags().Changed("priority") {
				fmt.Printf("  Priority:       %s\n", prioritySymbol(job.Priority))
			}
			if cmd.Flags().Changed("max-iterations") {
				fmt.Printf("  Max iterations: %d\n", job.MaxIterations)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&priority, "priority", "", "Priority: high, normal, low")
	cmd.Flags().IntVar(&maxIterations, "max-iterations", 0, "Max iterations")
	return cmd
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
			case after > 0:
				insertAt := -1
				for i, jid := range newOrder {
					if jid == after {
						insertAt = i + 1
						break
					}
				}
				if insertAt < 0 {
					return fmt.Errorf("target job #%d not found in queue", after)
				}
				newOrder = append(newOrder[:insertAt], append([]int64{id}, newOrder[insertAt:]...)...)
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

			if args[0] != "set" {
				return fmt.Errorf("unknown subcommand %q\n\nUsage:\n  ralph-o-matic config              Show current configuration\n  ralph-o-matic config set <key> <value>  Set a configuration value\n\nKeys: server, default_priority, default_max_iterations", args[0])
			}

			if len(args) < 3 {
				return fmt.Errorf("missing arguments: expected 'config set <key> <value>'\n\nKeys: server, default_priority, default_max_iterations")
			}

			key := args[1]
			value := args[2]

			switch key {
			case "server":
				cfg.Server = value
			case "default_priority":
				cfg.DefaultPriority = value
			case "default_max_iterations":
				v, err := strconv.Atoi(value)
				if err != nil {
					return fmt.Errorf("invalid value for default_max_iterations: %q (must be an integer)", value)
				}
				cfg.DefaultMaxIterations = v
			default:
				return fmt.Errorf("unknown config key: %s\n\nKeys: server, default_priority, default_max_iterations", key)
			}

			return cli.SaveConfig(cli.ConfigPath(), cfg)
		},
	}
	return cmd
}

func testNotifyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "test-notify <smtp|teams>",
		Short: "Send a test notification to verify configuration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			channel := args[0]
			if channel != "smtp" && channel != "teams" {
				return fmt.Errorf("channel must be 'smtp' or 'teams'")
			}

			fmt.Printf("Sending test %s notification...\n", channel)

			resp, err := client.TestNotify(channel)
			if err != nil {
				return err
			}

			if resp.Success {
				fmt.Println(resp.Message)
			} else {
				return fmt.Errorf("%s", resp.Error)
			}
			return nil
		},
	}
}

func serverConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server-config [set <key> <value>]",
		Short: "Show or set server configuration",
		Long: `Show or set server configuration.

When using 'set', values are auto-detected as int, float, bool, or string
(in that order). For example: "1" becomes an integer, "1.5" a float,
"true"/"false" a boolean, and anything else stays a string. The server
validates the resulting type against the config schema.

Use dotted keys for nested values, e.g.: large_model.name, notify.smtp.host`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				serverCfg, err := client.GetConfig()
				if err != nil {
					return err
				}

				fmt.Printf("default_backend: %s\n", serverCfg.DefaultBackend)
				fmt.Println()
				fmt.Println("# Ollama")
				fmt.Printf("ollama.host: %s\n", serverCfg.Ollama.Host)
				fmt.Printf("ollama.is_remote: %v\n", serverCfg.Ollama.IsRemote)
				fmt.Printf("large_model.name: %s\n", serverCfg.LargeModel.Name)
				fmt.Printf("large_model.device: %s\n", serverCfg.LargeModel.Device)
				fmt.Printf("large_model.memory_gb: %.1f\n", serverCfg.LargeModel.MemoryGB)
				fmt.Printf("small_model.name: %s\n", serverCfg.SmallModel.Name)
				fmt.Printf("small_model.device: %s\n", serverCfg.SmallModel.Device)
				fmt.Printf("small_model.memory_gb: %.1f\n", serverCfg.SmallModel.MemoryGB)
				fmt.Println()
				fmt.Println("# Anthropic")
				fmt.Printf("anthropic.large_model: %s\n", serverCfg.Anthropic.LargeModel)
				fmt.Printf("anthropic.small_model: %s\n", serverCfg.Anthropic.SmallModel)
				fmt.Println()
				fmt.Println("# Execution")
				fmt.Printf("default_max_iterations: %d\n", serverCfg.DefaultMaxIterations)
				fmt.Printf("max_claude_retries: %d\n", serverCfg.MaxClaudeRetries)
				fmt.Printf("max_git_retries: %d\n", serverCfg.MaxGitRetries)
				fmt.Printf("git_retry_backoff_ms: %d\n", serverCfg.GitRetryBackoffMs)
				fmt.Println()
				fmt.Println("# Storage")
				if serverCfg.WorkspaceDir != "" {
					fmt.Printf("workspace_dir: %s\n", serverCfg.WorkspaceDir)
				} else {
					fmt.Printf("workspace_dir: (default)\n")
				}
				fmt.Printf("job_retention_days: %d\n", serverCfg.JobRetentionDays)
				fmt.Println()
				fmt.Println("# Notifications")
				fmt.Printf("notify.smtp.enabled: %v\n", serverCfg.Notify.SMTP.Enabled)
				if serverCfg.Notify.SMTP.Enabled {
					fmt.Printf("notify.smtp.host: %s\n", serverCfg.Notify.SMTP.Host)
					fmt.Printf("notify.smtp.port: %d\n", serverCfg.Notify.SMTP.Port)
					fmt.Printf("notify.smtp.from: %s\n", serverCfg.Notify.SMTP.From)
				}
				fmt.Printf("notify.teams.enabled: %v\n", serverCfg.Notify.Teams.Enabled)

				return nil
			}

			if args[0] != "set" {
				return fmt.Errorf("unknown subcommand %q\n\nUsage:\n  ralph-o-matic server-config                    Show server configuration\n  ralph-o-matic server-config set <key> <value>   Set a server configuration value\n\nUse dotted keys for nested values, e.g.: large_model.name, notify.smtp.host", args[0])
			}

			if len(args) < 3 {
				return fmt.Errorf("missing arguments: expected 'server-config set <key> <value>'\n\nUse dotted keys for nested values, e.g.: large_model.name, notify.smtp.host")
			}

			// Parse value to appropriate JSON type (int, float, bool, or string)
			var value interface{} = args[2]
			if v, err := strconv.Atoi(args[2]); err == nil {
				value = v
			} else if v, err := strconv.ParseFloat(args[2], 64); err == nil {
				value = v
			} else if v, err := strconv.ParseBool(args[2]); err == nil {
				value = v
			}

			updates := buildNestedMap(args[1], value)

			_, err := client.UpdateConfig(updates)
			if err != nil {
				return err
			}
			fmt.Printf("Set %s = %v\n", args[1], args[2])
			return nil
		},
	}
	return cmd
}

// Helper functions

// buildNestedMap converts a dotted key like "large_model.name" and a value
// into a nested map: {"large_model": {"name": value}}.
// Keys without dots produce a flat single-key map.
func buildNestedMap(dottedKey string, value interface{}) map[string]interface{} {
	parts := strings.Split(dottedKey, ".")
	if len(parts) == 1 {
		return map[string]interface{}{dottedKey: value}
	}

	// Build inside-out: start with the leaf value and wrap in parent maps
	result := map[string]interface{}{parts[len(parts)-1]: value}
	for i := len(parts) - 2; i >= 0; i-- {
		result = map[string]interface{}{parts[i]: result}
	}
	return result
}

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
	fmt.Println("═══════════════════")

	// Group by status
	var running, paused, queued []*models.Job
	for _, j := range jobs {
		switch j.Status {
		case models.StatusRunning:
			running = append(running, j)
		case models.StatusPaused:
			paused = append(paused, j)
		case models.StatusQueued:
			queued = append(queued, j)
		}
	}

	if len(running) > 0 {
		fmt.Println("\n▶ RUNNING")
		for _, j := range running {
			pct := int(j.Progress() * 100)
			bar := progressBar(j.Progress(), 10)
			elapsed := formatDuration(j.Duration())
			fmt.Printf("  #%-4d %-30s iter %d/%d    %s %d%%    %s\n",
				j.ID, j.Branch, j.Iteration, j.MaxIterations, bar, pct, elapsed)
		}
	}

	if len(paused) > 0 {
		fmt.Println("\n⏸ PAUSED")
		for _, j := range paused {
			pausedAgo := ""
			if j.PausedAt != nil {
				pausedAgo = fmt.Sprintf("paused %s ago", formatDuration(time.Since(*j.PausedAt)))
			}
			fmt.Printf("  #%-4d %-30s iter %d/%d                      %s\n",
				j.ID, j.Branch, j.Iteration, j.MaxIterations, pausedAgo)
		}
	}

	if len(queued) > 0 {
		fmt.Printf("\n⏳ QUEUED (%d)\n", len(queued))
		for _, j := range queued {
			fmt.Printf("  #%-4d %-30s %s\n", j.ID, j.Branch, prioritySymbol(j.Priority))
		}
	}

	// Show today's completed/failed jobs
	var completedToday []*models.Job
	today := time.Now().Truncate(24 * time.Hour)
	for _, j := range jobs {
		switch j.Status {
		case models.StatusCompleted, models.StatusFailed, models.StatusCancelled:
			if j.CompletedAt != nil && j.CompletedAt.After(today) {
				completedToday = append(completedToday, j)
			}
		}
	}

	if len(completedToday) > 0 {
		fmt.Println("\nTODAY")
		for _, j := range completedToday {
			var symbol string
			switch j.Status {
			case models.StatusFailed:
				symbol = "✗"
			case models.StatusCancelled:
				symbol = "⊘"
			default:
				symbol = "✓"
			}
			line := fmt.Sprintf("  %s #%-4d %-30s %d iters", symbol, j.ID, j.Branch, j.Iteration)
			if d := j.Duration(); d > 0 {
				line += fmt.Sprintf("    %s", formatDuration(d))
			}
			if j.PRURL != "" {
				line += fmt.Sprintf("    %s", j.PRURL)
			}
			fmt.Println(line)
		}
	}

	fmt.Printf("\nDashboard: %s\n", cfg.Server)
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}

// progressBar renders a text progress bar, e.g. "████████░░" for 80% with width 10.
func progressBar(fraction float64, width int) string {
	if fraction < 0 {
		fraction = 0
	}
	if fraction > 1 {
		fraction = 1
	}
	filled := int(fraction * float64(width))
	empty := width - filled
	return strings.Repeat("█", filled) + strings.Repeat("░", empty)
}

// statusSymbol returns a Unicode symbol for the job status.
func statusSymbol(s models.JobStatus) string {
	switch s {
	case models.StatusRunning:
		return "▶"
	case models.StatusPaused:
		return "⏸"
	case models.StatusQueued:
		return "⏳"
	case models.StatusCompleted:
		return "✓"
	case models.StatusFailed:
		return "✗"
	case models.StatusCancelled:
		return "⊘"
	default:
		return "?"
	}
}

// prioritySymbol returns a symbol + label for display.
func prioritySymbol(p models.Priority) string {
	switch p {
	case models.PriorityHigh:
		return "⚡ high"
	case models.PriorityLow:
		return "○ low"
	default:
		return "• normal"
	}
}

func printJobDetail(job *models.Job) {
	fmt.Printf("Job #%d  %s %s\n", job.ID, statusSymbol(job.Status), strings.ToUpper(string(job.Status)))
	fmt.Printf("  Repository:  %s\n", job.RepoURL)
	fmt.Printf("  Branch:      %s\n", job.Branch)
	if job.ResultBranch != "" {
		fmt.Printf("  Result:      %s\n", job.ResultBranch)
	}
	if job.WorkingDir != "" {
		fmt.Printf("  Working dir: %s\n", job.WorkingDir)
	}
	fmt.Printf("  Priority:    %s\n", prioritySymbol(job.Priority))
	if job.Backend != "" {
		fmt.Printf("  Backend:     %s\n", job.Backend)
	}
	fmt.Printf("  Iteration:   %d/%d\n", job.Iteration, job.MaxIterations)

	if job.Status == models.StatusRunning {
		pct := int(job.Progress() * 100)
		fmt.Printf("  Progress:    %s %d%%\n", progressBar(job.Progress(), 20), pct)
	}

	if d := job.Duration(); d > 0 {
		label := "Duration:"
		if job.Status == models.StatusRunning {
			label = "Elapsed:"
		}
		fmt.Printf("  %-12s  %s\n", label, formatDuration(d))
	}

	if job.Status == models.StatusPaused && job.PausedAt != nil {
		fmt.Printf("  Paused:      %s ago\n", formatDuration(time.Since(*job.PausedAt)))
	}

	fmt.Printf("  Created:     %s\n", job.CreatedAt.Format("2006-01-02 15:04"))
	if job.StartedAt != nil {
		fmt.Printf("  Started:     %s\n", job.StartedAt.Format("2006-01-02 15:04"))
	}
	if job.CompletedAt != nil {
		fmt.Printf("  Completed:   %s\n", job.CompletedAt.Format("2006-01-02 15:04"))
	}

	if job.PRURL != "" {
		fmt.Printf("  PR:          %s\n", job.PRURL)
	}
	if job.Error != "" {
		fmt.Printf("  Error:       %s\n", job.Error)
	}
}
