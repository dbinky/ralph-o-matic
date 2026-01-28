package platform

import (
	"fmt"
	"sort"

	"github.com/ryan/ralph-o-matic/internal/models"
)

// ModelConfig represents a recommended model configuration
type ModelConfig struct {
	Large    models.ModelPlacement
	Small    models.ModelPlacement
	Score    int  // large.quality + small.quality
	TightFit bool // true if total memory is within 10% of available
}

// SelectModels finds the best (large, small) model pairings for the given hardware
func SelectModels(catalog *Catalog, hw *HardwareInfo) ([]ModelConfig, error) {
	largeModels := catalog.LargeModels()
	smallModels := catalog.SmallModels()

	var configs []ModelConfig

	for _, large := range largeModels {
		for _, small := range smallModels {
			// Skip same model for both roles
			if large.Name == small.Name {
				continue
			}

			placement := findBestPlacement(large, small, hw)
			if placement == nil {
				continue
			}

			totalMemory := large.MemoryGB + small.MemoryGB
			availableMemory := hw.SystemRAMGB
			if hw.HasGPU() {
				availableMemory += hw.BestGPU().VRAMGB
				// For Apple Silicon, don't double-count
				if len(hw.GPUs) == 1 && hw.GPUs[0].Type == "apple" {
					availableMemory = hw.SystemRAMGB
				}
			}
			tightFit := totalMemory > (availableMemory * 0.9)

			configs = append(configs, ModelConfig{
				Large:    placement.large,
				Small:    placement.small,
				Score:    large.Quality + small.Quality,
				TightFit: tightFit,
			})
		}
	}

	if len(configs) == 0 {
		return nil, fmt.Errorf("no valid model configuration fits in available memory (%.1fGB RAM", hw.SystemRAMGB)
	}

	// Sort by score descending, then by large model memory descending (prefer bigger)
	sort.SliceStable(configs, func(i, j int) bool {
		if configs[i].Score != configs[j].Score {
			return configs[i].Score > configs[j].Score
		}
		return configs[i].Large.MemoryGB > configs[j].Large.MemoryGB
	})

	// Return top configs (max 5)
	if len(configs) > 5 {
		configs = configs[:5]
	}

	return configs, nil
}

type placementResult struct {
	large models.ModelPlacement
	small models.ModelPlacement
}

func findBestPlacement(large, small CatalogModel, hw *HardwareInfo) *placementResult {
	gpuMem := 0.0
	if hw.HasGPU() {
		gpuMem = hw.BestGPU().VRAMGB
	}
	cpuMem := hw.SystemRAMGB

	// For Apple Silicon, GPU memory IS system memory (unified)
	isUnified := len(hw.GPUs) == 1 && hw.GPUs[0].Type == "apple"

	if isUnified {
		// Unified memory: both share the same pool, but both get "gpu" device
		if large.MemoryGB+small.MemoryGB <= cpuMem {
			return &placementResult{
				large: models.ModelPlacement{Name: large.Name, Device: "gpu", MemoryGB: large.MemoryGB},
				small: models.ModelPlacement{Name: small.Name, Device: "gpu", MemoryGB: small.MemoryGB},
			}
		}
		return nil
	}

	// Strategy 1: Both on GPU
	if gpuMem > 0 && large.MemoryGB+small.MemoryGB <= gpuMem {
		return &placementResult{
			large: models.ModelPlacement{Name: large.Name, Device: "gpu", MemoryGB: large.MemoryGB},
			small: models.ModelPlacement{Name: small.Name, Device: "gpu", MemoryGB: small.MemoryGB},
		}
	}

	// Strategy 2: Large on CPU, small on GPU (split)
	if gpuMem > 0 && small.MemoryGB <= gpuMem && large.MemoryGB <= cpuMem {
		return &placementResult{
			large: models.ModelPlacement{Name: large.Name, Device: "cpu", MemoryGB: large.MemoryGB},
			small: models.ModelPlacement{Name: small.Name, Device: "gpu", MemoryGB: small.MemoryGB},
		}
	}

	// Strategy 3: Large on GPU, small on CPU (split, less common)
	if gpuMem > 0 && large.MemoryGB <= gpuMem && small.MemoryGB <= cpuMem {
		return &placementResult{
			large: models.ModelPlacement{Name: large.Name, Device: "gpu", MemoryGB: large.MemoryGB},
			small: models.ModelPlacement{Name: small.Name, Device: "cpu", MemoryGB: small.MemoryGB},
		}
	}

	// Strategy 4: Both on CPU
	if large.MemoryGB+small.MemoryGB <= cpuMem {
		return &placementResult{
			large: models.ModelPlacement{Name: large.Name, Device: "cpu", MemoryGB: large.MemoryGB},
			small: models.ModelPlacement{Name: small.Name, Device: "cpu", MemoryGB: small.MemoryGB},
		}
	}

	return nil // Doesn't fit
}
