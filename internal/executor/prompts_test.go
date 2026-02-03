package executor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultBoundedPrompt_ContainsSpecPath(t *testing.T) {
	prompt := DefaultBoundedPrompt("docs/plans/auth-design.md", "docs/plans/feature-auth-ralph-status.md")
	assert.Contains(t, prompt, "docs/plans/auth-design.md")
}

func TestDefaultBoundedPrompt_ContainsProgressPath(t *testing.T) {
	prompt := DefaultBoundedPrompt("docs/plans/auth-design.md", "docs/plans/feature-auth-ralph-status.md")
	assert.Contains(t, prompt, "docs/plans/feature-auth-ralph-status.md")
}

func TestDefaultBoundedPrompt_ContainsPromiseTag(t *testing.T) {
	prompt := DefaultBoundedPrompt("spec.md", "progress.md")
	assert.Contains(t, prompt, "<promise>COMPLETE</promise>")
}

func TestDefaultBoundedPrompt_ContainsGuardrails(t *testing.T) {
	prompt := DefaultBoundedPrompt("spec.md", "progress.md")
	assert.Contains(t, prompt, "Search the codebase before assuming anything is missing")
	assert.Contains(t, prompt, "single highest-impact")
	assert.Contains(t, prompt, "if they fail, fix before moving on")
}

func TestDefaultOpenEndedPrompt_ContainsProgressPath(t *testing.T) {
	prompt := DefaultOpenEndedPrompt("docs/plans/main-ralph-status.md")
	assert.Contains(t, prompt, "docs/plans/main-ralph-status.md")
}

func TestDefaultOpenEndedPrompt_NoPromiseTag(t *testing.T) {
	prompt := DefaultOpenEndedPrompt("progress.md")
	assert.NotContains(t, prompt, "<promise>COMPLETE</promise>")
	assert.Contains(t, prompt, "Do not output a <promise> tag")
}

func TestDefaultOpenEndedPrompt_ContainsGuardrails(t *testing.T) {
	prompt := DefaultOpenEndedPrompt("progress.md")
	assert.Contains(t, prompt, "Search the codebase before assuming anything is missing")
	assert.Contains(t, prompt, "single highest-impact")
	assert.Contains(t, prompt, "if they fail, fix before moving on")
}

func TestProgressSeedContent_Bounded(t *testing.T) {
	content := ProgressSeedContent(true)
	require.Contains(t, content, "## Remaining")
	require.Contains(t, content, "## Completed")
	require.Contains(t, content, "## Discovered")
	assert.Contains(t, content, "Review spec and create initial task breakdown")
}

func TestProgressSeedContent_OpenEnded(t *testing.T) {
	content := ProgressSeedContent(false)
	require.Contains(t, content, "## Remaining")
	require.Contains(t, content, "## Completed")
	require.Contains(t, content, "## Discovered")
	assert.Contains(t, content, "Audit codebase and identify improvements")
	assert.NotContains(t, content, "Review spec")
}

func TestProgressFilePath(t *testing.T) {
	path := ProgressFilePath("feature-auth")
	assert.Equal(t, "docs/plans/feature-auth-ralph-status.md", path)
}

func TestProgressFilePath_SanitizesBranch(t *testing.T) {
	path := ProgressFilePath("ralph/feature-auth-result")
	assert.Equal(t, "docs/plans/ralph-feature-auth-result-ralph-status.md", path)
}

func TestBootstrapProgressFile_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	err := os.MkdirAll(filepath.Join(dir, "docs", "plans"), 0o755)
	require.NoError(t, err)

	relPath := ProgressFilePath("my-branch")
	created, err := BootstrapProgressFile(dir, relPath, true)
	require.NoError(t, err)
	assert.True(t, created)

	content, err := os.ReadFile(filepath.Join(dir, relPath))
	require.NoError(t, err)
	assert.Contains(t, string(content), "Review spec and create initial task breakdown")
}

func TestBootstrapProgressFile_DoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	relPath := ProgressFilePath("my-branch")
	fullPath := filepath.Join(dir, relPath)

	err := os.MkdirAll(filepath.Dir(fullPath), 0o755)
	require.NoError(t, err)
	err = os.WriteFile(fullPath, []byte("existing content"), 0o644)
	require.NoError(t, err)

	created, err := BootstrapProgressFile(dir, relPath, true)
	require.NoError(t, err)
	assert.False(t, created)

	content, err := os.ReadFile(fullPath)
	require.NoError(t, err)
	assert.Equal(t, "existing content", string(content))
}

func TestBootstrapProgressFile_CreatesDirectories(t *testing.T) {
	dir := t.TempDir()
	relPath := ProgressFilePath("my-branch")

	created, err := BootstrapProgressFile(dir, relPath, false)
	require.NoError(t, err)
	assert.True(t, created)

	content, err := os.ReadFile(filepath.Join(dir, relPath))
	require.NoError(t, err)
	assert.Contains(t, string(content), "Audit codebase and identify improvements")
}
