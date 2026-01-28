package git

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRepoManager_WorkspacePath(t *testing.T) {
	rm := NewRepoManager("/workspace")

	path := rm.WorkspacePath(42)
	assert.Equal(t, "/workspace/job-42", path)
}

func TestRepoManager_ResultBranch(t *testing.T) {
	rm := NewRepoManager("/workspace")

	result := rm.ResultBranch("feature/auth")
	assert.Equal(t, "ralph/feature/auth-result", result)
}
