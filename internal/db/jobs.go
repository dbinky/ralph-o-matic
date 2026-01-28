package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

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

	// Collect IDs first, then close rows before fetching full jobs
	// This avoids deadlock with single-connection in-memory databases
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, 0, fmt.Errorf("failed to scan job id: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()

	// Now fetch full job objects
	var jobs []*models.Job
	for _, id := range ids {
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

	// Collect IDs first, then close rows before fetching full jobs
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("failed to scan job id: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()

	// Now fetch full job objects
	var jobs []*models.Job
	for _, id := range ids {
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
