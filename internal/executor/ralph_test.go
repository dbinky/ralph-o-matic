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

func TestRalphHandler_ShouldContinue(t *testing.T) {
	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)

	// Not at max
	job.Iteration = 5
	assert.True(t, shouldContinue(job))

	// At max
	job.Iteration = 10
	assert.False(t, shouldContinue(job))
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
