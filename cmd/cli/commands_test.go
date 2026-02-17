package main

import (
	"testing"

	"github.com/ryan/ralph-o-matic/internal/cli"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildNestedMap(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		value    interface{}
		expected map[string]interface{}
	}{
		{
			name:     "flat key",
			key:      "default_max_iterations",
			value:    50,
			expected: map[string]interface{}{"default_max_iterations": 50},
		},
		{
			name:  "two-level dotted key",
			key:   "large_model.name",
			value: "devstral",
			expected: map[string]interface{}{
				"large_model": map[string]interface{}{
					"name": "devstral",
				},
			},
		},
		{
			name:  "three-level dotted key",
			key:   "notify.smtp.host",
			value: "mail.example.com",
			expected: map[string]interface{}{
				"notify": map[string]interface{}{
					"smtp": map[string]interface{}{
						"host": "mail.example.com",
					},
				},
			},
		},
		{
			name:  "boolean value",
			key:   "notify.smtp.enabled",
			value: true,
			expected: map[string]interface{}{
				"notify": map[string]interface{}{
					"smtp": map[string]interface{}{
						"enabled": true,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildNestedMap(tt.key, tt.value)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConfigCmd_UnknownSubcommand(t *testing.T) {
	cfg = cli.DefaultConfig()
	cmd := configCmd()

	cmd.SetArgs([]string{"foo"})
	err := cmd.Execute()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown subcommand")
}

func TestConfigCmd_SetMissingValue(t *testing.T) {
	cfg = cli.DefaultConfig()
	cmd := configCmd()

	cmd.SetArgs([]string{"set"})
	err := cmd.Execute()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing arguments")
}

func TestConfigCmd_SetMissingValueForKey(t *testing.T) {
	cfg = cli.DefaultConfig()
	cmd := configCmd()

	cmd.SetArgs([]string{"set", "server"})
	err := cmd.Execute()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing arguments")
}

func TestConfigCmd_SetInvalidMaxIterations(t *testing.T) {
	cfg = cli.DefaultConfig()
	cmd := configCmd()

	cmd.SetArgs([]string{"set", "default_max_iterations", "not-a-number"})
	err := cmd.Execute()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid value for default_max_iterations")
}

func TestUpdateCmd_NoFlags(t *testing.T) {
	client = cli.NewClient("http://localhost:0")
	cmd := updateCmd()

	cmd.SetArgs([]string{"1"})
	err := cmd.Execute()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no updates specified")
}

func TestUpdateCmd_InvalidJobID(t *testing.T) {
	client = cli.NewClient("http://localhost:0")
	cmd := updateCmd()

	cmd.SetArgs([]string{"abc", "--priority", "high"})
	err := cmd.Execute()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid job ID")
}

func TestServerConfigCmd_UnknownSubcommand(t *testing.T) {
	client = cli.NewClient("http://localhost:0")
	cmd := serverConfigCmd()

	cmd.SetArgs([]string{"get"})
	err := cmd.Execute()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown subcommand")
}

func TestServerConfigCmd_SetMissingArgs(t *testing.T) {
	client = cli.NewClient("http://localhost:0")
	cmd := serverConfigCmd()

	cmd.SetArgs([]string{"set"})
	err := cmd.Execute()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing arguments")
}

func TestServerConfigCmd_SetMissingValue(t *testing.T) {
	client = cli.NewClient("http://localhost:0")
	cmd := serverConfigCmd()

	cmd.SetArgs([]string{"set", "large_model.name"})
	err := cmd.Execute()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing arguments")
}
