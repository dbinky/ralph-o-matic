package models

import "fmt"

// ModelPlacement describes which model to use and where to run it
type ModelPlacement struct {
	Name     string  `json:"name"`
	Device   string  `json:"device"`    // "gpu", "cpu", or "auto"
	MemoryGB float64 `json:"memory_gb"`
}

// Validate checks that the placement has a name and a valid device
func (mp *ModelPlacement) Validate() error {
	if mp.Name == "" {
		return fmt.Errorf("model name is required")
	}
	switch mp.Device {
	case "", "gpu", "cpu", "auto":
		// valid
	default:
		return fmt.Errorf("device must be gpu, cpu, auto, or empty; got %q", mp.Device)
	}
	return nil
}

// OllamaConfig holds connection settings for the Ollama server
type OllamaConfig struct {
	Host     string `json:"host"`
	IsRemote bool   `json:"is_remote"`
}

// Validate checks that host is set
func (oc *OllamaConfig) Validate() error {
	if oc.Host == "" {
		return fmt.Errorf("ollama host is required")
	}
	return nil
}

// ServerConfig holds server-wide configuration
type ServerConfig struct {
	// Ollama connection
	Ollama OllamaConfig `json:"ollama"`

	// Models
	LargeModel ModelPlacement `json:"large_model"`
	SmallModel ModelPlacement `json:"small_model"`

	// Execution
	DefaultMaxIterations int `json:"default_max_iterations"`
	ConcurrentJobs       int `json:"concurrent_jobs"`

	// Storage
	WorkspaceDir     string `json:"workspace_dir"`
	JobRetentionDays int    `json:"job_retention_days"`

	// Retry behavior
	MaxClaudeRetries  int `json:"max_claude_retries"`
	MaxGitRetries     int `json:"max_git_retries"`
	GitRetryBackoffMs int `json:"git_retry_backoff_ms"`
}

// DefaultServerConfig returns a ServerConfig with sensible defaults
func DefaultServerConfig() *ServerConfig {
	return &ServerConfig{
		Ollama:               OllamaConfig{Host: "http://localhost:11434", IsRemote: false},
		LargeModel:           ModelPlacement{Name: "qwen3-coder:70b", Device: "cpu", MemoryGB: 42},
		SmallModel:           ModelPlacement{Name: "qwen2.5-coder:7b", Device: "gpu", MemoryGB: 5},
		DefaultMaxIterations: 50,
		ConcurrentJobs:       1,
		JobRetentionDays:     30,
		MaxClaudeRetries:     3,
		MaxGitRetries:        3,
		GitRetryBackoffMs:    1000,
	}
}

// Validate checks if the config has valid values
func (c *ServerConfig) Validate() error {
	if err := c.Ollama.Validate(); err != nil {
		return fmt.Errorf("ollama: %w", err)
	}
	if err := c.LargeModel.Validate(); err != nil {
		return fmt.Errorf("large_model: %w", err)
	}
	if err := c.SmallModel.Validate(); err != nil {
		return fmt.Errorf("small_model: %w", err)
	}
	if c.DefaultMaxIterations <= 0 {
		return fmt.Errorf("default_max_iterations must be positive")
	}
	if c.ConcurrentJobs <= 0 {
		return fmt.Errorf("concurrent_jobs must be positive")
	}
	if c.JobRetentionDays < 0 {
		return fmt.Errorf("job_retention_days cannot be negative")
	}
	return nil
}

// Merge returns a new config with non-zero values from updates applied
func (c *ServerConfig) Merge(updates *ServerConfig) *ServerConfig {
	result := *c // Copy

	// Ollama: merge individual fields
	if updates.Ollama.Host != "" {
		result.Ollama.Host = updates.Ollama.Host
	}
	if updates.Ollama.IsRemote {
		result.Ollama.IsRemote = updates.Ollama.IsRemote
	}

	// LargeModel: merge individual fields
	if updates.LargeModel.Name != "" {
		result.LargeModel.Name = updates.LargeModel.Name
	}
	if updates.LargeModel.Device != "" {
		result.LargeModel.Device = updates.LargeModel.Device
	}
	if updates.LargeModel.MemoryGB != 0 {
		result.LargeModel.MemoryGB = updates.LargeModel.MemoryGB
	}

	// SmallModel: merge individual fields
	if updates.SmallModel.Name != "" {
		result.SmallModel.Name = updates.SmallModel.Name
	}
	if updates.SmallModel.Device != "" {
		result.SmallModel.Device = updates.SmallModel.Device
	}
	if updates.SmallModel.MemoryGB != 0 {
		result.SmallModel.MemoryGB = updates.SmallModel.MemoryGB
	}

	if updates.DefaultMaxIterations > 0 {
		result.DefaultMaxIterations = updates.DefaultMaxIterations
	}
	if updates.ConcurrentJobs > 0 {
		result.ConcurrentJobs = updates.ConcurrentJobs
	}
	if updates.WorkspaceDir != "" {
		result.WorkspaceDir = updates.WorkspaceDir
	}
	if updates.JobRetentionDays > 0 {
		result.JobRetentionDays = updates.JobRetentionDays
	}
	if updates.MaxClaudeRetries > 0 {
		result.MaxClaudeRetries = updates.MaxClaudeRetries
	}
	if updates.MaxGitRetries > 0 {
		result.MaxGitRetries = updates.MaxGitRetries
	}
	if updates.GitRetryBackoffMs > 0 {
		result.GitRetryBackoffMs = updates.GitRetryBackoffMs
	}

	return &result
}
