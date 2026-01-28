# Phase 2: Database Layer

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement SQLite database layer with migrations, job persistence, and config storage. Full TDD with comprehensive test coverage.

**Architecture:** Pure Go SQLite driver (modernc.org/sqlite) for zero CGO dependencies. Repository pattern for data access. SQL migrations embedded in binary.

**Tech Stack:** Go 1.22+, modernc.org/sqlite (pure Go), embed for migrations

**Dependencies:** Phase 1 must be complete (models defined)

---

## Task 1: Add SQLite Dependencies

**Files:**
- Modify: `go.mod`

**Step 1: Add SQLite driver**

Run:
```bash
go get modernc.org/sqlite
```

Expected: sqlite added to go.mod

**Step 2: Verify dependency**

Run:
```bash
go mod tidy
cat go.mod | grep sqlite
```

Expected: `modernc.org/sqlite` listed

**Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add modernc.org/sqlite for pure Go SQLite"
```

---

## Task 2: Create Database Schema Migration

**Files:**
- Create: `internal/db/migrations/001_initial_schema.sql`

**Step 1: Write the migration SQL**

Create `internal/db/migrations/001_initial_schema.sql`:

```sql
-- Jobs table
CREATE TABLE IF NOT EXISTS jobs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    status TEXT NOT NULL DEFAULT 'queued',
    priority TEXT NOT NULL DEFAULT 'normal',
    position INTEGER NOT NULL DEFAULT 0,

    -- Repository info
    repo_url TEXT NOT NULL,
    branch TEXT NOT NULL,
    result_branch TEXT NOT NULL,
    working_dir TEXT,

    -- Execution config
    prompt TEXT NOT NULL,
    max_iterations INTEGER NOT NULL,
    env TEXT, -- JSON encoded map[string]string

    -- Progress tracking
    iteration INTEGER NOT NULL DEFAULT 0,
    retry_count INTEGER NOT NULL DEFAULT 0,

    -- Timestamps
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at DATETIME,
    paused_at DATETIME,
    completed_at DATETIME,

    -- Results
    pr_url TEXT,
    error TEXT
);

-- Indexes for common queries
CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status);
CREATE INDEX IF NOT EXISTS idx_jobs_priority_position ON jobs(priority, position);
CREATE INDEX IF NOT EXISTS idx_jobs_branch ON jobs(branch);
CREATE INDEX IF NOT EXISTS idx_jobs_created_at ON jobs(created_at);

-- Config table (key-value store)
CREATE TABLE IF NOT EXISTS config (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Job logs table
CREATE TABLE IF NOT EXISTS job_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    job_id INTEGER NOT NULL,
    iteration INTEGER NOT NULL,
    timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    message TEXT NOT NULL,
    FOREIGN KEY (job_id) REFERENCES jobs(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_job_logs_job_id ON job_logs(job_id);

-- Migrations table (tracks applied migrations)
CREATE TABLE IF NOT EXISTS migrations (
    version INTEGER PRIMARY KEY,
    applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

**Step 2: Commit**

```bash
mkdir -p internal/db/migrations
git add internal/db/migrations/001_initial_schema.sql
git commit -m "feat(db): add initial schema migration"
```

---

## Task 3: Implement Database Connection with Tests

**Files:**
- Create: `internal/db/db.go`
- Create: `internal/db/db_test.go`

**Step 1: Write the failing tests**

Create `internal/db/db_test.go`:

```go
package db

import (
	"os"
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
```

**Step 2: Run tests to verify they fail**

Run:
```bash
go test ./internal/db/... -v
```

Expected: FAIL - package not found

**Step 3: Write minimal implementation**

Create `internal/db/db.go`:

```go
package db

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
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
	// Ensure parent directory exists for file-based databases
	if path != ":memory:" {
		dir := filepath.Dir(path)
		if dir != "." && dir != "" {
			// Check if parent exists - don't create it
			if _, err := fs.Stat(nil, dir); err != nil {
				// For non-memory databases, try to open anyway
				// SQLite will fail with appropriate error
			}
		}
	}

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
```

**Step 4: Run tests to verify they pass**

Run:
```bash
go test ./internal/db/... -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/db/db.go internal/db/db_test.go
git commit -m "feat(db): add database connection with migration support"
```

---

## Task 4: Implement Job Repository with Tests

**Files:**
- Create: `internal/db/jobs.go`
- Create: `internal/db/jobs_test.go`

**Step 1: Write the failing tests**

Create `internal/db/jobs_test.go`:

```go
package db

import (
	"testing"
	"time"

	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJobRepo_Create(t *testing.T) {
	db := newTestDB(t)
	repo := NewJobRepo(db)

	job := models.NewJob(
		"git@github.com:user/repo.git",
		"feature/test",
		"Run all tests",
		50,
	)
	job.Priority = models.PriorityHigh
	job.WorkingDir = "packages/auth"
	job.Env = map[string]string{"NODE_ENV": "test"}

	err := repo.Create(job)
	require.NoError(t, err)

	assert.Greater(t, job.ID, int64(0))
	assert.Equal(t, 1, job.Position) // First job gets position 1
}

func TestJobRepo_Get(t *testing.T) {
	db := newTestDB(t)
	repo := NewJobRepo(db)

	// Create a job
	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	err := repo.Create(job)
	require.NoError(t, err)

	// Fetch it back
	fetched, err := repo.Get(job.ID)
	require.NoError(t, err)

	assert.Equal(t, job.ID, fetched.ID)
	assert.Equal(t, job.RepoURL, fetched.RepoURL)
	assert.Equal(t, job.Branch, fetched.Branch)
	assert.Equal(t, job.Prompt, fetched.Prompt)
	assert.Equal(t, job.Status, fetched.Status)
}

func TestJobRepo_Get_NotFound(t *testing.T) {
	db := newTestDB(t)
	repo := NewJobRepo(db)

	_, err := repo.Get(99999)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestJobRepo_Update(t *testing.T) {
	db := newTestDB(t)
	repo := NewJobRepo(db)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	err := repo.Create(job)
	require.NoError(t, err)

	// Modify and update
	job.Status = models.StatusRunning
	now := time.Now()
	job.StartedAt = &now
	job.Iteration = 5

	err = repo.Update(job)
	require.NoError(t, err)

	// Verify changes persisted
	fetched, err := repo.Get(job.ID)
	require.NoError(t, err)

	assert.Equal(t, models.StatusRunning, fetched.Status)
	assert.NotNil(t, fetched.StartedAt)
	assert.Equal(t, 5, fetched.Iteration)
}

func TestJobRepo_Delete(t *testing.T) {
	db := newTestDB(t)
	repo := NewJobRepo(db)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	err := repo.Create(job)
	require.NoError(t, err)

	err = repo.Delete(job.ID)
	require.NoError(t, err)

	_, err = repo.Get(job.ID)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestJobRepo_List_All(t *testing.T) {
	db := newTestDB(t)
	repo := NewJobRepo(db)

	// Create multiple jobs
	for i := 0; i < 5; i++ {
		job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
		err := repo.Create(job)
		require.NoError(t, err)
	}

	jobs, total, err := repo.List(ListOptions{})
	require.NoError(t, err)

	assert.Equal(t, 5, total)
	assert.Len(t, jobs, 5)
}

func TestJobRepo_List_WithStatus(t *testing.T) {
	db := newTestDB(t)
	repo := NewJobRepo(db)

	// Create jobs with different statuses
	for _, status := range []models.JobStatus{models.StatusQueued, models.StatusQueued, models.StatusRunning} {
		job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
		job.Status = status
		err := repo.Create(job)
		require.NoError(t, err)
	}

	jobs, total, err := repo.List(ListOptions{
		Statuses: []models.JobStatus{models.StatusQueued},
	})
	require.NoError(t, err)

	assert.Equal(t, 2, total)
	assert.Len(t, jobs, 2)
	for _, job := range jobs {
		assert.Equal(t, models.StatusQueued, job.Status)
	}
}

func TestJobRepo_List_WithPagination(t *testing.T) {
	db := newTestDB(t)
	repo := NewJobRepo(db)

	// Create 10 jobs
	for i := 0; i < 10; i++ {
		job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
		err := repo.Create(job)
		require.NoError(t, err)
	}

	// Get first page
	jobs, total, err := repo.List(ListOptions{Limit: 3, Offset: 0})
	require.NoError(t, err)
	assert.Equal(t, 10, total)
	assert.Len(t, jobs, 3)

	// Get second page
	jobs, total, err = repo.List(ListOptions{Limit: 3, Offset: 3})
	require.NoError(t, err)
	assert.Equal(t, 10, total)
	assert.Len(t, jobs, 3)
}

func TestJobRepo_ListQueued(t *testing.T) {
	db := newTestDB(t)
	repo := NewJobRepo(db)

	// Create jobs with different priorities
	for i, priority := range []models.Priority{models.PriorityLow, models.PriorityHigh, models.PriorityNormal} {
		job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
		job.Priority = priority
		job.Position = i + 1
		err := repo.Create(job)
		require.NoError(t, err)
	}

	jobs, err := repo.ListQueued()
	require.NoError(t, err)

	// Should be ordered by priority (high first) then position
	require.Len(t, jobs, 3)
	assert.Equal(t, models.PriorityHigh, jobs[0].Priority)
	assert.Equal(t, models.PriorityNormal, jobs[1].Priority)
	assert.Equal(t, models.PriorityLow, jobs[2].Priority)
}

func TestJobRepo_GetByBranch(t *testing.T) {
	db := newTestDB(t)
	repo := NewJobRepo(db)

	job := models.NewJob("git@github.com:user/repo.git", "feature/unique", "test", 10)
	err := repo.Create(job)
	require.NoError(t, err)

	found, err := repo.GetByBranch("feature/unique", false)
	require.NoError(t, err)
	assert.Equal(t, job.ID, found.ID)
}

func TestJobRepo_GetByBranch_ActiveOnly(t *testing.T) {
	db := newTestDB(t)
	repo := NewJobRepo(db)

	// Create completed job
	job1 := models.NewJob("git@github.com:user/repo.git", "feature/test", "test", 10)
	job1.Status = models.StatusCompleted
	err := repo.Create(job1)
	require.NoError(t, err)

	// Create queued job with same branch
	job2 := models.NewJob("git@github.com:user/repo.git", "feature/test", "test", 10)
	err = repo.Create(job2)
	require.NoError(t, err)

	// Should find only the active one
	found, err := repo.GetByBranch("feature/test", true)
	require.NoError(t, err)
	assert.Equal(t, job2.ID, found.ID)
}

func TestJobRepo_UpdatePositions(t *testing.T) {
	db := newTestDB(t)
	repo := NewJobRepo(db)

	// Create 3 jobs
	var jobs []*models.Job
	for i := 0; i < 3; i++ {
		job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
		err := repo.Create(job)
		require.NoError(t, err)
		jobs = append(jobs, job)
	}

	// Reorder: [3, 1, 2]
	newOrder := []int64{jobs[2].ID, jobs[0].ID, jobs[1].ID}
	err := repo.UpdatePositions(newOrder)
	require.NoError(t, err)

	// Verify new positions
	fetched, err := repo.Get(jobs[2].ID)
	require.NoError(t, err)
	assert.Equal(t, 1, fetched.Position)

	fetched, err = repo.Get(jobs[0].ID)
	require.NoError(t, err)
	assert.Equal(t, 2, fetched.Position)

	fetched, err = repo.Get(jobs[1].ID)
	require.NoError(t, err)
	assert.Equal(t, 3, fetched.Position)
}

func TestJobRepo_NextPosition(t *testing.T) {
	db := newTestDB(t)
	repo := NewJobRepo(db)

	// Empty queue
	pos, err := repo.NextPosition()
	require.NoError(t, err)
	assert.Equal(t, 1, pos)

	// Add a job
	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	err = repo.Create(job)
	require.NoError(t, err)

	pos, err = repo.NextPosition()
	require.NoError(t, err)
	assert.Equal(t, 2, pos)
}

func TestJobRepo_CountByStatus(t *testing.T) {
	db := newTestDB(t)
	repo := NewJobRepo(db)

	// Create jobs with different statuses
	statuses := []models.JobStatus{
		models.StatusQueued, models.StatusQueued, models.StatusQueued,
		models.StatusRunning,
		models.StatusCompleted, models.StatusCompleted,
	}
	for _, status := range statuses {
		job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
		job.Status = status
		err := repo.Create(job)
		require.NoError(t, err)
	}

	counts, err := repo.CountByStatus()
	require.NoError(t, err)

	assert.Equal(t, 3, counts[models.StatusQueued])
	assert.Equal(t, 1, counts[models.StatusRunning])
	assert.Equal(t, 2, counts[models.StatusCompleted])
}
```

**Step 2: Run tests to verify they fail**

Run:
```bash
go test ./internal/db/... -v
```

Expected: FAIL - JobRepo not defined

**Step 3: Write minimal implementation**

Create `internal/db/jobs.go`:

```go
package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ryan/ralph-o-matic/internal/models"
)

var ErrNotFound = errors.New("not found")

// JobRepo handles job persistence
type JobRepo struct {
	db *DB
}

// NewJobRepo creates a new job repository
func NewJobRepo(db *DB) *JobRepo {
	return &JobRepo{db: db}
}

// Create inserts a new job and sets its ID and Position
func (r *JobRepo) Create(job *models.Job) error {
	// Get next position
	pos, err := r.NextPosition()
	if err != nil {
		return fmt.Errorf("failed to get next position: %w", err)
	}
	job.Position = pos

	// Encode env as JSON
	var envJSON []byte
	if job.Env != nil {
		envJSON, err = json.Marshal(job.Env)
		if err != nil {
			return fmt.Errorf("failed to encode env: %w", err)
		}
	}

	result, err := r.db.conn.Exec(`
		INSERT INTO jobs (
			status, priority, position,
			repo_url, branch, result_branch, working_dir,
			prompt, max_iterations, env,
			iteration, retry_count,
			created_at, started_at, paused_at, completed_at,
			pr_url, error
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		job.Status, job.Priority, job.Position,
		job.RepoURL, job.Branch, job.ResultBranch, job.WorkingDir,
		job.Prompt, job.MaxIterations, envJSON,
		job.Iteration, job.RetryCount,
		job.CreatedAt, job.StartedAt, job.PausedAt, job.CompletedAt,
		job.PRURL, job.Error,
	)
	if err != nil {
		return fmt.Errorf("failed to insert job: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get last insert id: %w", err)
	}
	job.ID = id

	return nil
}

// Get retrieves a job by ID
func (r *JobRepo) Get(id int64) (*models.Job, error) {
	job := &models.Job{}
	var envJSON sql.NullString
	var startedAt, pausedAt, completedAt sql.NullTime
	var workingDir, prURL, errStr sql.NullString

	err := r.db.conn.QueryRow(`
		SELECT
			id, status, priority, position,
			repo_url, branch, result_branch, working_dir,
			prompt, max_iterations, env,
			iteration, retry_count,
			created_at, started_at, paused_at, completed_at,
			pr_url, error
		FROM jobs WHERE id = ?
	`, id).Scan(
		&job.ID, &job.Status, &job.Priority, &job.Position,
		&job.RepoURL, &job.Branch, &job.ResultBranch, &workingDir,
		&job.Prompt, &job.MaxIterations, &envJSON,
		&job.Iteration, &job.RetryCount,
		&job.CreatedAt, &startedAt, &pausedAt, &completedAt,
		&prURL, &errStr,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get job: %w", err)
	}

	// Handle nullable fields
	if workingDir.Valid {
		job.WorkingDir = workingDir.String
	}
	if startedAt.Valid {
		job.StartedAt = &startedAt.Time
	}
	if pausedAt.Valid {
		job.PausedAt = &pausedAt.Time
	}
	if completedAt.Valid {
		job.CompletedAt = &completedAt.Time
	}
	if prURL.Valid {
		job.PRURL = prURL.String
	}
	if errStr.Valid {
		job.Error = errStr.String
	}
	if envJSON.Valid && envJSON.String != "" {
		if err := json.Unmarshal([]byte(envJSON.String), &job.Env); err != nil {
			return nil, fmt.Errorf("failed to decode env: %w", err)
		}
	}

	return job, nil
}

// Update saves changes to an existing job
func (r *JobRepo) Update(job *models.Job) error {
	var envJSON []byte
	var err error
	if job.Env != nil {
		envJSON, err = json.Marshal(job.Env)
		if err != nil {
			return fmt.Errorf("failed to encode env: %w", err)
		}
	}

	_, err = r.db.conn.Exec(`
		UPDATE jobs SET
			status = ?, priority = ?, position = ?,
			repo_url = ?, branch = ?, result_branch = ?, working_dir = ?,
			prompt = ?, max_iterations = ?, env = ?,
			iteration = ?, retry_count = ?,
			started_at = ?, paused_at = ?, completed_at = ?,
			pr_url = ?, error = ?
		WHERE id = ?
	`,
		job.Status, job.Priority, job.Position,
		job.RepoURL, job.Branch, job.ResultBranch, job.WorkingDir,
		job.Prompt, job.MaxIterations, envJSON,
		job.Iteration, job.RetryCount,
		job.StartedAt, job.PausedAt, job.CompletedAt,
		job.PRURL, job.Error,
		job.ID,
	)
	if err != nil {
		return fmt.Errorf("failed to update job: %w", err)
	}

	return nil
}

// Delete removes a job by ID
func (r *JobRepo) Delete(id int64) error {
	_, err := r.db.conn.Exec("DELETE FROM jobs WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete job: %w", err)
	}
	return nil
}

// ListOptions configures List queries
type ListOptions struct {
	Statuses []models.JobStatus
	Limit    int
	Offset   int
}

// List retrieves jobs with optional filtering and pagination
func (r *JobRepo) List(opts ListOptions) ([]*models.Job, int, error) {
	var where []string
	var args []interface{}

	if len(opts.Statuses) > 0 {
		placeholders := make([]string, len(opts.Statuses))
		for i, s := range opts.Statuses {
			placeholders[i] = "?"
			args = append(args, s)
		}
		where = append(where, "status IN ("+strings.Join(placeholders, ",")+")")
	}

	whereClause := ""
	if len(where) > 0 {
		whereClause = "WHERE " + strings.Join(where, " AND ")
	}

	// Get total count
	var total int
	countQuery := "SELECT COUNT(*) FROM jobs " + whereClause
	if err := r.db.conn.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count jobs: %w", err)
	}

	// Build query with pagination
	query := "SELECT id FROM jobs " + whereClause + " ORDER BY created_at DESC"
	if opts.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", opts.Limit)
		if opts.Offset > 0 {
			query += fmt.Sprintf(" OFFSET %d", opts.Offset)
		}
	}

	rows, err := r.db.conn.Query(query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list jobs: %w", err)
	}
	defer rows.Close()

	var jobs []*models.Job
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, 0, fmt.Errorf("failed to scan job id: %w", err)
		}
		job, err := r.Get(id)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to get job %d: %w", id, err)
		}
		jobs = append(jobs, job)
	}

	return jobs, total, nil
}

// ListQueued returns queued jobs ordered by priority and position
func (r *JobRepo) ListQueued() ([]*models.Job, error) {
	rows, err := r.db.conn.Query(`
		SELECT id FROM jobs
		WHERE status = ?
		ORDER BY
			CASE priority
				WHEN 'high' THEN 1
				WHEN 'normal' THEN 2
				WHEN 'low' THEN 3
			END,
			position
	`, models.StatusQueued)
	if err != nil {
		return nil, fmt.Errorf("failed to list queued jobs: %w", err)
	}
	defer rows.Close()

	var jobs []*models.Job
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan job id: %w", err)
		}
		job, err := r.Get(id)
		if err != nil {
			return nil, fmt.Errorf("failed to get job %d: %w", id, err)
		}
		jobs = append(jobs, job)
	}

	return jobs, nil
}

// GetByBranch finds a job by branch name
func (r *JobRepo) GetByBranch(branch string, activeOnly bool) (*models.Job, error) {
	query := "SELECT id FROM jobs WHERE branch = ?"
	args := []interface{}{branch}

	if activeOnly {
		query += " AND status NOT IN (?, ?, ?)"
		args = append(args, models.StatusCompleted, models.StatusFailed, models.StatusCancelled)
	}

	query += " ORDER BY created_at DESC LIMIT 1"

	var id int64
	err := r.db.conn.QueryRow(query, args...).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get job by branch: %w", err)
	}

	return r.Get(id)
}

// UpdatePositions updates the position of multiple jobs
func (r *JobRepo) UpdatePositions(jobIDs []int64) error {
	tx, err := r.db.conn.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	for i, id := range jobIDs {
		_, err := tx.Exec("UPDATE jobs SET position = ? WHERE id = ?", i+1, id)
		if err != nil {
			return fmt.Errorf("failed to update position for job %d: %w", id, err)
		}
	}

	return tx.Commit()
}

// NextPosition returns the next available position number
func (r *JobRepo) NextPosition() (int, error) {
	var maxPos sql.NullInt64
	err := r.db.conn.QueryRow("SELECT MAX(position) FROM jobs WHERE status = ?", models.StatusQueued).Scan(&maxPos)
	if err != nil {
		return 0, fmt.Errorf("failed to get max position: %w", err)
	}

	if !maxPos.Valid {
		return 1, nil
	}
	return int(maxPos.Int64) + 1, nil
}

// CountByStatus returns job counts grouped by status
func (r *JobRepo) CountByStatus() (map[models.JobStatus]int, error) {
	rows, err := r.db.conn.Query("SELECT status, COUNT(*) FROM jobs GROUP BY status")
	if err != nil {
		return nil, fmt.Errorf("failed to count by status: %w", err)
	}
	defer rows.Close()

	counts := make(map[models.JobStatus]int)
	for rows.Next() {
		var status models.JobStatus
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, fmt.Errorf("failed to scan count: %w", err)
		}
		counts[status] = count
	}

	return counts, nil
}
```

**Step 4: Run tests to verify they pass**

Run:
```bash
go test ./internal/db/... -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/db/jobs.go internal/db/jobs_test.go
git commit -m "feat(db): add job repository with CRUD and query operations"
```

---

## Task 5: Implement Config Repository with Tests

**Files:**
- Create: `internal/db/config.go`
- Create: `internal/db/config_test.go`

**Step 1: Write the failing tests**

Create `internal/db/config_test.go`:

```go
package db

import (
	"testing"

	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigRepo_GetDefault(t *testing.T) {
	db := newTestDB(t)
	repo := NewConfigRepo(db)

	cfg, err := repo.Get()
	require.NoError(t, err)

	// Should return defaults when no config exists
	assert.Equal(t, "qwen3-coder:70b", cfg.LargeModel)
	assert.Equal(t, "qwen2.5-coder:7b", cfg.SmallModel)
}

func TestConfigRepo_Save(t *testing.T) {
	db := newTestDB(t)
	repo := NewConfigRepo(db)

	cfg := models.DefaultServerConfig()
	cfg.LargeModel = "custom-model:latest"
	cfg.ConcurrentJobs = 5

	err := repo.Save(cfg)
	require.NoError(t, err)

	// Fetch and verify
	fetched, err := repo.Get()
	require.NoError(t, err)

	assert.Equal(t, "custom-model:latest", fetched.LargeModel)
	assert.Equal(t, 5, fetched.ConcurrentJobs)
}

func TestConfigRepo_Update(t *testing.T) {
	db := newTestDB(t)
	repo := NewConfigRepo(db)

	// Save initial config
	cfg := models.DefaultServerConfig()
	err := repo.Save(cfg)
	require.NoError(t, err)

	// Update specific field
	err = repo.Update("large_model", "updated-model")
	require.NoError(t, err)

	// Verify
	fetched, err := repo.Get()
	require.NoError(t, err)
	assert.Equal(t, "updated-model", fetched.LargeModel)
}

func TestConfigRepo_GetKey(t *testing.T) {
	db := newTestDB(t)
	repo := NewConfigRepo(db)

	cfg := models.DefaultServerConfig()
	cfg.LargeModel = "test-model"
	err := repo.Save(cfg)
	require.NoError(t, err)

	value, err := repo.GetKey("large_model")
	require.NoError(t, err)
	assert.Equal(t, "test-model", value)
}

func TestConfigRepo_GetKey_NotFound(t *testing.T) {
	db := newTestDB(t)
	repo := NewConfigRepo(db)

	_, err := repo.GetKey("nonexistent")
	assert.ErrorIs(t, err, ErrNotFound)
}
```

**Step 2: Run tests to verify they fail**

Run:
```bash
go test ./internal/db/... -v
```

Expected: FAIL - ConfigRepo not defined

**Step 3: Write minimal implementation**

Create `internal/db/config.go`:

```go
package db

import (
	"database/sql"
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
	values := map[string]string{
		"large_model":            cfg.LargeModel,
		"small_model":            cfg.SmallModel,
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
		cfg.LargeModel = value
	case "small_model":
		cfg.SmallModel = value
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
```

**Step 4: Run tests to verify they pass**

Run:
```bash
go test ./internal/db/... -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/db/config.go internal/db/config_test.go
git commit -m "feat(db): add config repository for server settings"
```

---

## Task 6: Implement Job Logs Repository with Tests

**Files:**
- Create: `internal/db/logs.go`
- Create: `internal/db/logs_test.go`

**Step 1: Write the failing tests**

Create `internal/db/logs_test.go`:

```go
package db

import (
	"testing"

	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogRepo_Append(t *testing.T) {
	db := newTestDB(t)
	jobRepo := NewJobRepo(db)
	logRepo := NewLogRepo(db)

	// Create a job first
	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	err := jobRepo.Create(job)
	require.NoError(t, err)

	// Append logs
	err = logRepo.Append(job.ID, 1, "Starting iteration 1")
	require.NoError(t, err)

	err = logRepo.Append(job.ID, 1, "Running tests...")
	require.NoError(t, err)
}

func TestLogRepo_GetForJob(t *testing.T) {
	db := newTestDB(t)
	jobRepo := NewJobRepo(db)
	logRepo := NewLogRepo(db)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	err := jobRepo.Create(job)
	require.NoError(t, err)

	// Add logs for multiple iterations
	messages := []struct {
		iteration int
		message   string
	}{
		{1, "Iteration 1 start"},
		{1, "Iteration 1 end"},
		{2, "Iteration 2 start"},
		{2, "Iteration 2 end"},
	}

	for _, m := range messages {
		err := logRepo.Append(job.ID, m.iteration, m.message)
		require.NoError(t, err)
	}

	logs, err := logRepo.GetForJob(job.ID)
	require.NoError(t, err)
	assert.Len(t, logs, 4)
	assert.Equal(t, "Iteration 1 start", logs[0].Message)
}

func TestLogRepo_GetForIteration(t *testing.T) {
	db := newTestDB(t)
	jobRepo := NewJobRepo(db)
	logRepo := NewLogRepo(db)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	err := jobRepo.Create(job)
	require.NoError(t, err)

	logRepo.Append(job.ID, 1, "Iter 1 message")
	logRepo.Append(job.ID, 2, "Iter 2 message")
	logRepo.Append(job.ID, 2, "Iter 2 another message")

	logs, err := logRepo.GetForIteration(job.ID, 2)
	require.NoError(t, err)
	assert.Len(t, logs, 2)
}

func TestLogRepo_DeleteForJob(t *testing.T) {
	db := newTestDB(t)
	jobRepo := NewJobRepo(db)
	logRepo := NewLogRepo(db)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	err := jobRepo.Create(job)
	require.NoError(t, err)

	logRepo.Append(job.ID, 1, "Message 1")
	logRepo.Append(job.ID, 1, "Message 2")

	err = logRepo.DeleteForJob(job.ID)
	require.NoError(t, err)

	logs, err := logRepo.GetForJob(job.ID)
	require.NoError(t, err)
	assert.Len(t, logs, 0)
}

func TestLogRepo_GetLatest(t *testing.T) {
	db := newTestDB(t)
	jobRepo := NewJobRepo(db)
	logRepo := NewLogRepo(db)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	err := jobRepo.Create(job)
	require.NoError(t, err)

	for i := 0; i < 10; i++ {
		logRepo.Append(job.ID, 1, "Message")
	}

	logs, err := logRepo.GetLatest(job.ID, 5)
	require.NoError(t, err)
	assert.Len(t, logs, 5)
}
```

**Step 2: Run tests to verify they fail**

Run:
```bash
go test ./internal/db/... -v
```

Expected: FAIL - LogRepo not defined

**Step 3: Write minimal implementation**

Create `internal/db/logs.go`:

```go
package db

import (
	"fmt"
	"time"
)

// JobLog represents a single log entry
type JobLog struct {
	ID        int64
	JobID     int64
	Iteration int
	Timestamp time.Time
	Message   string
}

// LogRepo handles job log persistence
type LogRepo struct {
	db *DB
}

// NewLogRepo creates a new log repository
func NewLogRepo(db *DB) *LogRepo {
	return &LogRepo{db: db}
}

// Append adds a log entry for a job
func (r *LogRepo) Append(jobID int64, iteration int, message string) error {
	_, err := r.db.conn.Exec(`
		INSERT INTO job_logs (job_id, iteration, message)
		VALUES (?, ?, ?)
	`, jobID, iteration, message)
	if err != nil {
		return fmt.Errorf("failed to append log: %w", err)
	}
	return nil
}

// GetForJob retrieves all logs for a job
func (r *LogRepo) GetForJob(jobID int64) ([]*JobLog, error) {
	return r.queryLogs("SELECT id, job_id, iteration, timestamp, message FROM job_logs WHERE job_id = ? ORDER BY timestamp", jobID)
}

// GetForIteration retrieves logs for a specific iteration
func (r *LogRepo) GetForIteration(jobID int64, iteration int) ([]*JobLog, error) {
	return r.queryLogs("SELECT id, job_id, iteration, timestamp, message FROM job_logs WHERE job_id = ? AND iteration = ? ORDER BY timestamp", jobID, iteration)
}

// GetLatest retrieves the N most recent logs for a job
func (r *LogRepo) GetLatest(jobID int64, limit int) ([]*JobLog, error) {
	return r.queryLogs("SELECT id, job_id, iteration, timestamp, message FROM job_logs WHERE job_id = ? ORDER BY timestamp DESC LIMIT ?", jobID, limit)
}

// DeleteForJob removes all logs for a job
func (r *LogRepo) DeleteForJob(jobID int64) error {
	_, err := r.db.conn.Exec("DELETE FROM job_logs WHERE job_id = ?", jobID)
	if err != nil {
		return fmt.Errorf("failed to delete logs: %w", err)
	}
	return nil
}

func (r *LogRepo) queryLogs(query string, args ...interface{}) ([]*JobLog, error) {
	rows, err := r.db.conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query logs: %w", err)
	}
	defer rows.Close()

	var logs []*JobLog
	for rows.Next() {
		log := &JobLog{}
		if err := rows.Scan(&log.ID, &log.JobID, &log.Iteration, &log.Timestamp, &log.Message); err != nil {
			return nil, fmt.Errorf("failed to scan log: %w", err)
		}
		logs = append(logs, log)
	}

	return logs, nil
}
```

**Step 4: Run tests to verify they pass**

Run:
```bash
go test ./internal/db/... -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/db/logs.go internal/db/logs_test.go
git commit -m "feat(db): add log repository for job execution logs"
```

---

## Task 7: Run Full Test Suite and Verify

**Step 1: Run all tests**

Run:
```bash
make test-unit
```

Expected: All tests pass

**Step 2: Check test coverage**

Run:
```bash
go test ./internal/db/... -cover
```

Expected: Coverage > 80%

**Step 3: Verify no race conditions**

Run:
```bash
go test ./internal/db/... -race
```

Expected: No race conditions detected

---

## Phase 2 Completion Checklist

- [ ] SQLite dependency added
- [ ] Schema migration created
- [ ] Database connection with migration support
- [ ] Job repository with full CRUD
- [ ] Job list with filtering and pagination
- [ ] Config repository with defaults
- [ ] Log repository for job logs
- [ ] All tests passing
- [ ] No race conditions
- [ ] All code committed

**Next Phase:** Phase 3 - Queue & Scheduler
