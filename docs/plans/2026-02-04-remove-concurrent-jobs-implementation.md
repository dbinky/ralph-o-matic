# Remove `concurrent_jobs` Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Remove the unused `concurrent_jobs` configuration setting from all layers of the codebase.

**Architecture:** Pure removal — delete the field from the model, DB serialization, API response, CLI display, dashboard display, install script, README, and all tests that reference it. Add a DB migration to clean up orphaned rows. TDD: write tests asserting the field is absent first, verify they fail, then remove the code to make them pass.

**Tech Stack:** Go 1.24, SQLite, testify, chi router

---

### Task 1: Write Failing Model Tests

**Files:**
- Modify: `internal/models/config_test.go`

**Step 1: Write the failing tests**

Add these tests to `internal/models/config_test.go`. They will fail because `ConcurrentJobs` still exists on the struct.

```go
func TestDefaultServerConfig_NoConcurrentJobs(t *testing.T) {
	cfg := DefaultServerConfig()

	// ConcurrentJobs field should not exist — verify via JSON serialization
	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	var raw map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &raw))
	_, hasConcurrentJobs := raw["concurrent_jobs"]
	assert.False(t, hasConcurrentJobs, "concurrent_jobs should not be in serialized config")
}

func TestServerConfig_Validate_NoConcurrentJobsCheck(t *testing.T) {
	cfg := DefaultServerConfig()
	// Default config should validate without any concurrent_jobs logic
	assert.NoError(t, cfg.Validate())

	// Verify there's no ConcurrentJobs field by checking JSON round-trip
	data, _ := json.Marshal(cfg)
	var raw map[string]interface{}
	json.Unmarshal(data, &raw)
	_, exists := raw["concurrent_jobs"]
	assert.False(t, exists, "concurrent_jobs field should not exist on ServerConfig")
}

func TestServerConfig_Merge_NoConcurrentJobs(t *testing.T) {
	base := DefaultServerConfig()
	updates := &ServerConfig{DefaultMaxIterations: 100}
	merged := base.Merge(updates)

	// Verify merged config has no concurrent_jobs in JSON
	data, _ := json.Marshal(merged)
	var raw map[string]interface{}
	json.Unmarshal(data, &raw)
	_, exists := raw["concurrent_jobs"]
	assert.False(t, exists, "concurrent_jobs should not appear in merged config")
	assert.Equal(t, 100, merged.DefaultMaxIterations)
}
```

**Step 2: Run tests to verify they fail**

Run: `go test -v -run "TestDefaultServerConfig_NoConcurrentJobs|TestServerConfig_Validate_NoConcurrentJobsCheck|TestServerConfig_Merge_NoConcurrentJobs" ./internal/models/`
Expected: FAIL — `concurrent_jobs` key exists in JSON output.

**Step 3: Commit failing tests**

```bash
git add internal/models/config_test.go
git commit -m "test: add failing tests asserting concurrent_jobs field is removed from model"
```

---

### Task 2: Write Failing DB Tests

**Files:**
- Modify: `internal/db/config_test.go`

**Step 1: Write the failing tests**

Add these tests to `internal/db/config_test.go`:

```go
func TestConfigRepo_SaveLoad_NoConcurrentJobs(t *testing.T) {
	db := newTestDB(t)
	repo := NewConfigRepo(db)

	cfg := models.DefaultServerConfig()
	err := repo.Save(cfg)
	require.NoError(t, err)

	// Verify concurrent_jobs key is NOT in the database
	_, err = repo.GetKey("concurrent_jobs")
	assert.ErrorIs(t, err, ErrNotFound, "concurrent_jobs should not be stored in DB")

	// Load should work fine without it
	loaded, err := repo.Get()
	require.NoError(t, err)
	assert.Equal(t, "devstral", loaded.LargeModel.Name)
}

func TestConfigRepo_UnknownKey_ConcurrentJobs_Ignored(t *testing.T) {
	db := newTestDB(t)
	repo := NewConfigRepo(db)

	// Simulate pre-migration DB with orphaned concurrent_jobs key
	err := repo.Update("concurrent_jobs", "5")
	require.NoError(t, err)

	// Loading config should succeed (unknown keys are skipped)
	cfg, err := repo.Get()
	require.NoError(t, err)
	assert.Equal(t, "devstral", cfg.LargeModel.Name)
}

func TestConfigRepo_FullRoundTrip_NoConcurrentJobs(t *testing.T) {
	db := newTestDB(t)
	repo := NewConfigRepo(db)

	cfg := models.DefaultServerConfig()
	cfg.LargeModel = models.ModelPlacement{Name: "custom:70b", Device: "gpu", MemoryGB: 42}
	cfg.DefaultMaxIterations = 100
	cfg.WorkspaceDir = "/tmp/test"
	cfg.JobRetentionDays = 7

	err := repo.Save(cfg)
	require.NoError(t, err)

	fetched, err := repo.Get()
	require.NoError(t, err)
	assert.Equal(t, "custom:70b", fetched.LargeModel.Name)
	assert.Equal(t, 100, fetched.DefaultMaxIterations)
	assert.Equal(t, "/tmp/test", fetched.WorkspaceDir)
	assert.Equal(t, 7, fetched.JobRetentionDays)
}
```

**Step 2: Run tests to verify they fail**

Run: `go test -v -run "TestConfigRepo_SaveLoad_NoConcurrentJobs|TestConfigRepo_UnknownKey_ConcurrentJobs_Ignored|TestConfigRepo_FullRoundTrip_NoConcurrentJobs" ./internal/db/`
Expected: `TestConfigRepo_SaveLoad_NoConcurrentJobs` FAILS because `Save()` still writes `concurrent_jobs`. The other two may pass since they don't assert on `ConcurrentJobs` directly, but that's fine — they serve as regression guards.

**Step 3: Commit failing tests**

```bash
git add internal/db/config_test.go
git commit -m "test: add failing tests asserting concurrent_jobs removed from DB layer"
```

---

### Task 3: Write Failing API Tests

**Files:**
- Modify: `internal/api/config_test.go`

**Step 1: Write the failing tests**

Add these tests to `internal/api/config_test.go`:

```go
func TestAPI_GetConfig_NoConcurrentJobsField(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest("GET", "/api/config", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var raw map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
	_, hasConcurrentJobs := raw["concurrent_jobs"]
	assert.False(t, hasConcurrentJobs, "GET /api/config should not include concurrent_jobs")
}

func TestAPI_UpdateConfig_ConcurrentJobsIgnored(t *testing.T) {
	srv, _ := newTestServer(t)

	// Send a PATCH with concurrent_jobs — it should be silently ignored
	body := []byte(`{"concurrent_jobs": 5, "default_max_iterations": 75}`)
	req := httptest.NewRequest("PATCH", "/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var raw map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
	_, hasConcurrentJobs := raw["concurrent_jobs"]
	assert.False(t, hasConcurrentJobs, "PATCH response should not include concurrent_jobs")

	// Verify the valid field was applied
	assert.Equal(t, float64(75), raw["default_max_iterations"])
}

func TestAPI_GetConfig_ResponseSchema_NoConcurrentJobs(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest("GET", "/api/config", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	// Verify all expected fields exist, concurrent_jobs does not
	var raw map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))

	expectedKeys := []string{
		"ollama", "large_model", "small_model",
		"default_max_iterations", "job_retention_days",
		"default_backend", "anthropic",
		"max_claude_retries", "max_git_retries", "git_retry_backoff_ms",
	}
	for _, key := range expectedKeys {
		assert.Contains(t, raw, key, "expected key %s in response", key)
	}

	unexpectedKeys := []string{"concurrent_jobs"}
	for _, key := range unexpectedKeys {
		assert.NotContains(t, raw, key, "unexpected key %s in response", key)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test -v -run "TestAPI_GetConfig_NoConcurrentJobsField|TestAPI_UpdateConfig_ConcurrentJobsIgnored|TestAPI_GetConfig_ResponseSchema_NoConcurrentJobs" ./internal/api/`
Expected: FAIL — `configResponse` still has `ConcurrentJobs` field.

**Step 3: Commit failing tests**

```bash
git add internal/api/config_test.go
git commit -m "test: add failing tests asserting concurrent_jobs removed from API layer"
```

---

### Task 4: Implement Model Changes

**Files:**
- Modify: `internal/models/config.go`

**Step 1: Remove ConcurrentJobs from ServerConfig struct**

In `internal/models/config.go`, remove line 113:
```go
ConcurrentJobs       int `json:"concurrent_jobs"`
```

The `// Execution` comment should now only have `DefaultMaxIterations` under it.

**Step 2: Remove from DefaultServerConfig()**

In `DefaultServerConfig()`, remove line 144:
```go
ConcurrentJobs:       1,
```

**Step 3: Remove validation check**

In `Validate()`, remove lines 166-168:
```go
if c.ConcurrentJobs <= 0 {
    return fmt.Errorf("concurrent_jobs must be positive")
}
```

**Step 4: Remove from Merge()**

In `Merge()`, remove lines 220-222:
```go
if updates.ConcurrentJobs > 0 {
    result.ConcurrentJobs = updates.ConcurrentJobs
}
```

**Step 5: Update existing tests that reference ConcurrentJobs**

In `internal/models/config_test.go`:

- `TestDefaultServerConfig`: Remove line 84: `assert.Equal(t, 1, cfg.ConcurrentJobs)`
- `TestServerConfig_Validate`: Remove the entire `"zero jobs fails"` subtest (lines 159-163)

**Step 6: Run all model tests to verify they pass**

Run: `go test -v ./internal/models/`
Expected: ALL PASS

**Step 7: Commit**

```bash
git add internal/models/config.go internal/models/config_test.go
git commit -m "feat: remove concurrent_jobs from ServerConfig model"
```

---

### Task 5: Implement DB Changes

**Files:**
- Modify: `internal/db/config.go`
- Create: `internal/db/migrations/004_remove_concurrent_jobs.sql`

**Step 1: Remove from Save() serialization**

In `internal/db/config.go`, remove line 76 from the `values` map:
```go
"concurrent_jobs":          strconv.Itoa(cfg.ConcurrentJobs),
```

**Step 2: Remove from applyConfigValue()**

In `internal/db/config.go`, remove lines 170-175 (the `case "concurrent_jobs":` block):
```go
case "concurrent_jobs":
    v, err := strconv.Atoi(value)
    if err != nil {
        return err
    }
    cfg.ConcurrentJobs = v
```

**Step 3: Create the migration**

Create `internal/db/migrations/004_remove_concurrent_jobs.sql`:
```sql
-- Remove unused concurrent_jobs configuration key.
-- The setting was stored but never enforced by the worker.
DELETE FROM config WHERE key = 'concurrent_jobs';
```

**Step 4: Update existing DB tests that reference ConcurrentJobs**

In `internal/db/config_test.go`:

- `TestConfigRepo_Save` (line 35): Remove `cfg.ConcurrentJobs = 5` and its assertion `assert.Equal(t, 5, fetched.ConcurrentJobs)` (line 47)
- `TestConfigRepo_FullRoundTrip_AllStructuredFields` (line 133): Remove `cfg.ConcurrentJobs = 4` and `assert.Equal(t, 4, fetched.ConcurrentJobs)` (line 155)
- `TestConfigRepo_UpdateScalar_PreservesStructured` (lines 164-183): This entire test uses `concurrent_jobs` as its scalar — replace with a different scalar field. Change to update `default_max_iterations` instead:
  ```go
  func TestConfigRepo_UpdateScalar_PreservesStructured(t *testing.T) {
  	db := newTestDB(t)
  	repo := NewConfigRepo(db)

  	cfg := models.DefaultServerConfig()
  	cfg.LargeModel = models.ModelPlacement{Name: "keep-this:70b", Device: "gpu", MemoryGB: 42}
  	err := repo.Save(cfg)
  	require.NoError(t, err)

  	// Update a scalar field
  	err = repo.Update("default_max_iterations", "100")
  	require.NoError(t, err)

  	fetched, err := repo.Get()
  	require.NoError(t, err)
  	assert.Equal(t, 100, fetched.DefaultMaxIterations)
  	assert.Equal(t, "keep-this:70b", fetched.LargeModel.Name)
  	assert.Equal(t, "gpu", fetched.LargeModel.Device)
  	assert.Equal(t, 42.0, fetched.LargeModel.MemoryGB)
  }
  ```
- `TestConfigRepo_SaveThenSave_Overwrites` (lines 256, 262, 269): Remove `cfg1.ConcurrentJobs = 2`, `cfg2.ConcurrentJobs = 8`, and `assert.Equal(t, 8, fetched.ConcurrentJobs)`

**Step 5: Run all DB tests**

Run: `go test -v ./internal/db/`
Expected: ALL PASS

**Step 6: Commit**

```bash
git add internal/db/config.go internal/db/config_test.go internal/db/migrations/004_remove_concurrent_jobs.sql
git commit -m "feat: remove concurrent_jobs from DB layer, add cleanup migration"
```

---

### Task 6: Implement API Changes

**Files:**
- Modify: `internal/api/config.go`

**Step 1: Remove from configResponse struct**

In `internal/api/config.go`, remove line 30:
```go
ConcurrentJobs       int                     `json:"concurrent_jobs"`
```

**Step 2: Remove from newConfigResponse()**

In `internal/api/config.go`, remove line 46:
```go
ConcurrentJobs:       cfg.ConcurrentJobs,
```

**Step 3: Update existing API tests that reference ConcurrentJobs**

In `internal/api/config_test.go`:

- `TestAPI_UpdateConfig` (lines 36-58): Remove `ConcurrentJobs: 3` from the payload (line 42) and `assert.Equal(t, 3, resp.ConcurrentJobs)` (line 58). Replace with a different field assertion, e.g. verify `LargeModel.Name` was applied.

**Step 4: Run all API tests**

Run: `go test -v ./internal/api/`
Expected: ALL PASS

**Step 5: Commit**

```bash
git add internal/api/config.go internal/api/config_test.go
git commit -m "feat: remove concurrent_jobs from API response"
```

---

### Task 7: Implement CLI, Dashboard, Install Script, and README Changes

**Files:**
- Modify: `cmd/cli/commands.go`
- Modify: `internal/dashboard/dashboard.go`
- Modify: `scripts/install.sh`
- Modify: `README.md`

**Step 1: Remove from CLI display**

In `cmd/cli/commands.go`, remove line 351:
```go
fmt.Printf("concurrent_jobs: %d\n", serverCfg.ConcurrentJobs)
```

**Step 2: Remove from dashboard config page**

In `internal/dashboard/dashboard.go`, remove line 186:
```go
{"concurrent_jobs", fmt.Sprintf("%d", cfg.ConcurrentJobs)},
```

**Step 3: Remove from install script**

In `scripts/install.sh`, remove line 743:
```yaml
concurrent_jobs: 1
```

**Step 4: Remove from README**

In `README.md`, remove the row:
```markdown
| `concurrent_jobs` | `1` | Parallel job limit |
```

**Step 5: Run full test suite**

Run: `make test`
Expected: ALL PASS

**Step 6: Run lint**

Run: `make lint`
Expected: PASS (no unused imports, no references to removed field)

**Step 7: Commit**

```bash
git add cmd/cli/commands.go internal/dashboard/dashboard.go scripts/install.sh README.md
git commit -m "feat: remove concurrent_jobs from CLI, dashboard, install script, and docs"
```

---

### Task 8: Final Verification

**Step 1: Search for any remaining references**

Run: `grep -r "concurrent_jobs\|ConcurrentJobs" --include="*.go" --include="*.sql" --include="*.sh" --include="*.md" --include="*.html" .`
Expected: Only the design doc (`docs/plans/2026-02-04-remove-concurrent-jobs-design.md`) and this implementation plan should match. No production code references.

**Step 2: Run full test suite with race detector**

Run: `make test`
Expected: ALL PASS

**Step 3: Build**

Run: `make build`
Expected: SUCCESS

**Step 4: Run BATS tests**

Run: `make test-bats`
Expected: ALL PASS (install script no longer references concurrent_jobs)

**Step 5: Commit (if any cleanup was needed)**

If no additional changes were needed, skip this step.
