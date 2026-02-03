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
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/ryan/ralph-o-matic/internal/api"
	"github.com/ryan/ralph-o-matic/internal/auth"
	"github.com/ryan/ralph-o-matic/internal/db"
	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/ryan/ralph-o-matic/internal/queue"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testEnv holds all infrastructure needed for integration tests.
type testEnv struct {
	rsaKey       *rsa.PrivateKey
	oidcServer   *httptest.Server
	provider     *auth.EntraProvider
	sessions     *auth.SessionStore
	database     *db.DB
	queue        *queue.Queue
	apiServer    *httptest.Server
	clientID     string
}

// newTestEnv creates a fully wired test environment with auth enabled.
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	clientID := "test-client-id"

	// 1. RSA key for signing JWTs
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err, "generating RSA key")

	// 2. Mock OIDC server
	oidcSrv := newMockOIDCServer(t, key)

	// 3. EntraProvider pointing at mock OIDC server
	cfg := auth.EntraConfig{
		TenantID:     "test-tenant-id",
		ClientID:     clientID,
		ClientSecret: "test-secret",
	}
	provider, err := auth.NewEntraProvider(context.Background(), cfg, oidcSrv.URL)
	require.NoError(t, err, "creating EntraProvider")

	// 4. SessionStore
	sessions := auth.NewSessionStore(30 * time.Minute)

	// 5. In-memory database with migrations
	database, err := db.New(":memory:")
	require.NoError(t, err)
	err = database.Migrate()
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	// 6. Queue
	q := queue.New(database)

	// 7. API server with auth
	opts := &api.ServerOptions{
		AuthProvider: provider,
		Sessions:     sessions,
		Secure:       false,
	}
	srv := api.NewServer(database, q, ":0", opts)

	// 8. httptest.Server wrapping the API server's router
	ts := httptest.NewServer(srv.Router())
	t.Cleanup(ts.Close)

	return &testEnv{
		rsaKey:     key,
		oidcServer: oidcSrv,
		provider:   provider,
		sessions:   sessions,
		database:   database,
		queue:      q,
		apiServer:  ts,
		clientID:   clientID,
	}
}

// newTestEnvNoAuth creates a test environment with auth disabled (mode none).
func newTestEnvNoAuth(t *testing.T) *testEnv {
	t.Helper()

	database, err := db.New(":memory:")
	require.NoError(t, err)
	err = database.Migrate()
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	q := queue.New(database)

	// nil opts = auth disabled
	srv := api.NewServer(database, q, ":0", nil)

	ts := httptest.NewServer(srv.Router())
	t.Cleanup(ts.Close)

	return &testEnv{
		database:  database,
		queue:     q,
		apiServer: ts,
	}
}

// newMockOIDCServer creates a mock OIDC provider with discovery and JWKS endpoints.
func newMockOIDCServer(t *testing.T, key *rsa.PrivateKey) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		scheme := "http"
		issuer := fmt.Sprintf("%s://%s", scheme, r.Host)

		doc := map[string]interface{}{
			"issuer":                 issuer,
			"authorization_endpoint": issuer + "/authorize",
			"token_endpoint":         issuer + "/token",
			"jwks_uri":               issuer + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(doc)
	})

	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		jwk := jose.JSONWebKey{
			Key:       &key.PublicKey,
			KeyID:     "test-key-1",
			Algorithm: string(jose.RS256),
			Use:       "sig",
		}
		jwks := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{jwk}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jwks)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// signToken signs a JWT with the test RSA key and the given claims map.
func (e *testEnv) signToken(t *testing.T, claims map[string]interface{}) string {
	t.Helper()

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: e.rsaKey},
		(&jose.SignerOptions{}).WithHeader(jose.HeaderKey("kid"), "test-key-1"),
	)
	require.NoError(t, err, "creating JWT signer")

	builder := jwt.Signed(signer)
	builder = builder.Claims(claims)

	raw, err := builder.Serialize()
	require.NoError(t, err, "serializing JWT")
	return raw
}

// validToken is a convenience wrapper that produces a valid token with standard claims.
func (e *testEnv) validToken(t *testing.T, oid, name, email string, roles []string) string {
	t.Helper()

	claims := map[string]interface{}{
		"iss":                e.oidcServer.URL,
		"aud":                e.clientID,
		"exp":                time.Now().Add(1 * time.Hour).Unix(),
		"iat":                time.Now().Unix(),
		"oid":                oid,
		"name":               name,
		"preferred_username": email,
	}
	if len(roles) > 0 {
		claims["roles"] = roles
	}
	return e.signToken(t, claims)
}

// createJobInDB inserts a job directly into the database for a given owner.
func (e *testEnv) createJobInDB(t *testing.T, ownerID, ownerName string) *models.Job {
	t.Helper()

	job := models.NewJob("https://github.com/test/repo", "main", "test prompt", 5)
	job.OwnerID = ownerID
	job.OwnerName = ownerName

	err := e.queue.Enqueue(job)
	require.NoError(t, err, "enqueueing job")
	return job
}

// --- Integration Tests ---

func TestIntegration_HealthEndpoint_NoAuthRequired(t *testing.T) {
	env := newTestEnv(t)

	resp, err := http.Get(env.apiServer.URL + "/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]string
	err = json.NewDecoder(resp.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, "ok", body["status"])
}

func TestIntegration_UnauthenticatedAPI_Returns401(t *testing.T) {
	env := newTestEnv(t)

	resp, err := http.Get(env.apiServer.URL + "/api/jobs")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	var body map[string]string
	err = json.NewDecoder(resp.Body).Decode(&body)
	require.NoError(t, err)
	assert.Contains(t, body["error"], "authentication required")
}

func TestIntegration_UnauthenticatedBrowser_Redirects(t *testing.T) {
	env := newTestEnv(t)

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, err := http.NewRequest("GET", env.apiServer.URL+"/", nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "text/html")

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusFound, resp.StatusCode)
	assert.Equal(t, "/auth/login", resp.Header.Get("Location"))
}

func TestIntegration_AuthenticatedAPI_BearerToken(t *testing.T) {
	env := newTestEnv(t)

	token := env.validToken(t, "user-oid-1", "Admin User", "admin@example.com", []string{"Admin"})

	req, err := http.NewRequest("GET", env.apiServer.URL+"/api/jobs", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body api.ListJobsResponse
	err = json.NewDecoder(resp.Body).Decode(&body)
	require.NoError(t, err)
	// Successful decode with zero total is fine; jobs may be nil or empty slice.
	assert.Equal(t, 0, body.Total)
}

func TestIntegration_ExpiredToken_Returns401(t *testing.T) {
	env := newTestEnv(t)

	claims := map[string]interface{}{
		"iss":                env.oidcServer.URL,
		"aud":                env.clientID,
		"exp":                time.Now().Add(-1 * time.Hour).Unix(),
		"iat":                time.Now().Add(-2 * time.Hour).Unix(),
		"oid":                "user-oid-expired",
		"name":               "Expired User",
		"preferred_username": "expired@example.com",
	}
	token := env.signToken(t, claims)

	req, err := http.NewRequest("GET", env.apiServer.URL+"/api/jobs", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestIntegration_WrongAudienceToken_Returns401(t *testing.T) {
	env := newTestEnv(t)

	claims := map[string]interface{}{
		"iss":                env.oidcServer.URL,
		"aud":                "wrong-audience",
		"exp":                time.Now().Add(1 * time.Hour).Unix(),
		"iat":                time.Now().Unix(),
		"oid":                "user-oid-wrongaud",
		"name":               "Wrong Aud User",
		"preferred_username": "wrongaud@example.com",
	}
	token := env.signToken(t, claims)

	req, err := http.NewRequest("GET", env.apiServer.URL+"/api/jobs", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestIntegration_AuthConfig_ReturnsEntraMode(t *testing.T) {
	env := newTestEnv(t)

	resp, err := http.Get(env.apiServer.URL + "/auth/config")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body auth.AuthConfigResponse
	err = json.NewDecoder(resp.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, "entra", body.Mode)
	assert.Equal(t, "test-client-id", body.ClientID)
	assert.Equal(t, "test-tenant-id", body.TenantID)
}

func TestIntegration_JobOwnership_UserSeesOwnJobs(t *testing.T) {
	env := newTestEnv(t)

	// Create jobs for two different users
	env.createJobInDB(t, "user-a-oid", "User A")
	env.createJobInDB(t, "user-a-oid", "User A")
	env.createJobInDB(t, "user-b-oid", "User B")

	// Request as user-a (non-admin)
	token := env.validToken(t, "user-a-oid", "User A", "usera@example.com", []string{"User"})

	req, err := http.NewRequest("GET", env.apiServer.URL+"/api/jobs", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body api.ListJobsResponse
	err = json.NewDecoder(resp.Body).Decode(&body)
	require.NoError(t, err)

	// user-a should only see their 2 jobs
	assert.Equal(t, 2, body.Total)
	for _, job := range body.Jobs {
		assert.Equal(t, "user-a-oid", job.OwnerID)
	}
}

func TestIntegration_JobOwnership_AdminSeesAll(t *testing.T) {
	env := newTestEnv(t)

	// Create jobs for two different users
	env.createJobInDB(t, "user-a-oid", "User A")
	env.createJobInDB(t, "user-b-oid", "User B")

	// Request as admin
	token := env.validToken(t, "admin-oid", "Admin User", "admin@example.com", []string{"Admin"})

	req, err := http.NewRequest("GET", env.apiServer.URL+"/api/jobs", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body api.ListJobsResponse
	err = json.NewDecoder(resp.Body).Decode(&body)
	require.NoError(t, err)

	// Admin should see all jobs
	assert.Equal(t, 2, body.Total)
}

func TestIntegration_JobOwnership_UserCannotAccessOtherJob(t *testing.T) {
	env := newTestEnv(t)

	// Create a job owned by user-b
	job := env.createJobInDB(t, "user-b-oid", "User B")

	// Request as user-a (non-admin)
	token := env.validToken(t, "user-a-oid", "User A", "usera@example.com", []string{"User"})

	req, err := http.NewRequest("GET", fmt.Sprintf("%s/api/jobs/%d", env.apiServer.URL, job.ID), nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestIntegration_JobCreation_SetsOwner(t *testing.T) {
	env := newTestEnv(t)

	token := env.validToken(t, "creator-oid", "Creator Name", "creator@example.com", []string{"User"})

	body := `{
		"repo_url": "https://github.com/test/repo",
		"branch": "feature-branch",
		"prompt": "fix the tests",
		"max_iterations": 10
	}`

	req, err := http.NewRequest("POST", env.apiServer.URL+"/api/jobs", strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	var job models.Job
	err = json.NewDecoder(resp.Body).Decode(&job)
	require.NoError(t, err)
	assert.Equal(t, "creator-oid", job.OwnerID)
	assert.Equal(t, "Creator Name", job.OwnerName)
}

func TestIntegration_ModeNone_AllEndpointsOpen(t *testing.T) {
	env := newTestEnvNoAuth(t)

	// GET /api/jobs without auth should succeed
	resp, err := http.Get(env.apiServer.URL + "/api/jobs")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var jobsBody api.ListJobsResponse
	err = json.NewDecoder(resp.Body).Decode(&jobsBody)
	require.NoError(t, err)
	assert.Equal(t, 0, jobsBody.Total)

	// GET /auth/config should return mode "none"
	resp2, err := http.Get(env.apiServer.URL + "/auth/config")
	require.NoError(t, err)
	defer resp2.Body.Close()

	assert.Equal(t, http.StatusOK, resp2.StatusCode)

	var configBody auth.AuthConfigResponse
	err = json.NewDecoder(resp2.Body).Decode(&configBody)
	require.NoError(t, err)
	assert.Equal(t, "none", configBody.Mode)
	assert.Empty(t, configBody.ClientID)
}
