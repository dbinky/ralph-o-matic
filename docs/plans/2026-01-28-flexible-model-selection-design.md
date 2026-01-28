# Flexible Model Selection Design

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace hardcoded Ollama model configuration with smart model selection based on hardware detection, support local and remote Ollama, and allow per-model device placement (GPU vs CPU).

**Branch:** `dev-fexible-model-selection`

---

## Overview

The current system hardcodes `qwen3-coder:70b` and `qwen2.5-coder:7b` with a localhost-only Ollama connection. This design introduces:

1. A **models catalog** (`models.yaml`) defining known-good coding models with memory requirements and quality scores
2. **Hardware detection** that enumerates system RAM and GPU VRAM
3. A **selection algorithm** that finds the optimal (large, small) model pairing across all available devices
4. An **Ollama client** for managing models on local or remote Ollama instances
5. **ServerConfig changes** to track model placement and Ollama connection info
6. **Installer updates** that present an ideal configuration and let users customize

---

## Section 1: Model Catalog File

Ship a `models.yaml` at the repo root, embedded into the binary via `//go:embed`. Can be overridden by placing a `models.yaml` in the user's config directory.

```yaml
# models.yaml
models:
  - name: "qwen3-coder:70b"
    memory_gb: 42
    role: large
    quality: 10
    description: "Best coding performance"

  - name: "qwen2.5-coder:32b"
    memory_gb: 20
    role: large
    quality: 8
    description: "Strong coding, much smaller footprint"

  - name: "qwen2.5-coder:14b"
    memory_gb: 10
    role: large
    quality: 6
    description: "Good coding capability"

  - name: "qwen2.5-coder:7b"
    memory_gb: 5
    role: both
    quality: 4
    description: "Decent coding, fast inference"

  - name: "qwen2.5-coder:1.5b"
    memory_gb: 1.5
    role: small
    quality: 2
    description: "Lightweight helper only"
```

Fields:
- `name` - Ollama model tag
- `memory_gb` - Expected memory footprint when loaded
- `role` - `large`, `small`, or `both` (can serve either role)
- `quality` - Relative ranking (higher = better), used by selection algorithm
- `description` - Human-readable summary for installer display

---

## Section 2: Hardware Detection & Smart Model Placement

The installer detects all available compute resources:

1. **System RAM** - Total physical memory
2. **GPU(s)** - Type (NVIDIA via nvidia-smi, AMD via rocm-smi, Apple Silicon unified memory) and VRAM per device

Then presents a choice flow:

```
Detected hardware:
  System RAM: 48 GB
  GPU: NVIDIA RTX 3070 (8 GB VRAM)

How would you like to run models?
  1. GPU + CPU split [Recommended]
  2. GPU only (8 GB available)
  3. CPU only (48 GB available)
  4. Skip (remote Ollama)
```

The selection algorithm considers all available resources holistically and computes the **ideal configuration** - the highest-quality model pairing that fits across all devices. This includes split configurations where the large model runs on CPU and the small model runs on GPU (or vice versa).

Example output for 8GB GPU + 48GB RAM:

```
Recommended configuration:
  Large model: qwen3-coder:70b (42 GB) -> CPU
  Small model: qwen2.5-coder:7b (5 GB) -> GPU
  Reasoning: Large model exceeds GPU VRAM, runs well on 48GB RAM.
             Small model fits on GPU for fast responses.

  Accept this configuration? [Y/n/customize]
```

If user picks "customize":

```
Available models:
  #  Model                  Memory   Best fit
  1. qwen3-coder:70b        42 GB    CPU only
  2. qwen2.5-coder:32b      20 GB    CPU only
  3. qwen2.5-coder:14b      10 GB    CPU only
  4. qwen2.5-coder:7b        5 GB    GPU or CPU
  5. qwen2.5-coder:1.5b    1.5 GB    GPU or CPU

Select large model [1-5]:
Select small model [1-5]:
```

The "Best fit" column is computed per-model based on the user's actual hardware. Placement logic: prefer GPU when it fits (faster inference), fall back to CPU.

---

## Section 3: Selection Algorithm

Instead of rigid tiers, the algorithm scores all valid configurations:

1. Enumerate all valid (large, small) pairs from the catalog (large must have `role: large` or `both`, small must have `role: small` or `both`)
2. For each pair, find the best placement across available devices (GPU, CPU) where both models fit simultaneously
3. Score each configuration by `large.quality + small.quality`
4. Return the top configuration as the recommendation, plus 2-3 ranked alternatives

Placement rules for each pair:
- Try both models on GPU first (if both fit in VRAM)
- Try small on GPU + large on CPU (split)
- Try large on GPU + small on CPU (split)
- Try both on CPU (if both fit in RAM)
- If no placement works, skip this pair

Example for 8GB GPU + 48GB RAM:
- Pair (70b, 7b): 70b on CPU (42 < 48), 7b on GPU (5 < 8) -> score 14
- Pair (32b, 7b): 32b on CPU (20 < 48), 7b on GPU (5 < 8) -> score 12
- Pair (14b, 7b): 14b on CPU (10 < 48), 7b on GPU (5 < 8) -> score 10

Algorithm picks 70b+7b split as ideal.

---

## Section 4: ServerConfig Changes

`ServerConfig` changes from flat strings to structured types:

```go
type ModelPlacement struct {
    Name     string  `json:"name"`      // e.g. "qwen3-coder:70b"
    Device   string  `json:"device"`    // "gpu", "cpu", or "auto"
    MemoryGB float64 `json:"memory_gb"` // expected memory footprint
}

type OllamaConfig struct {
    Host     string `json:"host"`      // "http://localhost:11434" or remote URL
    IsRemote bool   `json:"is_remote"` // skip local install/management if true
}
```

`ServerConfig` fields change from:

```go
LargeModel string `json:"large_model"`
SmallModel string `json:"small_model"`
```

To:

```go
Ollama     OllamaConfig  `json:"ollama"`
LargeModel ModelPlacement `json:"large_model"`
SmallModel ModelPlacement `json:"small_model"`
```

Default config:

```go
Ollama:     OllamaConfig{Host: "http://localhost:11434", IsRemote: false},
LargeModel: ModelPlacement{Name: "qwen3-coder:70b", Device: "cpu", MemoryGB: 42},
SmallModel: ModelPlacement{Name: "qwen2.5-coder:7b", Device: "gpu", MemoryGB: 5},
```

This is a breaking change. All code referencing `config.LargeModel` as a string must update to `config.LargeModel.Name`.

---

## Section 5: Ollama Client

New `internal/platform/ollama.go` talks to the Ollama REST API:

```go
type OllamaClient struct {
    host string
}
```

Operations:
- **Ping** - `GET /api/tags` - verify Ollama is reachable
- **List models** - `GET /api/tags` - return what's already pulled with sizes
- **Pull model** - `POST /api/pull` - download a model (used during install and upgrades)
- **Load model with placement** - `POST /api/generate` with `keep_alive` and device hints - preload onto the right device

For **remote Ollama**, the same client works with a different host. The installer flow:

```
Ollama setup:
  1. Install locally [Recommended for new setups]
  2. Use remote Ollama

> 2

Remote Ollama URL: http://192.168.1.50:11434

Connecting... OK
Available models on remote:
  - qwen3-coder:70b (42 GB)
  - qwen2.5-coder:7b (5 GB)
  - llama3:8b (5 GB)

Recommended configuration:
  Large model: qwen3-coder:70b -> remote
  Small model: qwen2.5-coder:7b -> remote

Missing recommended models: none

Accept? [Y/n/customize]
```

If models are missing from the remote, it offers to pull them (with user confirmation, since it's someone else's server).

---

## Section 6: File Layout

```
models.yaml                          # Embedded model catalog (repo root)

internal/platform/
  ollama.go                          # Ollama REST API client
  ollama_test.go
  hardware.go                        # Hardware detection (RAM, GPU, VRAM)
  hardware_test.go
  selector.go                        # Model selection algorithm
  selector_test.go

internal/models/
  config.go                          # Updated ServerConfig (breaking change)
  config_test.go

internal/executor/
  claude.go                          # Updated BuildEnv for new config shape
  claude_test.go

internal/api/
  config.go                          # Config endpoints pass through new fields

scripts/
  install.sh                         # Rewritten model selection flow
  install.ps1                        # Same for Windows
```

---

## Section 7: Testing Strategy

All code is test-driven. Tests are written before implementation for each component.

### Hardware Detection Tests (`hardware_test.go`)

**Happy path:**
- Detect system RAM correctly on current platform
- Detect Apple Silicon unified memory reports as both RAM and GPU

**Success scenarios:**
- NVIDIA GPU detected via nvidia-smi, VRAM parsed correctly
- AMD GPU detected via rocm-smi, VRAM parsed correctly
- Multiple GPUs detected, each with separate VRAM reported
- No GPU detected, returns empty GPU list with system RAM only

**Failure scenarios:**
- nvidia-smi not installed, GPU detection gracefully skips NVIDIA
- rocm-smi returns error, GPU detection gracefully skips AMD
- GPU command returns unparseable output, skips with warning

**Edge cases:**
- Machine with 0 GPUs (cloud VM, WSL without passthrough)
- Machine with multiple GPU types (NVIDIA + integrated Intel)
- System RAM below minimum (e.g. 4GB) - still returns result, selection handles rejection

### Model Catalog Tests (`selector_test.go`)

**Happy path:**
- Load models.yaml, parse all fields correctly
- Embedded default loads when no override file exists

**Success scenarios:**
- Override models.yaml from config directory takes precedence over embedded
- Unknown fields in YAML are ignored (forward compatibility)

**Failure scenarios:**
- Malformed YAML returns parse error
- Missing required fields (name, memory_gb, role) return validation error
- Empty models list returns error

**Edge cases:**
- Model with `role: both` appears as candidate for large and small
- Duplicate model names in catalog returns error

### Selection Algorithm Tests (`selector_test.go`)

**Happy path:**
- 48GB RAM + 8GB GPU -> selects 70b on CPU + 7b on GPU (score 14)
- 24GB unified (Apple Silicon) -> selects 14b + 7b, total 15GB fits

**Success scenarios:**
- GPU-only config: 48GB VRAM, no preference -> both models on GPU
- CPU-only config: 64GB RAM, no GPU -> both models on CPU
- Split config: small GPU + large RAM -> large on CPU, small on GPU
- Returns top recommendation plus 2-3 ranked alternatives
- `Device: "auto"` resolved to actual device in recommendation

**Failure scenarios:**
- No valid configuration fits (4GB RAM, no GPU) -> returns error with minimum requirements message
- Large model fits nowhere -> skipped, next pair tried
- All pairs exceed available memory -> clear error listing smallest viable option

**Error scenarios:**
- Zero available memory (detection failed) -> error, don't recommend anything
- Negative memory values -> validation rejects before selection runs

**Edge cases:**
- Both models exactly equal available memory (tight fit) -> recommend with warning
- Both models want GPU but only one fits -> second model falls back to CPU
- Same model used as both large and small (e.g. 7b for both on tiny hardware)
- Single model in catalog -> only one possible pair, selected or error
- Two pairs with identical scores -> stable sort, prefer larger large model

### Ollama Client Tests (`ollama_test.go`)

**Happy path:**
- Ping reachable Ollama returns nil error
- List models returns parsed model names and sizes
- Pull model streams progress and completes

**Success scenarios:**
- Remote Ollama at custom URL works identically to localhost
- Model already pulled, pull is a no-op (returns immediately)
- List models on server with 20+ models returns all of them

**Failure scenarios:**
- Ping unreachable host returns connection error with URL in message
- Pull model that doesn't exist in Ollama registry returns not-found error
- Network timeout during pull returns timeout error (not hang)

**Error scenarios:**
- Ollama returns HTTP 500 -> wrapped error with response body
- Ollama returns malformed JSON -> parse error, not panic
- Connection refused -> clear "is Ollama running?" message

**Edge cases:**
- Ollama URL with trailing slash (normalized)
- Ollama URL without scheme (auto-prepend http://)
- Very slow pull (multi-GB model) respects context cancellation

### ServerConfig Tests (`config_test.go`)

**Happy path:**
- DefaultServerConfig returns ModelPlacement structs with correct defaults
- Validate passes with complete OllamaConfig and ModelPlacement
- JSON round-trip preserves all new fields

**Success scenarios:**
- Merge updates LargeModel.Name without clobbering Device
- Merge updates Ollama.Host without clobbering IsRemote

**Failure scenarios:**
- Empty model name in ModelPlacement fails validation
- Invalid device value (not "gpu", "cpu", "auto") fails validation
- Empty Ollama host fails validation

**Edge cases:**
- Merge with zero-value ModelPlacement (all empty) changes nothing
- Device set to "gpu" but no GPU available -> validation passes (runtime concern, not config concern)

### Executor Integration Tests (`claude_test.go`)

**Happy path:**
- BuildEnv uses `config.Ollama.Host` for ANTHROPIC_BASE_URL
- BuildEnv uses `config.LargeModel.Name` for ANTHROPIC_MODEL
- BuildEnv uses `config.SmallModel.Name` for ANTHROPIC_DEFAULT_HAIKU_MODEL

**Failure scenarios:**
- Config with empty Ollama.Host -> BuildEnv returns error (not silently empty env var)

### Installer Tests (`scripts/tests/`)

**Happy path:**
- Full install flow with local Ollama on well-resourced machine
- Client-only install skips model selection entirely

**Success scenarios:**
- Remote Ollama flow: URL entered -> ping succeeds -> models listed -> recommendation shown
- `--yes` flag auto-accepts recommended configuration
- Custom model selection overrides recommendation correctly

**Failure scenarios:**
- Remote Ollama unreachable -> clear error, offer to retry or switch to local
- Model pull fails mid-download -> error with retry option
- Insufficient memory for any configuration -> lists minimum requirements and exits

**Edge cases:**
- Re-running installer with existing config -> detects current config, offers to keep or reconfigure
- Remote Ollama has custom/fine-tuned models not in catalog -> shown in list but not auto-recommended
