package executor

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func loadTestData(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	require.NoError(t, err)
	return data
}

// --- Happy Path ---

func TestParseResponse_SuccessWithRalphStatus(t *testing.T) {
	data := loadTestData(t, "success_result.json")
	meta, err := ParseResponse(data)

	require.NoError(t, err)
	assert.Equal(t, "111b2763-1e88-4c07-87b5-465c01876e9a", meta.SessionID)
	assert.True(t, meta.Completed)
	assert.True(t, meta.ExitSignal)
	assert.Equal(t, 5, meta.FilesModified)
	assert.False(t, meta.HasErrors)
	assert.False(t, meta.IsTestOnly)
}

func TestParseResponse_InProgress(t *testing.T) {
	data := loadTestData(t, "in_progress_result.json")
	meta, err := ParseResponse(data)

	require.NoError(t, err)
	assert.Equal(t, "222b2763-1e88-4c07-87b5-465c01876e9a", meta.SessionID)
	assert.False(t, meta.Completed)
	assert.False(t, meta.ExitSignal)
	assert.False(t, meta.HasErrors)
}

func TestParseResponse_NoSessionID(t *testing.T) {
	data := loadTestData(t, "no_session.json")
	meta, err := ParseResponse(data)

	require.NoError(t, err)
	assert.Empty(t, meta.SessionID)
	assert.False(t, meta.Completed)
}

// --- Completion Detection ---

func TestParseResponse_CompletionKeywords_NotDetected(t *testing.T) {
	// Keywords like "all tasks are complete" should NOT trigger completion
	// by themselves. Completion requires RALPH_STATUS block or <promise> tags.
	data := loadTestData(t, "completion_keywords.json")
	meta, err := ParseResponse(data)

	require.NoError(t, err)
	assert.False(t, meta.Completed, "keywords alone should not trigger completion")
}

func TestParseResponse_NoCompletionSignals(t *testing.T) {
	data := loadTestData(t, "in_progress_result.json")
	meta, err := ParseResponse(data)

	require.NoError(t, err)
	assert.False(t, meta.Completed)
	assert.False(t, meta.ExitSignal)
}

// --- Error Detection ---

func TestParseResponse_WithErrors(t *testing.T) {
	data := loadTestData(t, "error_result.json")
	meta, err := ParseResponse(data)

	require.NoError(t, err)
	assert.True(t, meta.HasErrors)
	assert.True(t, meta.IsError)
	assert.NotEmpty(t, meta.ErrorMessages)
}

func TestParseResponse_NoErrors(t *testing.T) {
	data := loadTestData(t, "in_progress_result.json")
	meta, err := ParseResponse(data)

	require.NoError(t, err)
	assert.False(t, meta.HasErrors)
	assert.Empty(t, meta.ErrorMessages)
}

// --- Test-Only Detection ---

func TestParseResponse_TestOnly(t *testing.T) {
	data := loadTestData(t, "test_only_result.json")
	meta, err := ParseResponse(data)

	require.NoError(t, err)
	assert.True(t, meta.IsTestOnly, "should detect test-only loop")
}

func TestParseResponse_ImplementationWithTests(t *testing.T) {
	data := loadTestData(t, "success_result.json")
	meta, err := ParseResponse(data)

	require.NoError(t, err)
	assert.False(t, meta.IsTestOnly, "implementation work should not be test-only")
}

// --- Edge Cases ---

func TestParseResponse_EmptyInput(t *testing.T) {
	_, err := ParseResponse([]byte{})
	assert.Error(t, err)
}

func TestParseResponse_MalformedJSON(t *testing.T) {
	_, err := ParseResponse([]byte(`{broken json`))
	assert.Error(t, err)
}

func TestParseResponse_ValidJSONMissingFields(t *testing.T) {
	meta, err := ParseResponse([]byte(`{"type":"result"}`))

	require.NoError(t, err)
	assert.Empty(t, meta.SessionID)
	assert.False(t, meta.Completed)
	assert.False(t, meta.HasErrors)
	assert.Equal(t, 0, meta.FilesModified)
}

func TestParseResponse_MixedTextAndJSON(t *testing.T) {
	// stderr prefix before JSON (common with subprocess capture)
	input := []byte(`some stderr output
{"type":"result","subtype":"success","is_error":false,"result":"Done.","session_id":"abc-123"}`)
	meta, err := ParseResponse(input)

	require.NoError(t, err)
	assert.Equal(t, "abc-123", meta.SessionID)
}

func TestParseResponse_RalphStatusExitSignalFalse(t *testing.T) {
	// Explicit EXIT_SIGNAL: false should be respected
	input := []byte(`{"type":"result","subtype":"success","is_error":false,"result":"Task done but more work remains.\n\n---RALPH_STATUS---\nSTATUS: COMPLETE\nEXIT_SIGNAL: false\nFILES_MODIFIED: 3\n---END_RALPH_STATUS---","session_id":"xyz-789"}`)
	meta, err := ParseResponse(input)

	require.NoError(t, err)
	assert.True(t, meta.Completed, "STATUS: COMPLETE should set Completed")
	assert.False(t, meta.ExitSignal, "EXIT_SIGNAL: false should be respected")
}
