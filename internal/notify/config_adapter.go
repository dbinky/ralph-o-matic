package notify

import "github.com/ryan/ralph-o-matic/internal/models"

// ServerConfigGetter can retrieve the full server config.
// This is satisfied by *db.ConfigRepo.
type ServerConfigGetter interface {
	Get() (*models.ServerConfig, error)
}

// ConfigRepoAdapter extracts NotifyConfig from a full ServerConfig provider.
type ConfigRepoAdapter struct {
	getter ServerConfigGetter
}

// NewConfigRepoAdapter wraps a ServerConfigGetter to provide notify config.
func NewConfigRepoAdapter(getter ServerConfigGetter) *ConfigRepoAdapter {
	return &ConfigRepoAdapter{getter: getter}
}

// GetNotifyConfig returns the notification portion of the server config.
func (a *ConfigRepoAdapter) GetNotifyConfig() (*models.NotifyConfig, error) {
	cfg, err := a.getter.Get()
	if err != nil {
		return nil, err
	}
	return &cfg.Notify, nil
}
