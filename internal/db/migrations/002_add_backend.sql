-- Add backend column to jobs table
ALTER TABLE jobs ADD COLUMN backend TEXT NOT NULL DEFAULT '';
