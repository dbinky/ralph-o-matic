package db

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/ryan/ralph-o-matic/internal/broadcast"
	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogRepo_Append(t *testing.T) {
	db := newTestDB(t)
	jobRepo := NewJobRepo(db)
	logRepo := NewLogRepo(db)

	// Create a job first
	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	err := jobRepo.Create(job)
	require.NoError(t, err)

	// Append logs
	err = logRepo.Append(job.ID, 1, "Starting iteration 1")
	require.NoError(t, err)

	err = logRepo.Append(job.ID, 1, "Running tests...")
	require.NoError(t, err)
}

func TestLogRepo_GetForJob(t *testing.T) {
	db := newTestDB(t)
	jobRepo := NewJobRepo(db)
	logRepo := NewLogRepo(db)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	err := jobRepo.Create(job)
	require.NoError(t, err)

	// Add logs for multiple iterations
	messages := []struct {
		iteration int
		message   string
	}{
		{1, "Iteration 1 start"},
		{1, "Iteration 1 end"},
		{2, "Iteration 2 start"},
		{2, "Iteration 2 end"},
	}

	for _, m := range messages {
		err := logRepo.Append(job.ID, m.iteration, m.message)
		require.NoError(t, err)
	}

	logs, err := logRepo.GetForJob(job.ID)
	require.NoError(t, err)
	assert.Len(t, logs, 4)
	assert.Equal(t, "Iteration 1 start", logs[0].Message)
}

func TestLogRepo_GetForIteration(t *testing.T) {
	db := newTestDB(t)
	jobRepo := NewJobRepo(db)
	logRepo := NewLogRepo(db)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	err := jobRepo.Create(job)
	require.NoError(t, err)

	logRepo.Append(job.ID, 1, "Iter 1 message")
	logRepo.Append(job.ID, 2, "Iter 2 message")
	logRepo.Append(job.ID, 2, "Iter 2 another message")

	logs, err := logRepo.GetForIteration(job.ID, 2)
	require.NoError(t, err)
	assert.Len(t, logs, 2)
}

func TestLogRepo_DeleteForJob(t *testing.T) {
	db := newTestDB(t)
	jobRepo := NewJobRepo(db)
	logRepo := NewLogRepo(db)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	err := jobRepo.Create(job)
	require.NoError(t, err)

	logRepo.Append(job.ID, 1, "Message 1")
	logRepo.Append(job.ID, 1, "Message 2")

	err = logRepo.DeleteForJob(job.ID)
	require.NoError(t, err)

	logs, err := logRepo.GetForJob(job.ID)
	require.NoError(t, err)
	assert.Len(t, logs, 0)
}

func TestLogRepo_GetLatest(t *testing.T) {
	db := newTestDB(t)
	jobRepo := NewJobRepo(db)
	logRepo := NewLogRepo(db)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	err := jobRepo.Create(job)
	require.NoError(t, err)

	for i := 0; i < 10; i++ {
		logRepo.Append(job.ID, 1, "Message")
	}

	logs, err := logRepo.GetLatest(job.ID, 5)
	require.NoError(t, err)
	assert.Len(t, logs, 5)
}

func TestLogRepo_Append_PublishesBroadcast(t *testing.T) {
	db := newTestDB(t)
	jobRepo := NewJobRepo(db)
	b := broadcast.New()
	logRepo := NewLogRepo(db)
	logRepo.SetBroadcaster(b)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, jobRepo.Create(job))

	_, ch := b.Subscribe(fmt.Sprintf("job:%d", job.ID))

	err := logRepo.Append(job.ID, 1, "Starting iteration 1")
	require.NoError(t, err)

	select {
	case msg := <-ch:
		var payload map[string]interface{}
		require.NoError(t, json.Unmarshal(msg, &payload))
		assert.Equal(t, "log", payload["type"])
		assert.Equal(t, float64(1), payload["iteration"])
		assert.Equal(t, "Starting iteration 1", payload["message"])
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for broadcast")
	}
}

func TestLogRepo_Append_SpecialCharacters(t *testing.T) {
	db := newTestDB(t)
	jobRepo := NewJobRepo(db)
	b := broadcast.New()
	logRepo := NewLogRepo(db)
	logRepo.SetBroadcaster(b)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, jobRepo.Create(job))

	_, ch := b.Subscribe(fmt.Sprintf("job:%d", job.ID))

	// Message with quotes, newlines, and unicode
	msg := "error: \"file not found\"\nnext line\ttab \u2603"
	err := logRepo.Append(job.ID, 2, msg)
	require.NoError(t, err)

	select {
	case raw := <-ch:
		var payload map[string]interface{}
		require.NoError(t, json.Unmarshal(raw, &payload), "payload must be valid JSON: %s", string(raw))
		assert.Equal(t, msg, payload["message"])
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for broadcast")
	}
}

func TestLogRepo_Append_NilBroadcaster(t *testing.T) {
	db := newTestDB(t)
	jobRepo := NewJobRepo(db)
	logRepo := NewLogRepo(db)
	// No broadcaster set

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, jobRepo.Create(job))

	// Should work without panic
	err := logRepo.Append(job.ID, 1, "Hello")
	require.NoError(t, err)
}
