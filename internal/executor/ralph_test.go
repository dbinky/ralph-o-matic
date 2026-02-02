package executor

import (
	"testing"

	"github.com/ryan/ralph-o-matic/internal/db"
	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.New(":memory:")
	require.NoError(t, err)
	require.NoError(t, database.Migrate())
	t.Cleanup(func() { database.Close() })
	return database
}

func TestEffectiveBackend(t *testing.T) {
	t.Run("job backend takes precedence", func(t *testing.T) {
		result := effectiveBackend(models.BackendAnthropic, models.BackendOllama)
		assert.Equal(t, models.BackendAnthropic, result)
	})

	t.Run("falls back to server default", func(t *testing.T) {
		result := effectiveBackend("", models.BackendAnthropic)
		assert.Equal(t, models.BackendAnthropic, result)
	})

	t.Run("falls back to ollama when both empty", func(t *testing.T) {
		result := effectiveBackend("", "")
		assert.Equal(t, models.BackendOllama, result)
	})
}

func TestRalphHandler_UpdateIteration(t *testing.T) {
	database := newTestDB(t)
	jobRepo := db.NewJobRepo(database)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, jobRepo.Create(job))

	handler := NewRalphHandler(database, models.DefaultServerConfig(), "/tmp")

	handler.updateIteration(job, 5)
	assert.Equal(t, 5, job.Iteration)

	// Verify persisted
	fetched, _ := jobRepo.Get(job.ID)
	assert.Equal(t, 5, fetched.Iteration)
}
