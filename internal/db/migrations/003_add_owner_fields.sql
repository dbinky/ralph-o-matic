-- Add ownership fields to jobs table for auth integration
ALTER TABLE jobs ADD COLUMN owner_id TEXT NOT NULL DEFAULT '';
ALTER TABLE jobs ADD COLUMN owner_name TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_jobs_owner_id ON jobs(owner_id);
