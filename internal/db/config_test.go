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
	assert.Equal(t, "qwen3-coder:70b", cfg.LargeModel.Name)
	assert.Equal(t, "cpu", cfg.LargeModel.Device)
	assert.Equal(t, "qwen2.5-coder:7b", cfg.SmallModel.Name)
	assert.Equal(t, "gpu", cfg.SmallModel.Device)
	assert.Equal(t, "http://localhost:11434", cfg.Ollama.Host)
	assert.False(t, cfg.Ollama.IsRemote)
}

func TestConfigRepo_Save(t *testing.T) {
	db := newTestDB(t)
	repo := NewConfigRepo(db)

	cfg := models.DefaultServerConfig()
	cfg.LargeModel.Name = "custom-model:latest"
	cfg.LargeModel.Device = "gpu"
	cfg.LargeModel.MemoryGB = 20
	cfg.ConcurrentJobs = 5

	err := repo.Save(cfg)
	require.NoError(t, err)

	// Fetch and verify
	fetched, err := repo.Get()
	require.NoError(t, err)

	assert.Equal(t, "custom-model:latest", fetched.LargeModel.Name)
	assert.Equal(t, "gpu", fetched.LargeModel.Device)
	assert.Equal(t, 20.0, fetched.LargeModel.MemoryGB)
	assert.Equal(t, 5, fetched.ConcurrentJobs)
}

func TestConfigRepo_SaveOllama(t *testing.T) {
	db := newTestDB(t)
	repo := NewConfigRepo(db)

	cfg := models.DefaultServerConfig()
	cfg.Ollama.Host = "http://192.168.1.50:11434"
	cfg.Ollama.IsRemote = true

	err := repo.Save(cfg)
	require.NoError(t, err)

	fetched, err := repo.Get()
	require.NoError(t, err)

	assert.Equal(t, "http://192.168.1.50:11434", fetched.Ollama.Host)
	assert.True(t, fetched.Ollama.IsRemote)
}

func TestConfigRepo_Update(t *testing.T) {
	db := newTestDB(t)
	repo := NewConfigRepo(db)

	// Save initial config
	cfg := models.DefaultServerConfig()
	err := repo.Save(cfg)
	require.NoError(t, err)

	// Update specific field using JSON
	err = repo.Update("large_model", `{"name":"updated-model","device":"gpu","memory_gb":20}`)
	require.NoError(t, err)

	// Verify
	fetched, err := repo.Get()
	require.NoError(t, err)
	assert.Equal(t, "updated-model", fetched.LargeModel.Name)
	assert.Equal(t, "gpu", fetched.LargeModel.Device)
}

func TestConfigRepo_GetKey(t *testing.T) {
	db := newTestDB(t)
	repo := NewConfigRepo(db)

	cfg := models.DefaultServerConfig()
	cfg.LargeModel.Name = "test-model"
	err := repo.Save(cfg)
	require.NoError(t, err)

	value, err := repo.GetKey("large_model")
	require.NoError(t, err)
	assert.Contains(t, value, "test-model")
}

func TestConfigRepo_GetKey_NotFound(t *testing.T) {
	db := newTestDB(t)
	repo := NewConfigRepo(db)

	_, err := repo.GetKey("nonexistent")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestConfigRepo_BackwardsCompat(t *testing.T) {
	db := newTestDB(t)
	repo := NewConfigRepo(db)

	// Simulate old-format data: plain string model name
	err := repo.Update("large_model", "old-plain-model-name")
	require.NoError(t, err)

	cfg, err := repo.Get()
	require.NoError(t, err)
	// Should parse as plain name, keeping default device
	assert.Equal(t, "old-plain-model-name", cfg.LargeModel.Name)
}
