//go:build !windows

package api

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCheckDisk_CurrentDir(t *testing.T) {
	// Current directory should have space available
	err := checkDisk(".", 1) // 1 byte minimum
	assert.NoError(t, err)
}

func TestCheckDisk_NonexistentPath(t *testing.T) {
	err := checkDisk("/nonexistent/path/that/does/not/exist", 1)
	assert.Error(t, err)
}

func TestCheckDisk_InsufficientSpace(t *testing.T) {
	err := checkDisk(".", math.MaxUint64)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "low disk space")
}
