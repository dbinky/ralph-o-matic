package db

import (
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// DB wraps the database connection
type DB struct {
	conn   *sql.DB
	mu     sync.Mutex
	closed bool
}

// New creates a new database connection
func New(path string) (*DB, error) {
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection
	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Enable foreign keys
	if _, err := conn.Exec("PRAGMA foreign_keys = ON"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	// Enable WAL mode for better concurrent access
	if path != ":memory:" {
		if _, err := conn.Exec("PRAGMA journal_mode = WAL"); err != nil {
			conn.Close()
			return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
		}
	}

	return &DB{conn: conn}, nil
}

// Close closes the database connection
func (db *DB) Close() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if db.closed {
		return nil
	}

	db.closed = true
	return db.conn.Close()
}

// Ping tests the database connection
func (db *DB) Ping() error {
	return db.conn.Ping()
}

// Conn returns the underlying sql.DB for advanced operations
func (db *DB) Conn() *sql.DB {
	return db.conn
}

// Migrate applies all pending migrations
func (db *DB) Migrate() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	// Create migrations table if not exists
	_, err := db.conn.Exec(`
		CREATE TABLE IF NOT EXISTS migrations (
			version INTEGER PRIMARY KEY,
			applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	// Get current version
	currentVersion, err := db.migrationVersionLocked()
	if err != nil {
		return fmt.Errorf("failed to get migration version: %w", err)
	}

	// Read migration files
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("failed to read migrations: %w", err)
	}

	// Sort migrations by version
	var migrations []struct {
		version int
		name    string
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		// Parse version from filename (e.g., "001_initial_schema.sql")
		parts := strings.SplitN(entry.Name(), "_", 2)
		if len(parts) < 2 {
			continue
		}
		version, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}
		migrations = append(migrations, struct {
			version int
			name    string
		}{version, entry.Name()})
	}
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].version < migrations[j].version
	})

	// Apply pending migrations
	for _, m := range migrations {
		if m.version <= currentVersion {
			continue
		}

		content, err := migrationsFS.ReadFile("migrations/" + m.name)
		if err != nil {
			return fmt.Errorf("failed to read migration %s: %w", m.name, err)
		}

		tx, err := db.conn.Begin()
		if err != nil {
			return fmt.Errorf("failed to begin transaction: %w", err)
		}

		if _, err := tx.Exec(string(content)); err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to apply migration %s: %w", m.name, err)
		}

		if _, err := tx.Exec("INSERT INTO migrations (version) VALUES (?)", m.version); err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to record migration %s: %w", m.name, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit migration %s: %w", m.name, err)
		}
	}

	return nil
}

// MigrationVersion returns the current migration version
func (db *DB) MigrationVersion() (int, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.migrationVersionLocked()
}

func (db *DB) migrationVersionLocked() (int, error) {
	var version int
	err := db.conn.QueryRow("SELECT COALESCE(MAX(version), 0) FROM migrations").Scan(&version)
	if err != nil {
		// Table might not exist yet
		if strings.Contains(err.Error(), "no such table") {
			return 0, nil
		}
		return 0, err
	}
	return version, nil
}
