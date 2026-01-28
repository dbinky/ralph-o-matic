package db

import (
	"testing"

	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigRepo_GetDefault(t *testing.T) {
	db := newTestDB(t)
	repo := NewConfigRepo(db)

	cfg, err := repo.Get()
	require.NoError(t, err)

	// Should return defaults when no config exists
	assert.Equal(t, "qwen3-coder:70b", cfg.LargeModel)
	assert.Equal(t, "qwen2.5-coder:7b", cfg.SmallModel)
}

func TestConfigRepo_Save(t *testing.T) {
	db := newTestDB(t)
	repo := NewConfigRepo(db)

	cfg := models.DefaultServerConfig()
	cfg.LargeModel = "custom-model:latest"
	cfg.ConcurrentJobs = 5

	err := repo.Save(cfg)
	require.NoError(t, err)

	// Fetch and verify
	fetched, err := repo.Get()
	require.NoError(t, err)

	assert.Equal(t, "custom-model:latest", fetched.LargeModel)
	assert.Equal(t, 5, fetched.ConcurrentJobs)
}

func TestConfigRepo_Update(t *testing.T) {
	db := newTestDB(t)
	repo := NewConfigRepo(db)

	// Save initial config
	cfg := models.DefaultServerConfig()
	err := repo.Save(cfg)
	require.NoError(t, err)

	// Update specific field
	err = repo.Update("large_model", "updated-model")
	require.NoError(t, err)

	// Verify
	fetched, err := repo.Get()
	require.NoError(t, err)
	assert.Equal(t, "updated-model", fetched.LargeModel)
}

func TestConfigRepo_GetKey(t *testing.T) {
	db := newTestDB(t)
	repo := NewConfigRepo(db)

	cfg := models.DefaultServerConfig()
	cfg.LargeModel = "test-model"
	err := repo.Save(cfg)
	require.NoError(t, err)

	value, err := repo.GetKey("large_model")
	require.NoError(t, err)
	assert.Equal(t, "test-model", value)
}

func TestConfigRepo_GetKey_NotFound(t *testing.T) {
	db := newTestDB(t)
	repo := NewConfigRepo(db)

	_, err := repo.GetKey("nonexistent")
	assert.ErrorIs(t, err, ErrNotFound)
}
