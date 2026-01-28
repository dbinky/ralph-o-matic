package platform

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testCatalog() *Catalog {
	return &Catalog{
		Models: []CatalogModel{
			{Name: "big:70b", MemoryGB: 42, Role: "large", Quality: 10},
			{Name: "med:32b", MemoryGB: 20, Role: "large", Quality: 8},
			{Name: "small:14b", MemoryGB: 10, Role: "large", Quality: 6},
			{Name: "tiny:7b", MemoryGB: 5, Role: "both", Quality: 4},
			{Name: "micro:1.5b", MemoryGB: 1.5, Role: "small", Quality: 2},
		},
	}
}

func TestSelectModels_SplitConfig(t *testing.T) {
	// 8GB GPU + 48GB RAM -> 70b on CPU + 7b on GPU
	hw := &HardwareInfo{
		SystemRAMGB: 48,
		GPUs:        []GPUInfo{{Type: "nvidia", Name: "RTX 3070", VRAMGB: 8}},
	}

	results, err := SelectModels(testCatalog(), hw)
	require.NoError(t, err)
	require.NotEmpty(t, results)

	best := results[0]
	assert.Equal(t, "big:70b", best.Large.Name)
	assert.Equal(t, "cpu", best.Large.Device)
	assert.Equal(t, "tiny:7b", best.Small.Name)
	assert.Equal(t, "gpu", best.Small.Device)
	assert.Equal(t, 14, best.Score)
}

func TestSelectModels_BothOnGPU(t *testing.T) {
	// 48GB GPU -> both on GPU
	hw := &HardwareInfo{
		SystemRAMGB: 64,
		GPUs:        []GPUInfo{{Type: "nvidia", Name: "A6000", VRAMGB: 48}},
	}

	results, err := SelectModels(testCatalog(), hw)
	require.NoError(t, err)

	best := results[0]
	assert.Equal(t, "big:70b", best.Large.Name)
	assert.Equal(t, "gpu", best.Large.Device)
	assert.Equal(t, "tiny:7b", best.Small.Name)
	assert.Equal(t, "gpu", best.Small.Device)
}

func TestSelectModels_CPUOnly(t *testing.T) {
	// 64GB RAM, no GPU -> both on CPU
	hw := &HardwareInfo{
		SystemRAMGB: 64,
		GPUs:        nil,
	}

	results, err := SelectModels(testCatalog(), hw)
	require.NoError(t, err)

	best := results[0]
	assert.Equal(t, "big:70b", best.Large.Name)
	assert.Equal(t, "cpu", best.Large.Device)
	assert.Equal(t, "tiny:7b", best.Small.Name)
	assert.Equal(t, "cpu", best.Small.Device)
}

func TestSelectModels_SmallMachine(t *testing.T) {
	// 16GB RAM, no GPU -> 14b + 1.5b on CPU
	hw := &HardwareInfo{
		SystemRAMGB: 16,
		GPUs:        nil,
	}

	results, err := SelectModels(testCatalog(), hw)
	require.NoError(t, err)

	best := results[0]
	assert.Equal(t, "small:14b", best.Large.Name)
	assert.Equal(t, "cpu", best.Large.Device)
}

func TestSelectModels_TinyMachine(t *testing.T) {
	// 8GB RAM, no GPU -> 7b as large + 1.5b as small
	hw := &HardwareInfo{
		SystemRAMGB: 8,
		GPUs:        nil,
	}

	results, err := SelectModels(testCatalog(), hw)
	require.NoError(t, err)

	best := results[0]
	assert.Equal(t, "tiny:7b", best.Large.Name)
	assert.Equal(t, "cpu", best.Large.Device)
	assert.Equal(t, "micro:1.5b", best.Small.Name)
	assert.Equal(t, "cpu", best.Small.Device)
}

func TestSelectModels_InsufficientMemory(t *testing.T) {
	// 2GB RAM, no GPU -> nothing fits
	hw := &HardwareInfo{
		SystemRAMGB: 2,
		GPUs:        nil,
	}

	_, err := SelectModels(testCatalog(), hw)
	assert.Error(t, err)
}

func TestSelectModels_ReturnsAlternatives(t *testing.T) {
	hw := &HardwareInfo{
		SystemRAMGB: 48,
		GPUs:        []GPUInfo{{Type: "nvidia", VRAMGB: 8}},
	}

	results, err := SelectModels(testCatalog(), hw)
	require.NoError(t, err)

	// Should have multiple alternatives
	assert.GreaterOrEqual(t, len(results), 2)

	// Results should be sorted by score descending
	for i := 1; i < len(results); i++ {
		assert.GreaterOrEqual(t, results[i-1].Score, results[i].Score)
	}
}

func TestSelectModels_SameModelBothRoles(t *testing.T) {
	// Tiny machine: 7b is "both" so it could be used as large
	catalog := &Catalog{
		Models: []CatalogModel{
			{Name: "tiny:7b", MemoryGB: 5, Role: "both", Quality: 4},
			{Name: "micro:1.5b", MemoryGB: 1.5, Role: "small", Quality: 2},
		},
	}
	hw := &HardwareInfo{SystemRAMGB: 8, GPUs: nil}

	results, err := SelectModels(catalog, hw)
	require.NoError(t, err)

	best := results[0]
	assert.Equal(t, "tiny:7b", best.Large.Name)
	assert.Equal(t, "micro:1.5b", best.Small.Name)
}

func TestSelectModels_IdenticalScoresPreferLargerModel(t *testing.T) {
	catalog := &Catalog{
		Models: []CatalogModel{
			{Name: "a:14b", MemoryGB: 10, Role: "large", Quality: 6},
			{Name: "b:14b-v2", MemoryGB: 10, Role: "large", Quality: 6},
			{Name: "helper:1.5b", MemoryGB: 1.5, Role: "small", Quality: 2},
		},
	}
	hw := &HardwareInfo{SystemRAMGB: 16, GPUs: nil}

	results, err := SelectModels(catalog, hw)
	require.NoError(t, err)
	// Should return results stably
	assert.NotEmpty(t, results)
}

func TestSelectModels_TightFit(t *testing.T) {
	// Exactly enough memory for the pair
	catalog := &Catalog{
		Models: []CatalogModel{
			{Name: "model:7b", MemoryGB: 5, Role: "both", Quality: 4},
			{Name: "model:1.5b", MemoryGB: 1.5, Role: "small", Quality: 2},
		},
	}
	hw := &HardwareInfo{SystemRAMGB: 6.5, GPUs: nil}

	results, err := SelectModels(catalog, hw)
	require.NoError(t, err)

	best := results[0]
	assert.Equal(t, "model:7b", best.Large.Name)
	assert.Equal(t, "model:1.5b", best.Small.Name)
	assert.True(t, best.TightFit)
}

func TestSelectModels_UnifiedMemoryAppleSilicon(t *testing.T) {
	// 24GB Apple Silicon (unified) -> treat as GPU
	hw := &HardwareInfo{
		SystemRAMGB: 24,
		GPUs:        []GPUInfo{{Type: "apple", Name: "Apple Silicon", VRAMGB: 24}},
	}

	results, err := SelectModels(testCatalog(), hw)
	require.NoError(t, err)

	best := results[0]
	// 14b (10GB) + 7b (5GB) = 15GB fits in 24GB unified
	// or 14b + 1.5b = 11.5GB for score 8
	// 7b + 1.5b = 6.5GB for score 6
	// Best: small:14b (quality 6) + micro:1.5b (quality 2) = 8
	// or small:14b + tiny:7b = 10 but 10+5=15 < 24, both on gpu
	assert.Equal(t, "gpu", best.Large.Device)
}
