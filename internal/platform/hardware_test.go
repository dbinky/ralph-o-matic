package platform

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectHardware(t *testing.T) {
	hw, err := DetectHardware()
	require.NoError(t, err)

	// Should always detect some RAM
	assert.Greater(t, hw.SystemRAMGB, 0.0)
}

func TestDetectHardware_HasPlatformInfo(t *testing.T) {
	hw, err := DetectHardware()
	require.NoError(t, err)

	assert.NotEmpty(t, hw.OS)
	assert.NotEmpty(t, hw.Arch)
}

func TestDetectHardware_AppleSilicon(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("Apple Silicon test only runs on darwin/arm64")
	}

	hw, err := DetectHardware()
	require.NoError(t, err)

	// Apple Silicon should report unified memory as GPU
	require.Len(t, hw.GPUs, 1)
	assert.Equal(t, "apple", hw.GPUs[0].Type)
	assert.Greater(t, hw.GPUs[0].VRAMGB, 0.0)
	// Unified memory: GPU VRAM should equal system RAM
	assert.Equal(t, hw.SystemRAMGB, hw.GPUs[0].VRAMGB)
}

func TestHardwareInfo_TotalGPUMemory(t *testing.T) {
	hw := &HardwareInfo{
		SystemRAMGB: 64,
		GPUs: []GPUInfo{
			{Type: "nvidia", Name: "RTX 4090", VRAMGB: 24},
			{Type: "nvidia", Name: "RTX 3090", VRAMGB: 24},
		},
	}

	assert.Equal(t, 48.0, hw.TotalGPUMemoryGB())
}

func TestHardwareInfo_TotalGPUMemory_NoGPU(t *testing.T) {
	hw := &HardwareInfo{
		SystemRAMGB: 32,
		GPUs:        nil,
	}

	assert.Equal(t, 0.0, hw.TotalGPUMemoryGB())
}

func TestHardwareInfo_HasGPU(t *testing.T) {
	t.Run("with GPU", func(t *testing.T) {
		hw := &HardwareInfo{GPUs: []GPUInfo{{Type: "nvidia", VRAMGB: 8}}}
		assert.True(t, hw.HasGPU())
	})

	t.Run("without GPU", func(t *testing.T) {
		hw := &HardwareInfo{GPUs: nil}
		assert.False(t, hw.HasGPU())
	})
}

func TestHardwareInfo_BestGPU(t *testing.T) {
	hw := &HardwareInfo{
		GPUs: []GPUInfo{
			{Type: "nvidia", Name: "RTX 3070", VRAMGB: 8},
			{Type: "nvidia", Name: "RTX 4090", VRAMGB: 24},
		},
	}

	best := hw.BestGPU()
	require.NotNil(t, best)
	assert.Equal(t, "RTX 4090", best.Name)
	assert.Equal(t, 24.0, best.VRAMGB)
}

func TestHardwareInfo_BestGPU_NoGPU(t *testing.T) {
	hw := &HardwareInfo{GPUs: nil}
	assert.Nil(t, hw.BestGPU())
}
