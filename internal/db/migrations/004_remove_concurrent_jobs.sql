-- Remove unused concurrent_jobs configuration key.
-- The setting was stored but never enforced by the worker.
DELETE FROM config WHERE key = 'concurrent_jobs';
