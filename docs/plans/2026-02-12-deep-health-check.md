# Deep Health Check Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a `/readiness` endpoint that checks DB, Ollama, and disk before reporting healthy, so load balancers and monitoring can detect real failures.

**Architecture:** Keep existing `/health` as a fast liveness probe (process is up). Add `/readiness` as a deep check that verifies DB connectivity, Ollama reachability (when backend=ollama), and workspace disk space. Each component reports its own status; overall status is "ok" only if all pass.

**Tech Stack:** Go, chi router, `platform.OllamaClient`, `db.DB.Ping()`, `syscall.Statfs` for disk space

---

### Task 1: Add `CheckDisk` helper

A small function that checks free disk space at a given path and returns an error if below a threshold.

**Files:**
- Create: `internal/api/health.go`
- Create: `internal/api/health_test.go`

**Step 1: Write the test for `CheckDisk`**

In `internal/api/health_test.go`:

```go
package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCheckDisk_CurrentDir(t *testing.T) {
	// Current directory should have space available
	err := checkDisk(".", 1) // 1 byte minimum
	assert.NoError(t, err)
}

func TestCheckDisk_NonexistentPath(t *testing.T) {
	err := checkDisk("/nonexistent/path/that/does/not/exist", 1)
	assert.Error(t, err)
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v -run TestCheckDisk ./internal/api/`
Expected: FAIL — `checkDisk` not defined

**Step 3: Implement `checkDisk`**

In `internal/api/health.go`:

```go
package api

import (
	"fmt"
	"syscall"
)

// checkDisk verifies that the filesystem at path has at least minBytes free.
func checkDisk(path string, minBytes uint64) error {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	free := stat.Bavail * uint64(stat.Bsize)
	if free < minBytes {
		return fmt.Errorf("low disk space: %d MB free, need %d MB", free/(1024*1024), minBytes/(1024*1024))
	}
	return nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test -v -run TestCheckDisk ./internal/api/`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/api/health.go internal/api/health_test.go
git commit -m "feat: add checkDisk helper for readiness probe"
```

---

### Task 2: Add `/readiness` handler with DB and disk checks

Wire the readiness endpoint into the router. It checks DB via `Ping()`, disk via `checkDisk`, and loads config to determine if Ollama should be checked.

**Files:**
- Modify: `internal/api/health.go`
- Modify: `internal/api/server.go:71-81` (route registration)
- Modify: `internal/api/health_test.go`

**Step 1: Write the test for readiness with healthy DB**

Add to `internal/api/health_test.go`:

```go
import (
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"github.com/ryan/ralph-o-matic/internal/db"
	"github.com/ryan/ralph-o-matic/internal/queue"
	"github.com/stretchr/testify/require"
)

func TestServer_Readiness_Healthy(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest("GET", "/readiness", nil)
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "ok", resp["status"])

	checks := resp["checks"].(map[string]interface{})
	assert.Equal(t, "ok", checks["database"])
	assert.Equal(t, "ok", checks["disk"])
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v -run TestServer_Readiness_Healthy ./internal/api/`
Expected: FAIL — 404 because route doesn't exist yet

**Step 3: Implement the readiness handler and route**

Add to `internal/api/health.go`:

```go
import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"syscall"
	"time"

	"github.com/ryan/ralph-o-matic/internal/db"
	"github.com/ryan/ralph-o-matic/internal/platform"
)

const (
	readinessTimeout = 5 * time.Second
	minDiskBytes     = 100 * 1024 * 1024 // 100 MB
)

// readinessResponse is the JSON response from /readiness.
type readinessResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

func (s *Server) handleReadiness(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
	defer cancel()

	checks := map[string]string{}
	healthy := true

	// DB check
	if err := s.db.Ping(); err != nil {
		checks["database"] = err.Error()
		healthy = false
	} else {
		checks["database"] = "ok"
	}

	// Disk check — use DB path's directory; fall back to current dir
	if err := checkDisk(".", minDiskBytes); err != nil {
		checks["disk"] = err.Error()
		healthy = false
	} else {
		checks["disk"] = "ok"
	}

	// Ollama check — only when backend is ollama
	configRepo := db.NewConfigRepo(s.db)
	cfg, err := configRepo.Get()
	if err != nil {
		checks["config"] = err.Error()
		healthy = false
	} else if cfg.DefaultBackend == "" || cfg.DefaultBackend == "ollama" {
		client := platform.NewOllamaClient(cfg.Ollama.Host)
		if err := client.Ping(ctx); err != nil {
			checks["ollama"] = err.Error()
			healthy = false
		} else {
			checks["ollama"] = "ok"
		}
	}
	// When backend is "anthropic", skip Ollama check entirely

	status := http.StatusOK
	resp := readinessResponse{Status: "ok", Checks: checks}
	if !healthy {
		status = http.StatusServiceUnavailable
		resp.Status = "unhealthy"
	}

	writeJSON(w, status, resp)
}
```

Register the route in `internal/api/server.go` — add right after the `/health` line (line 80):

```go
r.Get("/readiness", s.handleReadiness)
```

**Step 4: Run test to verify it passes**

Run: `go test -v -run TestServer_Readiness ./internal/api/`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/api/health.go internal/api/health_test.go internal/api/server.go
git commit -m "feat: add /readiness endpoint with DB and disk checks"
```

---

### Task 3: Add test for readiness with closed DB

Verify the endpoint properly reports database failures.

**Files:**
- Modify: `internal/api/health_test.go`

**Step 1: Write the closed-DB test**

Add to `internal/api/health_test.go`:

```go
func TestServer_Readiness_DBClosed(t *testing.T) {
	srv, database := newTestServer(t)
	database.Close()

	req := httptest.NewRequest("GET", "/readiness", nil)
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "unhealthy", resp["status"])

	checks := resp["checks"].(map[string]interface{})
	assert.NotEqual(t, "ok", checks["database"])
}
```

**Step 2: Run test to verify it passes**

Run: `go test -v -run TestServer_Readiness_DBClosed ./internal/api/`
Expected: PASS (handler should already handle this case)

**Step 3: Commit**

```bash
git add internal/api/health_test.go
git commit -m "test: add readiness test for DB failure case"
```

---

### Task 4: Add test for Ollama check with mock server

Verify the Ollama check works by using `httptest.NewServer` to simulate Ollama responses.

**Files:**
- Modify: `internal/api/health_test.go`

**Step 1: Write tests for Ollama healthy and unhealthy**

Add to `internal/api/health_test.go`:

```go
import (
	"net/http/httptest"
	// ... existing imports
)

func TestServer_Readiness_OllamaDown(t *testing.T) {
	srv, database := newTestServer(t)

	// Configure Ollama backend pointing to a closed server
	configRepo := db.NewConfigRepo(database)
	cfg, err := configRepo.Get()
	require.NoError(t, err)
	cfg.DefaultBackend = "ollama"
	cfg.Ollama.Host = "http://127.0.0.1:1" // nothing listening
	require.NoError(t, configRepo.Save(cfg))

	req := httptest.NewRequest("GET", "/readiness", nil)
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "unhealthy", resp["status"])

	checks := resp["checks"].(map[string]interface{})
	assert.NotEqual(t, "ok", checks["ollama"])
}

func TestServer_Readiness_OllamaHealthy(t *testing.T) {
	// Fake Ollama server
	fakeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"models":[]}`))
	}))
	defer fakeSrv.Close()

	srv, database := newTestServer(t)

	configRepo := db.NewConfigRepo(database)
	cfg, err := configRepo.Get()
	require.NoError(t, err)
	cfg.DefaultBackend = "ollama"
	cfg.Ollama.Host = fakeSrv.URL
	require.NoError(t, configRepo.Save(cfg))

	req := httptest.NewRequest("GET", "/readiness", nil)
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "ok", resp["status"])
	checks := resp["checks"].(map[string]interface{})
	assert.Equal(t, "ok", checks["ollama"])
}

func TestServer_Readiness_AnthropicBackend_SkipsOllama(t *testing.T) {
	srv, database := newTestServer(t)

	configRepo := db.NewConfigRepo(database)
	cfg, err := configRepo.Get()
	require.NoError(t, err)
	cfg.DefaultBackend = "anthropic"
	cfg.Anthropic.LargeModel = "claude-opus-4-5-20251101"
	cfg.Anthropic.SmallModel = "claude-haiku-4-5-20251001"
	require.NoError(t, configRepo.Save(cfg))

	req := httptest.NewRequest("GET", "/readiness", nil)
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "ok", resp["status"])

	checks := resp["checks"].(map[string]interface{})
	_, hasOllama := checks["ollama"]
	assert.False(t, hasOllama, "ollama check should be skipped for anthropic backend")
}
```

**Step 2: Run tests**

Run: `go test -v -run TestServer_Readiness ./internal/api/`
Expected: All PASS

**Step 3: Commit**

```bash
git add internal/api/health_test.go
git commit -m "test: add Ollama connectivity tests for readiness endpoint"
```

---

### Task 5: Update existing health test and ops docs

Update the existing health test comment and the ops guide to document both endpoints.

**Files:**
- Modify: `docs/ops-guide.md` (health check section ~line 252)

**Step 1: Update ops-guide.md**

Find the health check section and expand it to cover both endpoints:

```markdown
### Health & Readiness

**Liveness** — confirms the process is running. Use for restart-on-hang detection:

```bash
curl -sf http://127.0.0.1:9090/health
# {"status":"ok"}
```

**Readiness** — confirms DB, Ollama (if used), and disk are healthy. Use for
load balancer routing and monitoring alerts:

```bash
curl -sf http://127.0.0.1:9090/readiness
# {"status":"ok","checks":{"database":"ok","disk":"ok","ollama":"ok"}}
```

Returns HTTP 200 when all checks pass, 503 when any check fails. Individual
check messages describe the failure:

```json
{"status":"unhealthy","checks":{"database":"ok","disk":"ok","ollama":"failed to connect to Ollama at http://localhost:11434: ..."}}
```

Both endpoints are always accessible (no auth required).

**Kubernetes / systemd example:**

| Probe     | Endpoint     | Interval | Timeout | Failure threshold |
|-----------|-------------|----------|---------|-------------------|
| Liveness  | `/health`    | 10s      | 1s      | 3                 |
| Readiness | `/readiness` | 15s      | 6s      | 2                 |
```

**Step 2: Commit**

```bash
git add docs/ops-guide.md
git commit -m "docs: document /readiness endpoint in ops guide"
```

---

### Task 6: Run full test suite and lint

**Step 1: Run all tests**

Run: `make test`
Expected: All PASS

**Step 2: Run linter**

Run: `make lint`
Expected: No errors

**Step 3: Fix any issues found, then commit if needed**
