# Remove `concurrent_jobs` Configuration

**Date:** 2026-02-04
**Status:** Approved
**Branch:** `dev-readiness-gap`

## Problem

The `concurrent_jobs` config setting is stored and exposed via API, CLI, and dashboard, but the worker is hardcoded to process one job at a time serially. Setting it to any value has no effect. This is confusing for users and misleading for operators.

## Decision

Remove the `concurrent_jobs` concept entirely. Keep the simple serial queue for this early version. If concurrency is needed later, it can be designed and implemented properly.

## Scope

Remove all traces of `concurrent_jobs` from:

1. **Model** (`internal/models/config.go`) — Field, default, validation, merge logic
2. **Database** (`internal/db/config.go`) — Serialization and deserialization
3. **Migration** (`internal/db/migrations/004_remove_concurrent_jobs.sql`) — Delete orphaned key
4. **API** (`internal/api/config.go`) — Response struct and mapping
5. **CLI** (`cmd/cli/commands.go`) — Display in `server-config` command
6. **Dashboard** (`internal/dashboard/dashboard.go`) — Config page entry
7. **Install script** (`scripts/install.sh`) — Config template
8. **README** — Configuration table row
9. **Tests** — All files asserting on ConcurrentJobs

## Test Plan (TDD)

Tests written first, verified failing, then implementation makes them pass.

### Model tests (`internal/models/config_test.go`)
- Happy path: Default config validates without concurrent_jobs field
- Happy path: Config merge ignores concurrent_jobs
- Edge case: Validation passes with only remaining fields set

### DB tests (`internal/db/config_test.go`)
- Happy path: Save/Load round-trip works without concurrent_jobs
- Success: Config with all valid fields persists and loads correctly
- Edge case: Unknown key `concurrent_jobs` in DB is gracefully ignored on load
- Success: After migration, concurrent_jobs key no longer in config table

### API tests (`internal/api/config_test.go`)
- Happy path: `GET /api/config` response has no `concurrent_jobs` field
- Happy path: `PATCH /api/config` with valid fields succeeds
- Edge case: `PATCH /api/config` with `concurrent_jobs` in body is silently ignored
- Error: Response JSON schema has no `concurrent_jobs` key

### CLI
- Happy path: `server-config` output does not include `concurrent_jobs`

### Dashboard
- Happy path: Config page data does not include `concurrent_jobs` entry

## Migration

```sql
-- 004_remove_concurrent_jobs.sql
DELETE FROM config WHERE key = 'concurrent_jobs';
```

## Backward Compatibility

- Old API clients sending `concurrent_jobs` in PATCH requests: silently ignored (field not on struct)
- Old config rows in DB: cleaned up by migration
- Pre-migration DB load: unknown keys handled gracefully
