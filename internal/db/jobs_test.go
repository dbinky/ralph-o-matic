package db

import (
	"testing"
	"time"

	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJobRepo_Create(t *testing.T) {
	db := newTestDB(t)
	repo := NewJobRepo(db)

	job := models.NewJob(
		"git@github.com:user/repo.git",
		"feature/test",
		"Run all tests",
		50,
	)
	job.Priority = models.PriorityHigh
	job.WorkingDir = "packages/auth"
	job.Env = map[string]string{"NODE_ENV": "test"}

	err := repo.Create(job)
	require.NoError(t, err)

	assert.Greater(t, job.ID, int64(0))
	assert.Equal(t, 1, job.Position) // First job gets position 1
}

func TestJobRepo_Get(t *testing.T) {
	db := newTestDB(t)
	repo := NewJobRepo(db)

	// Create a job
	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	err := repo.Create(job)
	require.NoError(t, err)

	// Fetch it back
	fetched, err := repo.Get(job.ID)
	require.NoError(t, err)

	assert.Equal(t, job.ID, fetched.ID)
	assert.Equal(t, job.RepoURL, fetched.RepoURL)
	assert.Equal(t, job.Branch, fetched.Branch)
	assert.Equal(t, job.Prompt, fetched.Prompt)
	assert.Equal(t, job.Status, fetched.Status)
}

func TestJobRepo_Get_NotFound(t *testing.T) {
	db := newTestDB(t)
	repo := NewJobRepo(db)

	_, err := repo.Get(99999)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestJobRepo_Update(t *testing.T) {
	db := newTestDB(t)
	repo := NewJobRepo(db)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	err := repo.Create(job)
	require.NoError(t, err)

	// Modify and update
	job.Status = models.StatusRunning
	now := time.Now()
	job.StartedAt = &now
	job.Iteration = 5

	err = repo.Update(job)
	require.NoError(t, err)

	// Verify changes persisted
	fetched, err := repo.Get(job.ID)
	require.NoError(t, err)

	assert.Equal(t, models.StatusRunning, fetched.Status)
	assert.NotNil(t, fetched.StartedAt)
	assert.Equal(t, 5, fetched.Iteration)
}

func TestJobRepo_Delete(t *testing.T) {
	db := newTestDB(t)
	repo := NewJobRepo(db)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	err := repo.Create(job)
	require.NoError(t, err)

	err = repo.Delete(job.ID)
	require.NoError(t, err)

	_, err = repo.Get(job.ID)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestJobRepo_List_All(t *testing.T) {
	db := newTestDB(t)
	repo := NewJobRepo(db)

	// Create multiple jobs
	for i := 0; i < 5; i++ {
		job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
		err := repo.Create(job)
		require.NoError(t, err)
	}

	jobs, total, err := repo.List(ListOptions{})
	require.NoError(t, err)

	assert.Equal(t, 5, total)
	assert.Len(t, jobs, 5)
}

func TestJobRepo_List_WithStatus(t *testing.T) {
	db := newTestDB(t)
	repo := NewJobRepo(db)

	// Create jobs with different statuses
	for _, status := range []models.JobStatus{models.StatusQueued, models.StatusQueued, models.StatusRunning} {
		job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
		job.Status = status
		err := repo.Create(job)
		require.NoError(t, err)
	}

	jobs, total, err := repo.List(ListOptions{
		Statuses: []models.JobStatus{models.StatusQueued},
	})
	require.NoError(t, err)

	assert.Equal(t, 2, total)
	assert.Len(t, jobs, 2)
	for _, job := range jobs {
		assert.Equal(t, models.StatusQueued, job.Status)
	}
}

func TestJobRepo_List_WithPagination(t *testing.T) {
	db := newTestDB(t)
	repo := NewJobRepo(db)

	// Create 10 jobs
	for i := 0; i < 10; i++ {
		job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
		err := repo.Create(job)
		require.NoError(t, err)
	}

	// Get first page
	jobs, total, err := repo.List(ListOptions{Limit: 3, Offset: 0})
	require.NoError(t, err)
	assert.Equal(t, 10, total)
	assert.Len(t, jobs, 3)

	// Get second page
	jobs, total, err = repo.List(ListOptions{Limit: 3, Offset: 3})
	require.NoError(t, err)
	assert.Equal(t, 10, total)
	assert.Len(t, jobs, 3)
}

func TestJobRepo_ListQueued(t *testing.T) {
	db := newTestDB(t)
	repo := NewJobRepo(db)

	// Create jobs with different priorities
	for i, priority := range []models.Priority{models.PriorityLow, models.PriorityHigh, models.PriorityNormal} {
		job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
		job.Priority = priority
		job.Position = i + 1
		err := repo.Create(job)
		require.NoError(t, err)
	}

	jobs, err := repo.ListQueued()
	require.NoError(t, err)

	// Should be ordered by priority (high first) then position
	require.Len(t, jobs, 3)
	assert.Equal(t, models.PriorityHigh, jobs[0].Priority)
	assert.Equal(t, models.PriorityNormal, jobs[1].Priority)
	assert.Equal(t, models.PriorityLow, jobs[2].Priority)
}

func TestJobRepo_GetByBranch(t *testing.T) {
	db := newTestDB(t)
	repo := NewJobRepo(db)

	job := models.NewJob("git@github.com:user/repo.git", "feature/unique", "test", 10)
	err := repo.Create(job)
	require.NoError(t, err)

	found, err := repo.GetByBranch("feature/unique", false)
	require.NoError(t, err)
	assert.Equal(t, job.ID, found.ID)
}

func TestJobRepo_GetByBranch_ActiveOnly(t *testing.T) {
	db := newTestDB(t)
	repo := NewJobRepo(db)

	// Create completed job
	job1 := models.NewJob("git@github.com:user/repo.git", "feature/test", "test", 10)
	job1.Status = models.StatusCompleted
	err := repo.Create(job1)
	require.NoError(t, err)

	// Create queued job with same branch
	job2 := models.NewJob("git@github.com:user/repo.git", "feature/test", "test", 10)
	err = repo.Create(job2)
	require.NoError(t, err)

	// Should find only the active one
	found, err := repo.GetByBranch("feature/test", true)
	require.NoError(t, err)
	assert.Equal(t, job2.ID, found.ID)
}

func TestJobRepo_UpdatePositions(t *testing.T) {
	db := newTestDB(t)
	repo := NewJobRepo(db)

	// Create 3 jobs
	var jobs []*models.Job
	for i := 0; i < 3; i++ {
		job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
		err := repo.Create(job)
		require.NoError(t, err)
		jobs = append(jobs, job)
	}

	// Reorder: [3, 1, 2]
	newOrder := []int64{jobs[2].ID, jobs[0].ID, jobs[1].ID}
	err := repo.UpdatePositions(newOrder)
	require.NoError(t, err)

	// Verify new positions
	fetched, err := repo.Get(jobs[2].ID)
	require.NoError(t, err)
	assert.Equal(t, 1, fetched.Position)

	fetched, err = repo.Get(jobs[0].ID)
	require.NoError(t, err)
	assert.Equal(t, 2, fetched.Position)

	fetched, err = repo.Get(jobs[1].ID)
	require.NoError(t, err)
	assert.Equal(t, 3, fetched.Position)
}

func TestJobRepo_NextPosition(t *testing.T) {
	db := newTestDB(t)
	repo := NewJobRepo(db)

	// Empty queue
	pos, err := repo.NextPosition()
	require.NoError(t, err)
	assert.Equal(t, 1, pos)

	// Add a job
	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	err = repo.Create(job)
	require.NoError(t, err)

	pos, err = repo.NextPosition()
	require.NoError(t, err)
	assert.Equal(t, 2, pos)
}

func TestJobRepo_CountByStatus(t *testing.T) {
	db := newTestDB(t)
	repo := NewJobRepo(db)

	// Create jobs with different statuses
	statuses := []models.JobStatus{
		models.StatusQueued, models.StatusQueued, models.StatusQueued,
		models.StatusRunning,
		models.StatusCompleted, models.StatusCompleted,
	}
	for _, status := range statuses {
		job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
		job.Status = status
		err := repo.Create(job)
		require.NoError(t, err)
	}

	counts, err := repo.CountByStatus()
	require.NoError(t, err)

	assert.Equal(t, 3, counts[models.StatusQueued])
	assert.Equal(t, 1, counts[models.StatusRunning])
	assert.Equal(t, 2, counts[models.StatusCompleted])
}
