package executor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const boundedTemplate = `You are refining code to meet a specification.

Spec: %s
Progress: %s

Each iteration:
1. Read the spec and progress file to understand current state
2. Search the codebase before assuming anything is missing — do not reimplement existing code
3. Pick the single highest-impact remaining task
4. Implement it, keeping the change focused and testable
5. Run tests — if they fail, fix before moving on
6. Update the progress file: mark completed items, add discovered work, note what's next

The code may have been drafted by another agent. Do not trust it. Verify against the spec.

When all spec requirements are satisfied and tests pass, output:
<promise>COMPLETE</promise>
`

const openEndedTemplate = `You are improving this codebase toward production quality.

Progress: %s

Each iteration:
1. Read the progress file to understand what's been done and what remains
2. Search the codebase before assuming anything is missing
3. Pick the single highest-impact improvement
4. Implement it, keeping the change focused and testable
5. Run tests — if they fail, fix before moving on
6. Update the progress file: mark completed items, add discovered work, note what's next

Do not output a <promise> tag. Continue improving until stopped.
`

// DefaultBoundedPrompt returns the default prompt for spec-driven jobs with exit criteria.
func DefaultBoundedPrompt(specPath, progressPath string) string {
	return fmt.Sprintf(boundedTemplate, specPath, progressPath)
}

// DefaultOpenEndedPrompt returns the default prompt for open-ended polish jobs.
func DefaultOpenEndedPrompt(progressPath string) string {
	return fmt.Sprintf(openEndedTemplate, progressPath)
}

// ProgressSeedContent returns the initial content for a new progress file.
// If bounded is true, the seed references the spec; otherwise it's a general audit.
func ProgressSeedContent(bounded bool) string {
	task := "Audit codebase and identify improvements"
	if bounded {
		task = "Review spec and create initial task breakdown"
	}
	return fmt.Sprintf(`# Progress

## Remaining
- [ ] %s

## Completed

## Discovered
`, task)
}

// BootstrapProgressFile creates the progress seed file at relPath within workDir
// if it doesn't already exist. Returns true if the file was created.
func BootstrapProgressFile(workDir, relPath string, bounded bool) (bool, error) {
	fullPath := filepath.Join(workDir, relPath)
	if _, err := os.Stat(fullPath); err == nil {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return false, fmt.Errorf("create progress dir: %w", err)
	}
	if err := os.WriteFile(fullPath, []byte(ProgressSeedContent(bounded)), 0o644); err != nil {
		return false, fmt.Errorf("write progress file: %w", err)
	}
	return true, nil
}

// ProgressFilePath returns the relative path for a job's progress file given its branch name.
func ProgressFilePath(branch string) string {
	safe := strings.ReplaceAll(branch, "/", "-")
	return fmt.Sprintf("docs/plans/%s-ralph-status.md", safe)
}
