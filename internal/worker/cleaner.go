package worker

import (
	"context"
	"log"
	"os"
	"sync"
	"time"

	"github.com/ryan/ralph-o-matic/internal/db"
	"github.com/ryan/ralph-o-matic/internal/git"
	"github.com/ryan/ralph-o-matic/internal/models"
)

const cleanupInterval = 1 * time.Hour

// Cleaner periodically removes workspace directories for completed jobs
// and purges expired job records from the database.
type Cleaner struct {
	jobRepo    *db.JobRepo
	configRepo *db.ConfigRepo
	repoMgr    *git.RepoManager
	gitChecker GitChecker

	running sync.Mutex
}

// NewCleaner creates a new workspace and job retention cleaner.
func NewCleaner(jobRepo *db.JobRepo, configRepo *db.ConfigRepo, repoMgr *git.RepoManager, gitChecker GitChecker) *Cleaner {
	return &Cleaner{
		jobRepo:    jobRepo,
		configRepo: configRepo,
		repoMgr:    repoMgr,
		gitChecker: gitChecker,
	}
}

// Run starts the cleanup loop. It blocks until ctx is cancelled.
func (c *Cleaner) Run(ctx context.Context) {
	log.Println("Cleaner started, running every", cleanupInterval)
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Cleaner stopping")
			return
		case <-ticker.C:
			c.tick(ctx)
		}
	}
}

// tick runs one cleanup cycle. Skips if a previous cycle is still running.
func (c *Cleaner) tick(ctx context.Context) {
	if !c.running.TryLock() {
		log.Println("Cleaner: skipping tick, previous cleanup still running")
		return
	}
	defer c.running.Unlock()

	c.cleanWorkspaces(ctx)
	c.purgeExpiredJobs(ctx)
}

// cleanWorkspaces removes workspace directories for jobs in terminal states.
func (c *Cleaner) cleanWorkspaces(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}

	jobs, err := c.jobRepo.ListTerminal()
	if err != nil {
		log.Printf("Cleaner: failed to list terminal jobs: %v", err)
		return
	}

	for _, job := range jobs {
		if ctx.Err() != nil {
			return
		}

		wsPath := c.repoMgr.WorkspacePath(job.ID)
		if _, err := os.Stat(wsPath); os.IsNotExist(err) {
			continue
		}

		if job.Status == models.StatusCompleted {
			safe, err := c.isWorkspaceSafeToDelete(wsPath)
			if err != nil {
				log.Printf("Cleaner: job #%d git check error, skipping workspace: %v", job.ID, err)
				continue
			}
			if !safe {
				log.Printf("Cleaner: WARNING job #%d workspace has uncommitted or unpushed changes, skipping", job.ID)
				continue
			}
		}

		if err := c.repoMgr.Cleanup(job.ID); err != nil {
			log.Printf("Cleaner: failed to remove workspace for job #%d: %v", job.ID, err)
			continue
		}
		log.Printf("Cleaner: cleaned up workspace for job #%d", job.ID)
	}
}

// isWorkspaceSafeToDelete checks for uncommitted or unpushed work.
func (c *Cleaner) isWorkspaceSafeToDelete(dir string) (bool, error) {
	uncommitted, err := c.gitChecker.HasUncommittedChanges(dir)
	if err != nil {
		return false, err
	}
	if uncommitted {
		return false, nil
	}

	unpushed, err := c.gitChecker.HasUnpushedCommits(dir)
	if err != nil {
		return false, err
	}
	if unpushed {
		return false, nil
	}

	return true, nil
}

// purgeExpiredJobs deletes job records older than job_retention_days.
func (c *Cleaner) purgeExpiredJobs(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}

	config, err := c.configRepo.Get()
	if err != nil {
		log.Printf("Cleaner: failed to load config: %v", err)
		return
	}

	if config.JobRetentionDays == 0 {
		return
	}

	cutoff := time.Now().Add(-time.Duration(config.JobRetentionDays) * 24 * time.Hour)

	jobs, err := c.jobRepo.ListExpired(cutoff)
	if err != nil {
		log.Printf("Cleaner: failed to list expired jobs: %v", err)
		return
	}

	for _, job := range jobs {
		if ctx.Err() != nil {
			return
		}

		// Defensive: clean up workspace if still present.
		// Git safety checks (uncommitted/unpushed) are intentionally skipped here.
		// These are expired records past retention — any workspace remnants are stale
		// and safe to remove unconditionally.
		wsPath := c.repoMgr.WorkspacePath(job.ID)
		if _, err := os.Stat(wsPath); err == nil {
			if rmErr := c.repoMgr.Cleanup(job.ID); rmErr != nil {
				log.Printf("Cleaner: failed to remove workspace for expired job #%d: %v", job.ID, rmErr)
			}
		}

		if err := c.jobRepo.Delete(job.ID); err != nil {
			log.Printf("Cleaner: failed to delete expired job #%d: %v", job.ID, err)
			continue
		}
		log.Printf("Cleaner: purged expired job #%d", job.ID)
	}
}
