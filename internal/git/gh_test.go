package git

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGH_IsInstalled(t *testing.T) {
	gh := NewGH()
	// This may fail if gh is not installed, which is fine for unit tests
	_ = gh.IsInstalled()
}

func TestGH_IsAuthenticated(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gh := NewGH()
	if !gh.IsInstalled() {
		t.Skip("gh not installed")
	}

	// Just verify it doesn't panic
	_ = gh.IsAuthenticated()
}

func TestGH_BuildPRBody(t *testing.T) {
	body := BuildPRBody(8, true, "docs/plans/design.md", nil)

	assert.Contains(t, body, "8 iterations")
	assert.Contains(t, body, "docs/plans/design.md")
	assert.Contains(t, body, "Completed")
}

func TestGH_BuildPRBody_Failed(t *testing.T) {
	body := BuildPRBody(50, false, "docs/plans/design.md", map[string]string{
		"remaining_issues": "3 tests failing",
	})

	assert.Contains(t, body, "50")
	assert.Contains(t, body, "without completing")
	assert.Contains(t, body, "3 tests failing")
}
