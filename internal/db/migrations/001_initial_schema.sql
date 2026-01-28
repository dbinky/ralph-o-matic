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
