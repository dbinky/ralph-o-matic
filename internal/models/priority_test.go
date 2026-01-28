package models

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPriority_Valid(t *testing.T) {
	tests := []struct {
		name     string
		priority Priority
		want     bool
	}{
		{"high is valid", PriorityHigh, true},
		{"normal is valid", PriorityNormal, true},
		{"low is valid", PriorityLow, true},
		{"empty is invalid", Priority(""), false},
		{"unknown is invalid", Priority("urgent"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.priority.Valid())
		})
	}
}

func TestPriority_Weight(t *testing.T) {
	assert.Greater(t, PriorityHigh.Weight(), PriorityNormal.Weight())
	assert.Greater(t, PriorityNormal.Weight(), PriorityLow.Weight())
}

func TestPriority_JSON(t *testing.T) {
	t.Run("marshal", func(t *testing.T) {
		data, err := json.Marshal(PriorityHigh)
		require.NoError(t, err)
		assert.Equal(t, `"high"`, string(data))
	})

	t.Run("unmarshal valid", func(t *testing.T) {
		var p Priority
		err := json.Unmarshal([]byte(`"normal"`), &p)
		require.NoError(t, err)
		assert.Equal(t, PriorityNormal, p)
	})

	t.Run("unmarshal invalid", func(t *testing.T) {
		var p Priority
		err := json.Unmarshal([]byte(`"urgent"`), &p)
		assert.Error(t, err)
	})
}

func TestParsePriority(t *testing.T) {
	tests := []struct {
		input   string
		want    Priority
		wantErr bool
	}{
		{"high", PriorityHigh, false},
		{"normal", PriorityNormal, false},
		{"low", PriorityLow, false},
		{"HIGH", PriorityHigh, false},
		{"Normal", PriorityNormal, false},
		{"", Priority(""), true},
		{"urgent", Priority(""), true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParsePriority(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}
