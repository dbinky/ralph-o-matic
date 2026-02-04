package models

import (
	"encoding/json"
	"fmt"
)

// Backend identifies which AI provider to use
type Backend string

const (
	BackendOllama    Backend = "ollama"
	BackendAnthropic Backend = "anthropic"
)

// Valid returns true for known backends and empty (which means "use default")
func (b Backend) Valid() bool {
	switch b {
	case "", BackendOllama, BackendAnthropic:
		return true
	default:
		return false
	}
}

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

// AnthropicConfig holds settings for the Anthropic API backend
type AnthropicConfig struct {
	APIKey     string `json:"api_key,omitempty"`
	LargeModel string `json:"large_model"`
	SmallModel string `json:"small_model"`
}

// Validate checks that model names are set
func (ac *AnthropicConfig) Validate() error {
	if ac.LargeModel == "" {
		return fmt.Errorf("large_model is required")
	}
	if ac.SmallModel == "" {
		return fmt.Errorf("small_model is required")
	}
	return nil
}

// SMTPConfig holds SMTP notification settings
type SMTPConfig struct {
	Enabled    bool     `json:"enabled"`
	Host       string   `json:"host"`
	Port       int      `json:"port"`
	Username   string   `json:"username"`
	Password   string   `json:"password"`
	From       string   `json:"from"`
	Recipients []string `json:"recipients"`
}

// TeamsConfig holds Microsoft Teams webhook notification settings
type TeamsConfig struct {
	Enabled    bool   `json:"enabled"`
	WebhookURL string `json:"webhook_url"`
}

// NotifyConfig holds notification configuration
type NotifyConfig struct {
	SMTP  SMTPConfig  `json:"smtp"`
	Teams TeamsConfig `json:"teams"`
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

	// Backend
	DefaultBackend Backend         `json:"default_backend"`
	Anthropic      AnthropicConfig `json:"anthropic"`

	// Retry behavior
	MaxClaudeRetries  int `json:"max_claude_retries"`
	MaxGitRetries     int `json:"max_git_retries"`
	GitRetryBackoffMs int `json:"git_retry_backoff_ms"`

	// Notifications
	Notify NotifyConfig `json:"notify"`
}

// DefaultServerConfig returns a ServerConfig with sensible defaults
func DefaultServerConfig() *ServerConfig {
	return &ServerConfig{
		Ollama:               OllamaConfig{Host: "http://localhost:11434", IsRemote: false},
		LargeModel:           ModelPlacement{Name: "devstral", Device: "cpu", MemoryGB: 15},
		SmallModel:           ModelPlacement{Name: "qwen3:8b", Device: "gpu", MemoryGB: 5.2},
		DefaultBackend: BackendOllama,
		Anthropic: AnthropicConfig{
			LargeModel: "claude-opus-4-5-20251101",
			SmallModel: "claude-haiku-4-5-20251001",
		},
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
	if !c.DefaultBackend.Valid() {
		return fmt.Errorf("invalid default_backend: %q", c.DefaultBackend)
	}
	if c.DefaultBackend == BackendAnthropic {
		if err := c.Anthropic.Validate(); err != nil {
			return fmt.Errorf("anthropic: %w", err)
		}
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

	// DefaultBackend
	if updates.DefaultBackend != "" {
		result.DefaultBackend = updates.DefaultBackend
	}

	// Anthropic: merge individual fields
	if updates.Anthropic.APIKey != "" {
		result.Anthropic.APIKey = updates.Anthropic.APIKey
	}
	if updates.Anthropic.LargeModel != "" {
		result.Anthropic.LargeModel = updates.Anthropic.LargeModel
	}
	if updates.Anthropic.SmallModel != "" {
		result.Anthropic.SmallModel = updates.Anthropic.SmallModel
	}

	// Notify: merge individual fields
	if updates.Notify.SMTP.Host != "" {
		result.Notify.SMTP.Host = updates.Notify.SMTP.Host
	}
	if updates.Notify.SMTP.Port != 0 {
		result.Notify.SMTP.Port = updates.Notify.SMTP.Port
	}
	if updates.Notify.SMTP.Username != "" {
		result.Notify.SMTP.Username = updates.Notify.SMTP.Username
	}
	if updates.Notify.SMTP.Password != "" {
		result.Notify.SMTP.Password = updates.Notify.SMTP.Password
	}
	if updates.Notify.SMTP.From != "" {
		result.Notify.SMTP.From = updates.Notify.SMTP.From
	}
	if len(updates.Notify.SMTP.Recipients) > 0 {
		result.Notify.SMTP.Recipients = updates.Notify.SMTP.Recipients
	}
	if updates.Notify.SMTP.Enabled {
		result.Notify.SMTP.Enabled = true
	}
	if updates.Notify.Teams.WebhookURL != "" {
		result.Notify.Teams.WebhookURL = updates.Notify.Teams.WebhookURL
	}
	if updates.Notify.Teams.Enabled {
		result.Notify.Teams.Enabled = true
	}

	return &result
}

// MergeJSON applies a partial JSON update to the config, correctly handling
// zero values (false, 0) by checking which fields are actually present in the
// raw JSON rather than relying on Go zero-value detection.
func (c *ServerConfig) MergeJSON(raw json.RawMessage) (*ServerConfig, error) {
	// First, do the standard merge for non-zero fields
	var updates ServerConfig
	if err := json.Unmarshal(raw, &updates); err != nil {
		return nil, err
	}
	result := c.Merge(&updates)

	// Now check for explicitly-set zero-value fields using a raw map
	var rawMap map[string]json.RawMessage
	if err := json.Unmarshal(raw, &rawMap); err != nil {
		return nil, err
	}

	if ollamaRaw, ok := rawMap["ollama"]; ok {
		var ollamaMap map[string]json.RawMessage
		if err := json.Unmarshal(ollamaRaw, &ollamaMap); err == nil {
			if _, ok := ollamaMap["is_remote"]; ok {
				result.Ollama.IsRemote = updates.Ollama.IsRemote
			}
		}
	}

	if lmRaw, ok := rawMap["large_model"]; ok {
		var lmMap map[string]json.RawMessage
		if err := json.Unmarshal(lmRaw, &lmMap); err == nil {
			if _, ok := lmMap["memory_gb"]; ok {
				result.LargeModel.MemoryGB = updates.LargeModel.MemoryGB
			}
		}
	}

	if smRaw, ok := rawMap["small_model"]; ok {
		var smMap map[string]json.RawMessage
		if err := json.Unmarshal(smRaw, &smMap); err == nil {
			if _, ok := smMap["memory_gb"]; ok {
				result.SmallModel.MemoryGB = updates.SmallModel.MemoryGB
			}
		}
	}

	if _, ok := rawMap["default_backend"]; ok {
		result.DefaultBackend = updates.DefaultBackend
	}

	if anthropicRaw, ok := rawMap["anthropic"]; ok {
		var anthropicMap map[string]json.RawMessage
		if err := json.Unmarshal(anthropicRaw, &anthropicMap); err == nil {
			if _, ok := anthropicMap["api_key"]; ok {
				result.Anthropic.APIKey = updates.Anthropic.APIKey
			}
			if _, ok := anthropicMap["large_model"]; ok {
				result.Anthropic.LargeModel = updates.Anthropic.LargeModel
			}
			if _, ok := anthropicMap["small_model"]; ok {
				result.Anthropic.SmallModel = updates.Anthropic.SmallModel
			}
		}
	}

	if notifyRaw, ok := rawMap["notify"]; ok {
		var notifyMap map[string]json.RawMessage
		if err := json.Unmarshal(notifyRaw, &notifyMap); err == nil {
			if smtpRaw, ok := notifyMap["smtp"]; ok {
				var smtpMap map[string]json.RawMessage
				if err := json.Unmarshal(smtpRaw, &smtpMap); err == nil {
					if _, ok := smtpMap["enabled"]; ok {
						result.Notify.SMTP.Enabled = updates.Notify.SMTP.Enabled
					}
					if _, ok := smtpMap["port"]; ok {
						result.Notify.SMTP.Port = updates.Notify.SMTP.Port
					}
				}
			}
			if teamsRaw, ok := notifyMap["teams"]; ok {
				var teamsMap map[string]json.RawMessage
				if err := json.Unmarshal(teamsRaw, &teamsMap); err == nil {
					if _, ok := teamsMap["enabled"]; ok {
						result.Notify.Teams.Enabled = updates.Notify.Teams.Enabled
					}
				}
			}
		}
	}

	return result, nil
}
