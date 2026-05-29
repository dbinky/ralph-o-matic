package models

// AnthropicConfigResponse is the Anthropic config returned by the API.
type AnthropicConfigResponse struct {
	LargeModel string `json:"large_model"`
	SmallModel string `json:"small_model"`
}

// OpenRouterConfigResponse is the OpenRouter config returned by the API.
// The API key is replaced with a boolean indicating whether one is set.
type OpenRouterConfigResponse struct {
	BaseURL    string `json:"base_url"`
	LargeModel string `json:"large_model"`
	SmallModel string `json:"small_model"`
	APIKeySet  bool   `json:"api_key_set"`
}

// SMTPConfigResponse is the redacted SMTP config returned by the API.
// The password is replaced with a boolean indicating whether one is set.
type SMTPConfigResponse struct {
	Enabled     bool     `json:"enabled"`
	Host        string   `json:"host"`
	Port        int      `json:"port"`
	Username    string   `json:"username"`
	PasswordSet bool     `json:"password_set"`
	From        string   `json:"from"`
	Recipients  []string `json:"recipients"`
}

// TeamsConfigResponse is the redacted Teams config returned by the API.
// The webhook URL is replaced with a boolean indicating whether one is set.
type TeamsConfigResponse struct {
	Enabled       bool `json:"enabled"`
	WebhookURLSet bool `json:"webhook_url_set"`
}

// NotifyConfigResponse is the redacted notification config returned by the API.
type NotifyConfigResponse struct {
	SMTP  SMTPConfigResponse  `json:"smtp"`
	Teams TeamsConfigResponse `json:"teams"`
}

// ServerConfigResponse is the redacted server config returned by GET /api/config.
// It differs from ServerConfig in that sensitive fields (API keys, passwords,
// webhook URLs) are replaced with boolean indicators.
type ServerConfigResponse struct {
	Ollama                OllamaConfig             `json:"ollama"`
	LargeModel            ModelPlacement           `json:"large_model"`
	SmallModel            ModelPlacement           `json:"small_model"`
	DefaultMaxIterations  int                      `json:"default_max_iterations"`
	WorkspaceDir          string                   `json:"workspace_dir,omitempty"`
	JobRetentionDays      int                      `json:"job_retention_days"`
	DefaultBackend        Backend                  `json:"default_backend"`
	Anthropic             AnthropicConfigResponse  `json:"anthropic"`
	OpenRouter            OpenRouterConfigResponse `json:"openrouter"`
	MaxClaudeRetries      int                      `json:"max_claude_retries"`
	MaxGitRetries         int                      `json:"max_git_retries"`
	GitRetryBackoffMs     int                      `json:"git_retry_backoff_ms"`
	Notify                NotifyConfigResponse     `json:"notify"`
	PostCompletionCommand string                   `json:"post_completion_command"`
	Disable1MContext      bool                     `json:"disable_1m_context"`
}

// NewServerConfigResponse builds a redacted response from a full ServerConfig.
func NewServerConfigResponse(cfg *ServerConfig) *ServerConfigResponse {
	return &ServerConfigResponse{
		Ollama:               cfg.Ollama,
		LargeModel:           cfg.LargeModel,
		SmallModel:           cfg.SmallModel,
		DefaultMaxIterations: cfg.DefaultMaxIterations,
		WorkspaceDir:         cfg.WorkspaceDir,
		JobRetentionDays:     cfg.JobRetentionDays,
		DefaultBackend:       cfg.DefaultBackend,
		Anthropic: AnthropicConfigResponse{
			LargeModel: cfg.Anthropic.LargeModel,
			SmallModel: cfg.Anthropic.SmallModel,
		},
		OpenRouter: OpenRouterConfigResponse{
			BaseURL:    cfg.OpenRouter.BaseURL,
			LargeModel: cfg.OpenRouter.LargeModel,
			SmallModel: cfg.OpenRouter.SmallModel,
			APIKeySet:  cfg.OpenRouter.APIKey != "",
		},
		MaxClaudeRetries:      cfg.MaxClaudeRetries,
		MaxGitRetries:         cfg.MaxGitRetries,
		GitRetryBackoffMs:     cfg.GitRetryBackoffMs,
		PostCompletionCommand: cfg.PostCompletionCommand,
		Disable1MContext:      cfg.Disable1MContext,
		Notify: NotifyConfigResponse{
			SMTP: SMTPConfigResponse{
				Enabled:     cfg.Notify.SMTP.Enabled,
				Host:        cfg.Notify.SMTP.Host,
				Port:        cfg.Notify.SMTP.Port,
				Username:    cfg.Notify.SMTP.Username,
				PasswordSet: cfg.Notify.SMTP.Password != "",
				From:        cfg.Notify.SMTP.From,
				Recipients:  cfg.Notify.SMTP.Recipients,
			},
			Teams: TeamsConfigResponse{
				Enabled:       cfg.Notify.Teams.Enabled,
				WebhookURLSet: cfg.Notify.Teams.WebhookURL != "",
			},
		},
	}
}
