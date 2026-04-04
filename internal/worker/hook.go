package worker

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/ryan/ralph-o-matic/internal/models"
)

// RunPostCompletionHook executes the configured post-completion command
// with job metadata as environment variables. Returns combined stdout/stderr
// and any error. Returns ("", nil) if command is empty.
func RunPostCompletionHook(ctx context.Context, command string, job *models.Job) (string, error) {
	if command == "" {
		return "", nil
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("RALPH_JOB_ID=%d", job.ID),
		fmt.Sprintf("RALPH_REPO_URL=%s", job.RepoURL),
		fmt.Sprintf("RALPH_BRANCH=%s", job.Branch),
		fmt.Sprintf("RALPH_RESULT_BRANCH=%s", job.ResultBranch),
		fmt.Sprintf("RALPH_PR_URL=%s", job.PRURL),
		fmt.Sprintf("RALPH_WORKING_DIR=%s", job.WorkingDir),
		fmt.Sprintf("RALPH_EXIT_STATUS=%s", string(job.Status)),
	)

	output, err := cmd.CombinedOutput()
	return string(output), err
}
