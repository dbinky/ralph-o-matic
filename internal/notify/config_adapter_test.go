package notify

import (
	"testing"

	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockServerConfigGetter implements the interface for testing
type mockServerConfigGetter struct {
	config *models.ServerConfig
	err    error
}

func (m *mockServerConfigGetter) Get() (*models.ServerConfig, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.config, nil
}

func TestConfigRepoAdapter_GetNotifyConfig(t *testing.T) {
	cfg := models.DefaultServerConfig()
	cfg.Notify.SMTP.Enabled = true
	cfg.Notify.SMTP.Host = "smtp.example.com"

	adapter := NewConfigRepoAdapter(&mockServerConfigGetter{config: cfg})

	nc, err := adapter.GetNotifyConfig()
	require.NoError(t, err)
	assert.True(t, nc.SMTP.Enabled)
	assert.Equal(t, "smtp.example.com", nc.SMTP.Host)
}

func TestConfigRepoAdapter_GetNotifyConfig_Error(t *testing.T) {
	adapter := NewConfigRepoAdapter(&mockServerConfigGetter{err: assert.AnError})

	_, err := adapter.GetNotifyConfig()
	assert.Error(t, err)
}

func TestConfigRepoAdapter_GetNotifyConfig_DefaultConfig(t *testing.T) {
	cfg := models.DefaultServerConfig()
	adapter := NewConfigRepoAdapter(&mockServerConfigGetter{config: cfg})

	nc, err := adapter.GetNotifyConfig()
	require.NoError(t, err)
	assert.False(t, nc.SMTP.Enabled)
	assert.False(t, nc.Teams.Enabled)
}
