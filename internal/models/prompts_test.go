package models

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultOpenEndedPrompt(t *testing.T) {
	prompt := DefaultOpenEndedPrompt("docs/plans/my-plan.md")

	assert.Contains(t, prompt, "docs/plans/my-plan.md")
	assert.Contains(t, prompt, "production quality")
	// Sanity check: no raw %s format verb left in output
	assert.False(t, strings.Contains(prompt, "%s"))
}
