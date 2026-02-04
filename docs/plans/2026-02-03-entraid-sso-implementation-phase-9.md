# Phase 9: Integration Tests Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Create end-to-end integration tests that spin up a full ralph-o-matic server with a mock EntraID provider and verify the complete auth lifecycle: browser flow, API auth, job ownership, role enforcement.

**Architecture:** Tests are tagged `//go:build integration` and live in `internal/auth/integration_test.go`. A test helper starts a mock OIDC server (reusing the test helpers from Phase 3), creates an `EntraProvider` pointing at it, starts a full HTTP server with auth enabled, and runs requests through the real middleware stack. Tests cover mode switching, health endpoint bypass, browser redirect, API auth, and job ownership filtering.

**Tech Stack:** Go `testing`, `httptest`, existing test helpers from Phase 3, `testify`

---

### Task 1: Create integration test infrastructure

**Files:**
- Create: `internal/auth/integration_test.go`

**Step 1: Write the test infrastructure and first integration test**

Create `internal/auth/integration_test.go`:

```go
//go:build integration

package auth_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ryan/ralph-o-matic/internal/api"
	"github.com/ryan/ralph-o-matic/internal/auth"
	"github.com/ryan/ralph-o-matic/internal/db"
	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/ryan/ralph-o-matic/internal/queue"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/go-jose/go-jose.v2"
)

// testEnv sets up a full server with mock EntraID
type testEnv struct {
	Server       *httptest.Server
	OIDCServer   *httptest.Server
	Provider     *auth.EntraProvider
	Sessions     *auth.SessionStore
	DB           *db.DB
	RSAKey       *rsa.PrivateKey
	ClientID     string
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	clientID := "test-client-id"

	// Mock OIDC server
	oidcMux := http.NewServeMux()
	var oidcURL string

	oidcMux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"issuer":                 oidcURL,
			"authorization_endpoint": oidcURL + "/authorize",
			"token_endpoint":         oidcURL + "/token",
			"jwks_uri":               oidcURL + "/jwks",
		})
	})

	oidcMux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		jwks := jose.JSONWebKeySet{
			Keys: []jose.JSONWebKey{
				{Key: &key.PublicKey, KeyID: "test-key-1", Algorithm: string(jose.RS256), Use: "sig"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jwks)
	})

	oidcSrv := httptest.NewServer(oidcMux)
	oidcURL = oidcSrv.URL
	t.Cleanup(oidcSrv.Close)

	// Create provider
	provider, err := auth.NewEntraProvider(context.Background(), auth.EntraConfig{
		TenantID:     "test-tenant",
		ClientID:     clientID,
		ClientSecret: "test-secret",
	}, oidcURL)
	require.NoError(t, err)

	sessions := auth.NewSessionStore(30 * time.Minute)

	// Create database
	database, err := db.New(":memory:")
	require.NoError(t, err)
	require.NoError(t, database.Migrate())
	t.Cleanup(func() { database.Close() })

	// Create server with auth
	q := queue.New(database)
	srv := api.NewServer(database, q, ":0", &api.ServerOptions{
		AuthProvider: provider,
		Sessions:     sessions,
	})

	testSrv := httptest.NewServer(srv.Router())
	t.Cleanup(testSrv.Close)

	return &testEnv{
		Server:     testSrv,
		OIDCServer: oidcSrv,
		Provider:   provider,
		Sessions:   sessions,
		DB:         database,
		RSAKey:     key,
		ClientID:   clientID,
	}
}

func (e *testEnv) signToken(t *testing.T, claims map[string]interface{}) string {
	t.Helper()

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: e.RSAKey},
		(&jose.SignerOptions{}).WithHeader("kid", "test-key-1"),
	)
	require.NoError(t, err)

	payload, err := json.Marshal(claims)
	require.NoError(t, err)

	jws, err := signer.Sign(payload)
	require.NoError(t, err)

	token, err := jws.CompactSerialize()
	require.NoError(t, err)
	return token
}

func (e *testEnv) validToken(t *testing.T, oid, name, email string, roles []string) string {
	return e.signToken(t, map[string]interface{}{
		"iss":                e.OIDCServer.URL,
		"aud":                e.ClientID,
		"exp":                time.Now().Add(1 * time.Hour).Unix(),
		"iat":                time.Now().Unix(),
		"oid":                oid,
		"name":               name,
		"preferred_username": email,
		"roles":              roles,
	})
}
```

**Step 2: Commit infrastructure**

```bash
git add internal/auth/integration_test.go
git commit -m "test(auth): add integration test infrastructure"
```

---

### Task 2: Write health endpoint bypass test

**Files:**
- Modify: `internal/auth/integration_test.go`

**Step 1: Write the test**

Add to `internal/auth/integration_test.go`:

```go
func TestIntegration_HealthEndpoint_NoAuthRequired(t *testing.T) {
	env := newTestEnv(t)

	resp, err := http.Get(env.Server.URL + "/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
```

**Step 2: Run test**

Run: `go test -v -tags=integration -run TestIntegration_HealthEndpoint ./internal/auth/`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/auth/integration_test.go
git commit -m "test(auth): integration test - health endpoint bypasses auth"
```

---

### Task 3: Write unauthenticated request tests

**Files:**
- Modify: `internal/auth/integration_test.go`

**Step 1: Write the tests**

```go
func TestIntegration_UnauthenticatedAPI_Returns401(t *testing.T) {
	env := newTestEnv(t)

	resp, err := http.Get(env.Server.URL + "/api/jobs")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestIntegration_UnauthenticatedBrowser_Redirects(t *testing.T) {
	env := newTestEnv(t)

	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse // don't follow redirects
	}}

	req, _ := http.NewRequest("GET", env.Server.URL+"/", nil)
	req.Header.Set("Accept", "text/html")

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusFound, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Location"), "/auth/login")
}
```

**Step 2: Run tests**

Run: `go test -v -tags=integration -run "TestIntegration_Unauthenticated" ./internal/auth/`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/auth/integration_test.go
git commit -m "test(auth): integration tests - unauthenticated requests"
```

---

### Task 4: Write authenticated API request tests

**Files:**
- Modify: `internal/auth/integration_test.go`

**Step 1: Write the tests**

```go
func TestIntegration_AuthenticatedAPI_BearerToken(t *testing.T) {
	env := newTestEnv(t)

	token := env.validToken(t, "user-oid-1", "Ryan", "ryan@contoso.com", []string{"Admin"})

	req, _ := http.NewRequest("GET", env.Server.URL+"/api/jobs", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestIntegration_ExpiredToken_Returns401(t *testing.T) {
	env := newTestEnv(t)

	token := env.signToken(t, map[string]interface{}{
		"iss": env.OIDCServer.URL,
		"aud": env.ClientID,
		"exp": time.Now().Add(-1 * time.Hour).Unix(),
		"iat": time.Now().Add(-2 * time.Hour).Unix(),
		"oid": "user-1",
	})

	req, _ := http.NewRequest("GET", env.Server.URL+"/api/jobs", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestIntegration_WrongAudienceToken_Returns401(t *testing.T) {
	env := newTestEnv(t)

	token := env.signToken(t, map[string]interface{}{
		"iss": env.OIDCServer.URL,
		"aud": "wrong-client-id",
		"exp": time.Now().Add(1 * time.Hour).Unix(),
		"iat": time.Now().Unix(),
		"oid": "user-1",
	})

	req, _ := http.NewRequest("GET", env.Server.URL+"/api/jobs", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}
```

**Step 2: Run tests**

Run: `go test -v -tags=integration -run "TestIntegration_Authenticated|TestIntegration_Expired|TestIntegration_WrongAudience" ./internal/auth/`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/auth/integration_test.go
git commit -m "test(auth): integration tests - API auth with Bearer tokens"
```

---

### Task 5: Write job ownership tests

**Files:**
- Modify: `internal/auth/integration_test.go`

**Step 1: Write the tests**

```go
func TestIntegration_JobOwnership_UserSeesOwnJobs(t *testing.T) {
	env := newTestEnv(t)
	repo := db.NewJobRepo(env.DB)

	// Create jobs for two users
	job1 := models.NewJob("https://github.com/test/repo", "main", "test", 10)
	job1.OwnerID = "user-a"
	job1.OwnerName = "User A"
	require.NoError(t, repo.Create(job1))

	job2 := models.NewJob("https://github.com/test/repo", "main", "test", 10)
	job2.OwnerID = "user-b"
	job2.OwnerName = "User B"
	require.NoError(t, repo.Create(job2))

	// Query as user-a
	token := env.validToken(t, "user-a", "User A", "a@contoso.com", []string{"User"})
	req, _ := http.NewRequest("GET", env.Server.URL+"/api/jobs", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result struct {
		Jobs  []*models.Job `json:"jobs"`
		Total int           `json:"total"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, 1, result.Total)
	assert.Equal(t, "user-a", result.Jobs[0].OwnerID)
}

func TestIntegration_JobOwnership_AdminSeesAll(t *testing.T) {
	env := newTestEnv(t)
	repo := db.NewJobRepo(env.DB)

	job1 := models.NewJob("https://github.com/test/repo", "main", "test", 10)
	job1.OwnerID = "user-a"
	require.NoError(t, repo.Create(job1))

	job2 := models.NewJob("https://github.com/test/repo", "main", "test", 10)
	job2.OwnerID = "user-b"
	require.NoError(t, repo.Create(job2))

	token := env.validToken(t, "admin-user", "Admin", "admin@contoso.com", []string{"Admin"})
	req, _ := http.NewRequest("GET", env.Server.URL+"/api/jobs", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	var result struct {
		Jobs  []*models.Job `json:"jobs"`
		Total int           `json:"total"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, 2, result.Total)
}

func TestIntegration_JobOwnership_UserCannotAccessOtherJob(t *testing.T) {
	env := newTestEnv(t)
	repo := db.NewJobRepo(env.DB)

	job := models.NewJob("https://github.com/test/repo", "main", "test", 10)
	job.OwnerID = "user-b"
	require.NoError(t, repo.Create(job))

	token := env.validToken(t, "user-a", "User A", "a@contoso.com", []string{"User"})
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/jobs/%d", env.Server.URL, job.ID), nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestIntegration_JobCreation_SetsOwner(t *testing.T) {
	env := newTestEnv(t)

	token := env.validToken(t, "creator-oid", "Creator", "creator@contoso.com", []string{"User"})

	body := `{"repo_url":"https://github.com/test/repo","branch":"main","prompt":"test","max_iterations":10}`
	req, _ := http.NewRequest("POST", env.Server.URL+"/api/jobs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	var job models.Job
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&job))
	assert.Equal(t, "creator-oid", job.OwnerID)
	assert.Equal(t, "Creator", job.OwnerName)
}
```

Add `strings` to the import.

**Step 2: Run tests**

Run: `go test -v -tags=integration -run "TestIntegration_JobOwnership|TestIntegration_JobCreation" ./internal/auth/`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/auth/integration_test.go
git commit -m "test(auth): integration tests - job ownership and role enforcement"
```

---

### Task 6: Write auth config endpoint test

**Files:**
- Modify: `internal/auth/integration_test.go`

**Step 1: Write the test**

```go
func TestIntegration_AuthConfig_ReturnsEntraMode(t *testing.T) {
	env := newTestEnv(t)

	resp, err := http.Get(env.Server.URL + "/auth/config")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var cfg auth.AuthConfigResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&cfg))
	assert.Equal(t, "entra", cfg.Mode)
	assert.Equal(t, env.ClientID, cfg.ClientID)
}
```

**Step 2: Run test**

Run: `go test -v -tags=integration -run TestIntegration_AuthConfig ./internal/auth/`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/auth/integration_test.go
git commit -m "test(auth): integration test - auth config endpoint"
```

---

### Task 7: Write mode=none integration test

**Files:**
- Modify: `internal/auth/integration_test.go`

**Step 1: Write the test**

```go
func TestIntegration_ModeNone_AllEndpointsOpen(t *testing.T) {
	// Create server WITHOUT auth (mode=none)
	database, err := db.New(":memory:")
	require.NoError(t, err)
	require.NoError(t, database.Migrate())
	t.Cleanup(func() { database.Close() })

	q := queue.New(database)
	srv := api.NewServer(database, q, ":0", nil) // nil opts = no auth

	testSrv := httptest.NewServer(srv.Router())
	defer testSrv.Close()

	// API request without auth should succeed
	resp, err := http.Get(testSrv.URL + "/api/jobs")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Auth config should return mode=none
	resp2, err := http.Get(testSrv.URL + "/auth/config")
	require.NoError(t, err)
	defer resp2.Body.Close()

	var cfg auth.AuthConfigResponse
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&cfg))
	assert.Equal(t, "none", cfg.Mode)
}
```

**Step 2: Run test**

Run: `go test -v -tags=integration -run TestIntegration_ModeNone ./internal/auth/`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/auth/integration_test.go
git commit -m "test(auth): integration test - mode=none open endpoints"
```

---

### Task 8: Run full integration test suite

**Step 1: Run all integration tests**

Run: `go test -v -tags=integration -race ./internal/auth/`
Expected: All PASS

**Step 2: Run all unit tests**

Run: `go test -v -short -race ./...`
Expected: All PASS

**Step 3: Run linter**

Run: `make lint`
Expected: No lint errors

**Step 4: Build**

Run: `make build`
Expected: Clean build

---

## Dependencies

- **Depends on:** All previous phases (1-7)
- **Blocks:** Nothing (final validation phase)

## Reference Files

- Design: `docs/plans/2026-02-03-entraid-sso-design.md` (lines 675-730, "Testing 11g: Integration & End-to-End")
- Existing integration test pattern: check for `//go:build integration` tags in the codebase
- Test infrastructure: Phase 3's `testOIDCServer` and `signTestJWT` helpers

## Notes for Implementer

- Integration tests live in `internal/auth/` with a `_test` package suffix (`package auth_test`) to test the public API.
- The `testEnv` struct centralizes all test infrastructure. Each test function gets a fresh env.
- These tests import from `internal/api`, `internal/db`, `internal/queue` — they test the full stack.
- The mock OIDC server is the same pattern from Phase 3 but set up at the integration level.
- Run with `-tags=integration` — CI should run these separately from unit tests.
- The `go-jose` import path depends on what `go-oidc` pulls in. Check `go.mod` after Phase 3.
