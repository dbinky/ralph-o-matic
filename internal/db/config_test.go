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
	assert.Equal(t, "devstral", cfg.LargeModel.Name)
	assert.Equal(t, "cpu", cfg.LargeModel.Device)
	assert.Equal(t, "qwen3:8b", cfg.SmallModel.Name)
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

	err := repo.Save(cfg)
	require.NoError(t, err)

	// Fetch and verify
	fetched, err := repo.Get()
	require.NoError(t, err)

	assert.Equal(t, "custom-model:latest", fetched.LargeModel.Name)
	assert.Equal(t, "gpu", fetched.LargeModel.Device)
	assert.Equal(t, 20.0, fetched.LargeModel.MemoryGB)
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

func TestConfigRepo_FullRoundTrip_AllStructuredFields(t *testing.T) {
	db := newTestDB(t)
	repo := NewConfigRepo(db)

	cfg := models.DefaultServerConfig()
	cfg.Ollama.Host = "http://192.168.1.50:11434"
	cfg.Ollama.IsRemote = true
	cfg.LargeModel = models.ModelPlacement{Name: "custom-large:70b", Device: "gpu", MemoryGB: 42}
	cfg.SmallModel = models.ModelPlacement{Name: "custom-small:1.5b", Device: "cpu", MemoryGB: 1.5}
	cfg.DefaultMaxIterations = 100
	cfg.WorkspaceDir = "/tmp/test-workspace"
	cfg.JobRetentionDays = 7
	cfg.MaxClaudeRetries = 5
	cfg.MaxGitRetries = 2
	cfg.GitRetryBackoffMs = 500

	err := repo.Save(cfg)
	require.NoError(t, err)

	fetched, err := repo.Get()
	require.NoError(t, err)

	assert.Equal(t, "http://192.168.1.50:11434", fetched.Ollama.Host)
	assert.True(t, fetched.Ollama.IsRemote)
	assert.Equal(t, "custom-large:70b", fetched.LargeModel.Name)
	assert.Equal(t, "gpu", fetched.LargeModel.Device)
	assert.Equal(t, 42.0, fetched.LargeModel.MemoryGB)
	assert.Equal(t, "custom-small:1.5b", fetched.SmallModel.Name)
	assert.Equal(t, "cpu", fetched.SmallModel.Device)
	assert.Equal(t, 1.5, fetched.SmallModel.MemoryGB)
	assert.Equal(t, 100, fetched.DefaultMaxIterations)
	assert.Equal(t, "/tmp/test-workspace", fetched.WorkspaceDir)
	assert.Equal(t, 7, fetched.JobRetentionDays)
	assert.Equal(t, 5, fetched.MaxClaudeRetries)
	assert.Equal(t, 2, fetched.MaxGitRetries)
	assert.Equal(t, 500, fetched.GitRetryBackoffMs)
}

func TestConfigRepo_UpdateScalar_PreservesStructured(t *testing.T) {
	db := newTestDB(t)
	repo := NewConfigRepo(db)

	cfg := models.DefaultServerConfig()
	cfg.LargeModel = models.ModelPlacement{Name: "keep-this:70b", Device: "gpu", MemoryGB: 42}
	err := repo.Save(cfg)
	require.NoError(t, err)

	// Update a scalar field
	err = repo.Update("default_max_iterations", "100")
	require.NoError(t, err)

	fetched, err := repo.Get()
	require.NoError(t, err)
	assert.Equal(t, 100, fetched.DefaultMaxIterations)
	assert.Equal(t, "keep-this:70b", fetched.LargeModel.Name)
	assert.Equal(t, "gpu", fetched.LargeModel.Device)
	assert.Equal(t, 42.0, fetched.LargeModel.MemoryGB)
}

func TestConfigRepo_BackwardsCompat_ThenStructured(t *testing.T) {
	db := newTestDB(t)
	repo := NewConfigRepo(db)

	// Save old-style plain string
	err := repo.Update("large_model", "old-model-name")
	require.NoError(t, err)

	// Now save structured config
	cfg := models.DefaultServerConfig()
	cfg.LargeModel = models.ModelPlacement{Name: "new-structured:70b", Device: "gpu", MemoryGB: 42}
	err = repo.Save(cfg)
	require.NoError(t, err)

	fetched, err := repo.Get()
	require.NoError(t, err)
	assert.Equal(t, "new-structured:70b", fetched.LargeModel.Name)
	assert.Equal(t, "gpu", fetched.LargeModel.Device)
}

func TestConfigRepo_FloatPrecision(t *testing.T) {
	db := newTestDB(t)
	repo := NewConfigRepo(db)

	cfg := models.DefaultServerConfig()
	cfg.LargeModel.MemoryGB = 42.5
	err := repo.Save(cfg)
	require.NoError(t, err)

	fetched, err := repo.Get()
	require.NoError(t, err)
	assert.Equal(t, 42.5, fetched.LargeModel.MemoryGB)
}

func TestConfigRepo_SaveAnthropic(t *testing.T) {
	db := newTestDB(t)
	repo := NewConfigRepo(db)

	cfg := models.DefaultServerConfig()
	cfg.DefaultBackend = models.BackendAnthropic
	cfg.Anthropic.LargeModel = "claude-sonnet-4-20250514"
	cfg.Anthropic.SmallModel = "claude-haiku-4-5-20251001"

	err := repo.Save(cfg)
	require.NoError(t, err)

	fetched, err := repo.Get()
	require.NoError(t, err)

	assert.Equal(t, models.BackendAnthropic, fetched.DefaultBackend)
	assert.Equal(t, "claude-sonnet-4-20250514", fetched.Anthropic.LargeModel)
	assert.Equal(t, "claude-haiku-4-5-20251001", fetched.Anthropic.SmallModel)
}

func TestConfigRepo_DefaultBackend_Defaults(t *testing.T) {
	db := newTestDB(t)
	repo := NewConfigRepo(db)

	cfg, err := repo.Get()
	require.NoError(t, err)
	assert.Equal(t, models.BackendOllama, cfg.DefaultBackend)
}

func TestConfigRepo_SaveLoad_NoConcurrentJobs(t *testing.T) {
	db := newTestDB(t)
	repo := NewConfigRepo(db)

	cfg := models.DefaultServerConfig()
	err := repo.Save(cfg)
	require.NoError(t, err)

	// Verify concurrent_jobs key is NOT in the database
	_, err = repo.GetKey("concurrent_jobs")
	assert.ErrorIs(t, err, ErrNotFound, "concurrent_jobs should not be stored in DB")

	// Load should work fine without it
	loaded, err := repo.Get()
	require.NoError(t, err)
	assert.Equal(t, "devstral", loaded.LargeModel.Name)
}

func TestConfigRepo_UnknownKey_ConcurrentJobs_Ignored(t *testing.T) {
	db := newTestDB(t)
	repo := NewConfigRepo(db)

	// Simulate pre-migration DB with orphaned concurrent_jobs key
	err := repo.Update("concurrent_jobs", "5")
	require.NoError(t, err)

	// Loading config should succeed (unknown keys are skipped)
	cfg, err := repo.Get()
	require.NoError(t, err)
	assert.Equal(t, "devstral", cfg.LargeModel.Name)
}

func TestConfigRepo_FullRoundTrip_NoConcurrentJobs(t *testing.T) {
	db := newTestDB(t)
	repo := NewConfigRepo(db)

	cfg := models.DefaultServerConfig()
	cfg.LargeModel = models.ModelPlacement{Name: "custom:70b", Device: "gpu", MemoryGB: 42}
	cfg.DefaultMaxIterations = 100
	cfg.WorkspaceDir = "/tmp/test"
	cfg.JobRetentionDays = 7

	err := repo.Save(cfg)
	require.NoError(t, err)

	fetched, err := repo.Get()
	require.NoError(t, err)
	assert.Equal(t, "custom:70b", fetched.LargeModel.Name)
	assert.Equal(t, 100, fetched.DefaultMaxIterations)
	assert.Equal(t, "/tmp/test", fetched.WorkspaceDir)
	assert.Equal(t, 7, fetched.JobRetentionDays)
}

func TestConfigRepo_SaveThenSave_Overwrites(t *testing.T) {
	db := newTestDB(t)
	repo := NewConfigRepo(db)

	cfg1 := models.DefaultServerConfig()
	cfg1.LargeModel.Name = "first-model"
	err := repo.Save(cfg1)
	require.NoError(t, err)

	cfg2 := models.DefaultServerConfig()
	cfg2.LargeModel.Name = "second-model"
	err = repo.Save(cfg2)
	require.NoError(t, err)

	fetched, err := repo.Get()
	require.NoError(t, err)
	assert.Equal(t, "second-model", fetched.LargeModel.Name)
}
