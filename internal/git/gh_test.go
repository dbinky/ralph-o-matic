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

func TestBuildPRTitle_Success(t *testing.T) {
	title := BuildPRTitle("feature/auth", true)
	assert.Contains(t, title, "feature/auth")
	assert.Contains(t, title, "✓")
	assert.NotContains(t, title, "FAILED")
}

func TestBuildPRTitle_Failed(t *testing.T) {
	title := BuildPRTitle("bugfix/login", false)
	assert.Contains(t, title, "bugfix/login")
	assert.Contains(t, title, "FAILED")
	assert.Contains(t, title, "✗")
}

func TestBuildPRBody_NoSpecPath(t *testing.T) {
	body := BuildPRBody(5, true, "", nil)
	assert.Contains(t, body, "5 iterations")
	assert.NotContains(t, body, "Specification")
	assert.Contains(t, body, "Ralph-o-matic")
}

func TestBuildPRBody_FailedNoDetails(t *testing.T) {
	body := BuildPRBody(10, false, "", nil)
	assert.Contains(t, body, "Manual intervention")
	assert.NotContains(t, body, "Current State")
}
