package platform

import (
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// GPUInfo describes a detected GPU
type GPUInfo struct {
	Type   string  // "nvidia", "amd", "apple"
	Name   string  // e.g. "RTX 4090"
	VRAMGB float64 // video memory in GB
}

// HardwareInfo describes the detected system hardware
type HardwareInfo struct {
	OS          string    // "darwin", "linux", "windows"
	Arch        string    // "amd64", "arm64"
	SystemRAMGB float64   // total system RAM in GB
	GPUs        []GPUInfo // detected GPUs
}

// DetectHardware probes the system for RAM, GPU, and platform info
func DetectHardware() (*HardwareInfo, error) {
	hw := &HardwareInfo{
		OS:   runtime.GOOS,
		Arch: runtime.GOARCH,
	}

	ram, err := detectRAM()
	if err != nil {
		return nil, fmt.Errorf("failed to detect RAM: %w", err)
	}
	hw.SystemRAMGB = ram

	hw.GPUs = detectGPUs(hw.OS, hw.Arch, hw.SystemRAMGB)

	return hw, nil
}

// TotalGPUMemoryGB returns the sum of all GPU VRAM
func (h *HardwareInfo) TotalGPUMemoryGB() float64 {
	var total float64
	for _, gpu := range h.GPUs {
		total += gpu.VRAMGB
	}
	return total
}

// HasGPU returns true if any GPU was detected
func (h *HardwareInfo) HasGPU() bool {
	return len(h.GPUs) > 0
}

// BestGPU returns the GPU with the most VRAM, or nil if no GPUs.
// Callers should check HasGPU() before using the returned pointer.
func (h *HardwareInfo) BestGPU() *GPUInfo {
	if len(h.GPUs) == 0 {
		return nil
	}
	best := &h.GPUs[0]
	for i := 1; i < len(h.GPUs); i++ {
		if h.GPUs[i].VRAMGB > best.VRAMGB {
			best = &h.GPUs[i]
		}
	}
	return best
}

func detectRAM() (float64, error) {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
		if err != nil {
			return 0, err
		}
		bytes, err := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
		if err != nil {
			return 0, err
		}
		return float64(bytes) / (1024 * 1024 * 1024), nil

	case "linux":
		out, err := exec.Command("grep", "MemTotal", "/proc/meminfo").Output()
		if err != nil {
			return 0, err
		}
		// Format: "MemTotal:       32768000 kB"
		fields := strings.Fields(string(out))
		if len(fields) < 2 {
			return 0, fmt.Errorf("unexpected meminfo format")
		}
		kb, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0, err
		}
		return float64(kb) / (1024 * 1024), nil

	default:
		return 0, fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

func detectGPUs(os, arch string, systemRAMGB float64) []GPUInfo {
	var gpus []GPUInfo

	// Apple Silicon: unified memory
	if os == "darwin" && arch == "arm64" {
		gpus = append(gpus, GPUInfo{
			Type:   "apple",
			Name:   "Apple Silicon",
			VRAMGB: systemRAMGB, // unified memory
		})
		return gpus
	}

	// NVIDIA
	if nvidiaGPUs := detectNVIDIA(); len(nvidiaGPUs) > 0 {
		gpus = append(gpus, nvidiaGPUs...)
	}

	// AMD
	if amdGPUs := detectAMD(); len(amdGPUs) > 0 {
		gpus = append(gpus, amdGPUs...)
	}

	return gpus
}

func detectNVIDIA() []GPUInfo {
	out, err := exec.Command("nvidia-smi",
		"--query-gpu=name,memory.total",
		"--format=csv,noheader,nounits").Output()
	if err != nil {
		return nil // nvidia-smi not available
	}

	var gpus []GPUInfo
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(line, ",", 2)
		if len(parts) != 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		vramMB, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		if err != nil {
			continue
		}
		gpus = append(gpus, GPUInfo{
			Type:   "nvidia",
			Name:   name,
			VRAMGB: vramMB / 1024,
		})
	}
	return gpus
}

func detectAMD() []GPUInfo {
	out, err := exec.Command("rocm-smi", "--showmeminfo", "vram").Output()
	if err != nil {
		return nil // rocm-smi not available
	}

	var gpus []GPUInfo
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "Total Memory") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		vramMB, err := strconv.ParseFloat(fields[3], 64)
		if err != nil {
			continue
		}
		gpus = append(gpus, GPUInfo{
			Type:   "amd",
			Name:   "AMD GPU",
			VRAMGB: vramMB / 1024,
		})
	}
	return gpus
}
