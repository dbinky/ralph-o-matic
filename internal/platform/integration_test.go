package platform

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_FullPipeline_HighEndMachine(t *testing.T) {
	catalog, err := LoadEmbeddedCatalog()
	require.NoError(t, err)

	hw := &HardwareInfo{
		SystemRAMGB: 64,
		GPUs:        []GPUInfo{{Type: "nvidia", Name: "RTX 4090", VRAMGB: 24}},
	}

	results, err := SelectModels(catalog, hw)
	require.NoError(t, err)
	require.NotEmpty(t, results)

	best := results[0]
	assert.NotEmpty(t, best.Large.Name)
	assert.NotEmpty(t, best.Small.Name)
	assert.Greater(t, best.Score, 0)
}

func TestIntegration_FullPipeline_LowEndMachine(t *testing.T) {
	catalog, err := LoadEmbeddedCatalog()
	require.NoError(t, err)

	hw := &HardwareInfo{
		SystemRAMGB: 8,
		GPUs:        nil,
	}

	results, err := SelectModels(catalog, hw)
	require.NoError(t, err)
	require.NotEmpty(t, results)

	best := results[0]
	assert.Equal(t, "cpu", best.Large.Device)
	assert.Equal(t, "cpu", best.Small.Device)
	// Total memory must fit in 8GB
	assert.LessOrEqual(t, best.Large.MemoryGB+best.Small.MemoryGB, 8.0)
}

func TestIntegration_FullPipeline_AppleSilicon(t *testing.T) {
	catalog, err := LoadEmbeddedCatalog()
	require.NoError(t, err)

	hw := &HardwareInfo{
		SystemRAMGB: 36,
		GPUs:        []GPUInfo{{Type: "apple", Name: "Apple Silicon", VRAMGB: 36}},
	}

	results, err := SelectModels(catalog, hw)
	require.NoError(t, err)
	require.NotEmpty(t, results)

	best := results[0]
	assert.Equal(t, "gpu", best.Large.Device)
	assert.Equal(t, "gpu", best.Small.Device)
}

func TestIntegration_FullPipeline_ResultIsValidServerConfig(t *testing.T) {
	catalog, err := LoadEmbeddedCatalog()
	require.NoError(t, err)

	hw := &HardwareInfo{
		SystemRAMGB: 64,
		GPUs:        nil,
	}

	results, err := SelectModels(catalog, hw)
	require.NoError(t, err)
	require.NotEmpty(t, results)

	best := results[0]

	cfg := models.DefaultServerConfig()
	cfg.LargeModel = best.Large
	cfg.SmallModel = best.Small

	err = cfg.Validate()
	assert.NoError(t, err)
}

func TestIntegration_OllamaClientWithCatalog(t *testing.T) {
	catalog, err := LoadEmbeddedCatalog()
	require.NoError(t, err)

	// Build a mock Ollama that has all catalog models
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ollamaModels []map[string]interface{}
		for _, m := range catalog.Models {
			ollamaModels = append(ollamaModels, map[string]interface{}{
				"name": m.Name,
				"size": int64(m.MemoryGB * 1024 * 1024 * 1024),
			})
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"models": ollamaModels})
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)

	// Select models for a machine
	hw := &HardwareInfo{SystemRAMGB: 64, GPUs: nil}
	results, err := SelectModels(catalog, hw)
	require.NoError(t, err)

	best := results[0]

	// Verify the recommended models exist on the mock Ollama
	hasLarge, err := client.HasModel(context.Background(), best.Large.Name)
	require.NoError(t, err)
	assert.True(t, hasLarge)

	hasSmall, err := client.HasModel(context.Background(), best.Small.Name)
	require.NoError(t, err)
	assert.True(t, hasSmall)
}
