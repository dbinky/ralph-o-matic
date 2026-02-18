package db

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ryan/ralph-o-matic/internal/broadcast"
)

// JobLog represents a single log entry
type JobLog struct {
	ID        int64     `json:"id"`
	JobID     int64     `json:"job_id"`
	Iteration int       `json:"iteration"`
	Timestamp time.Time `json:"timestamp"`
	Message   string    `json:"message"`
}

// LogRepo handles job log persistence
type LogRepo struct {
	db          *DB
	broadcaster *broadcast.Broadcaster
}

// NewLogRepo creates a new log repository
func NewLogRepo(db *DB) *LogRepo {
	return &LogRepo{db: db}
}

// SetBroadcaster sets the broadcaster for publishing log events.
func (r *LogRepo) SetBroadcaster(b *broadcast.Broadcaster) {
	r.broadcaster = b
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

	if r.broadcaster != nil {
		// Publish to job-specific topic (backward compatible — keeps "log" type)
		jobPayload, _ := json.Marshal(map[string]interface{}{
			"type":      "log",
			"iteration": iteration,
			"message":   message,
		})
		r.broadcaster.Publish(fmt.Sprintf("job:%d", jobID), jobPayload)

		// Publish to global topic (includes jobID for dashboard routing)
		globalPayload, _ := json.Marshal(map[string]interface{}{
			"type":      "job_log",
			"jobID":     jobID,
			"iteration": iteration,
			"message":   message,
		})
		r.broadcaster.Publish("global", globalPayload)
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
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate logs: %w", err)
	}

	return logs, nil
}
