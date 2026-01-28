package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/ryan/ralph-o-matic/internal/models"
)

// ConfigRepo handles config persistence
type ConfigRepo struct {
	db *DB
}

// NewConfigRepo creates a new config repository
func NewConfigRepo(db *DB) *ConfigRepo {
	return &ConfigRepo{db: db}
}

// Get retrieves the current config (or defaults if not set)
func (r *ConfigRepo) Get() (*models.ServerConfig, error) {
	cfg := models.DefaultServerConfig()

	rows, err := r.db.conn.Query("SELECT key, value FROM config")
	if err != nil {
		return nil, fmt.Errorf("failed to query config: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("failed to scan config row: %w", err)
		}

		if err := applyConfigValue(cfg, key, value); err != nil {
			// Log but don't fail on unknown keys
			continue
		}
	}

	return cfg, nil
}

// Save persists the entire config
func (r *ConfigRepo) Save(cfg *models.ServerConfig) error {
	// Serialize structured fields as JSON
	largeModelJSON, err := json.Marshal(cfg.LargeModel)
	if err != nil {
		return fmt.Errorf("failed to marshal large_model: %w", err)
	}
	smallModelJSON, err := json.Marshal(cfg.SmallModel)
	if err != nil {
		return fmt.Errorf("failed to marshal small_model: %w", err)
	}
	ollamaJSON, err := json.Marshal(cfg.Ollama)
	if err != nil {
		return fmt.Errorf("failed to marshal ollama: %w", err)
	}

	values := map[string]string{
		"large_model":            string(largeModelJSON),
		"small_model":            string(smallModelJSON),
		"ollama":                 string(ollamaJSON),
		"default_max_iterations": strconv.Itoa(cfg.DefaultMaxIterations),
		"concurrent_jobs":        strconv.Itoa(cfg.ConcurrentJobs),
		"workspace_dir":          cfg.WorkspaceDir,
		"job_retention_days":     strconv.Itoa(cfg.JobRetentionDays),
		"max_claude_retries":     strconv.Itoa(cfg.MaxClaudeRetries),
		"max_git_retries":        strconv.Itoa(cfg.MaxGitRetries),
		"git_retry_backoff_ms":   strconv.Itoa(cfg.GitRetryBackoffMs),
	}

	tx, err := r.db.conn.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	for key, value := range values {
		_, err := tx.Exec(`
			INSERT INTO config (key, value, updated_at)
			VALUES (?, ?, CURRENT_TIMESTAMP)
			ON CONFLICT(key) DO UPDATE SET value = ?, updated_at = CURRENT_TIMESTAMP
		`, key, value, value)
		if err != nil {
			return fmt.Errorf("failed to save config key %s: %w", key, err)
		}
	}

	return tx.Commit()
}

// Update sets a single config value
func (r *ConfigRepo) Update(key, value string) error {
	_, err := r.db.conn.Exec(`
		INSERT INTO config (key, value, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET value = ?, updated_at = CURRENT_TIMESTAMP
	`, key, value, value)
	if err != nil {
		return fmt.Errorf("failed to update config key %s: %w", key, err)
	}
	return nil
}

// GetKey retrieves a single config value
func (r *ConfigRepo) GetKey(key string) (string, error) {
	var value string
	err := r.db.conn.QueryRow("SELECT value FROM config WHERE key = ?", key).Scan(&value)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("failed to get config key %s: %w", key, err)
	}
	return value, nil
}

// applyConfigValue sets a config field from a string value
func applyConfigValue(cfg *models.ServerConfig, key, value string) error {
	switch key {
	case "large_model":
		var mp models.ModelPlacement
		if err := json.Unmarshal([]byte(value), &mp); err != nil {
			// Backwards compatibility: treat as plain model name
			cfg.LargeModel.Name = value
			return nil
		}
		cfg.LargeModel = mp
	case "small_model":
		var mp models.ModelPlacement
		if err := json.Unmarshal([]byte(value), &mp); err != nil {
			// Backwards compatibility: treat as plain model name
			cfg.SmallModel.Name = value
			return nil
		}
		cfg.SmallModel = mp
	case "ollama":
		var oc models.OllamaConfig
		if err := json.Unmarshal([]byte(value), &oc); err != nil {
			return err
		}
		cfg.Ollama = oc
	case "default_max_iterations":
		v, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		cfg.DefaultMaxIterations = v
	case "concurrent_jobs":
		v, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		cfg.ConcurrentJobs = v
	case "workspace_dir":
		cfg.WorkspaceDir = value
	case "job_retention_days":
		v, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		cfg.JobRetentionDays = v
	case "max_claude_retries":
		v, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		cfg.MaxClaudeRetries = v
	case "max_git_retries":
		v, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		cfg.MaxGitRetries = v
	case "git_retry_backoff_ms":
		v, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		cfg.GitRetryBackoffMs = v
	default:
		return fmt.Errorf("unknown config key: %s", key)
	}
	return nil
}
