package db

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_InMemory(t *testing.T) {
	db, err := New(":memory:")
	require.NoError(t, err)
	defer db.Close()

	assert.NotNil(t, db)
}

func TestNew_File(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := New(dbPath)
	require.NoError(t, err)
	defer db.Close()

	assert.NotNil(t, db)
	assert.FileExists(t, dbPath)
}

func TestNew_InvalidPath(t *testing.T) {
	_, err := New("/nonexistent/path/test.db")
	assert.Error(t, err)
}

func TestDB_Close(t *testing.T) {
	db, err := New(":memory:")
	require.NoError(t, err)

	err = db.Close()
	assert.NoError(t, err)

	// Double close should not error
	err = db.Close()
	assert.NoError(t, err)
}

func TestDB_Ping(t *testing.T) {
	db, err := New(":memory:")
	require.NoError(t, err)
	defer db.Close()

	err = db.Ping()
	assert.NoError(t, err)
}

func TestDB_Migrate(t *testing.T) {
	db, err := New(":memory:")
	require.NoError(t, err)
	defer db.Close()

	err = db.Migrate()
	require.NoError(t, err)

	// Verify tables exist
	var count int
	err = db.conn.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='jobs'").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Running migrate again should be idempotent
	err = db.Migrate()
	assert.NoError(t, err)
}

func TestDB_MigrationVersion(t *testing.T) {
	db, err := New(":memory:")
	require.NoError(t, err)
	defer db.Close()

	// Before migration, version should be 0
	version, err := db.MigrationVersion()
	require.NoError(t, err)
	assert.Equal(t, 0, version)

	// After migration, version should be latest
	err = db.Migrate()
	require.NoError(t, err)

	version, err = db.MigrationVersion()
	require.NoError(t, err)
	assert.Greater(t, version, 0)
}

// Helper to create a test database with migrations applied
func newTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := New(":memory:")
	require.NoError(t, err)
	err = db.Migrate()
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}
