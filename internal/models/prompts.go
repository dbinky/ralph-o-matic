package models

import "fmt"

// OpenEndedTemplate is the prompt template for open-ended polish jobs.
// It is defined in models so both the CLI and executor can use it
// without pulling in server-side dependencies.
const OpenEndedTemplate = `You are improving this codebase toward production quality.

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

// DefaultOpenEndedPrompt returns the formatted open-ended prompt for the given progress path.
func DefaultOpenEndedPrompt(progressPath string) string {
	return fmt.Sprintf(OpenEndedTemplate, progressPath)
}
