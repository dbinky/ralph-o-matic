# Phase 6: CLI Auth Flow Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add OAuth2 device/browser flow to the CLI so it can authenticate against EntraID-protected servers. The CLI discovers the auth mode via `GET /auth/config`, performs the browser-based authorization code + PKCE flow when needed, caches tokens locally, and transparently refreshes them.

**Architecture:** New `internal/cli/auth.go` implements a `TokenSource` interface that the existing `Client` uses to attach `Authorization: Bearer` headers. On first use, the CLI starts a temporary localhost HTTP listener, opens the user's browser to EntraID's authorize URL with PKCE, catches the callback, exchanges the code for tokens, and caches them to `~/.config/ralph-o-matic/token.json`. Subsequent requests use the cached token, refreshing silently when expired.

**Tech Stack:** `golang.org/x/oauth2`, `github.com/pkg/browser`, Go stdlib (`net/http`, `crypto/sha256`, `encoding/base64`)

---

### Task 1: Add browser dependency

**Files:**
- Modify: `go.mod`

**Step 1: Add the dependency**

Run:
```bash
go get github.com/pkg/browser@latest
```

**Step 2: Verify**

Run: `go mod tidy`
Expected: Clean exit

**Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add github.com/pkg/browser for CLI auth"
```

---

### Task 2: Create token cache (load/save)

**Files:**
- Create: `internal/cli/auth.go`
- Test: `internal/cli/auth_test.go`

**Step 1: Write the failing test**

Create `internal/cli/auth_test.go`:

```go
package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenCache_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token.json")

	token := &CachedToken{
		AccessToken:  "access-123",
		RefreshToken: "refresh-456",
		ExpiresAt:    time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC),
		Server:       "https://ralph.example.com",
	}

	err := saveToken(path, token)
	require.NoError(t, err)

	// Verify file permissions
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())

	loaded, err := loadToken(path)
	require.NoError(t, err)
	assert.Equal(t, "access-123", loaded.AccessToken)
	assert.Equal(t, "refresh-456", loaded.RefreshToken)
	assert.Equal(t, "https://ralph.example.com", loaded.Server)
}

func TestTokenCache_Load_FileNotFound(t *testing.T) {
	token, err := loadToken("/nonexistent/token.json")
	require.NoError(t, err)
	assert.Nil(t, token)
}

func TestTokenCache_Load_CorruptedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token.json")
	require.NoError(t, os.WriteFile(path, []byte("{invalid"), 0600))

	token, err := loadToken(path)
	require.NoError(t, err) // corrupted file is treated as missing
	assert.Nil(t, token)
}

func TestTokenCache_IsExpired(t *testing.T) {
	token := &CachedToken{
		AccessToken: "test",
		ExpiresAt:   time.Now().Add(-1 * time.Hour),
	}
	assert.True(t, token.IsExpired())

	token.ExpiresAt = time.Now().Add(1 * time.Hour)
	assert.False(t, token.IsExpired())
}

func TestTokenCache_IsExpired_WithBuffer(t *testing.T) {
	// Token expires in 30 seconds — should be considered "expired"
	// because we add a buffer for clock skew
	token := &CachedToken{
		AccessToken: "test",
		ExpiresAt:   time.Now().Add(30 * time.Second),
	}
	assert.True(t, token.IsExpired()) // 1-minute buffer
}

func TestTokenCachePath(t *testing.T) {
	path := TokenCachePath()
	assert.Contains(t, path, "ralph-o-matic")
	assert.Contains(t, path, "token.json")
}

func TestTokenCache_ForServer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token.json")

	// Save token for server A
	tokenA := &CachedToken{
		AccessToken: "token-a",
		Server:      "https://server-a.com",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	}
	require.NoError(t, saveToken(path, tokenA))

	// Load and check it's for server A
	loaded, err := loadToken(path)
	require.NoError(t, err)
	assert.Equal(t, "https://server-a.com", loaded.Server)
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v -run "TestTokenCache" ./internal/cli/`
Expected: FAIL — types not defined

**Step 3: Write minimal implementation**

Create `internal/cli/auth.go`:

```go
package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// CachedToken represents a locally cached OAuth2 token
type CachedToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at"`
	Server       string    `json:"server"`
}

const tokenExpiryBuffer = 1 * time.Minute

// IsExpired returns true if the token is expired or nearly expired
func (t *CachedToken) IsExpired() bool {
	return time.Now().Add(tokenExpiryBuffer).After(t.ExpiresAt)
}

// TokenCachePath returns the path for the token cache file
func TokenCachePath() string {
	var configDir string
	switch runtime.GOOS {
	case "windows":
		configDir = os.Getenv("APPDATA")
		if configDir == "" {
			configDir = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Roaming")
		}
	default:
		configDir = os.Getenv("XDG_CONFIG_HOME")
		if configDir == "" {
			configDir = filepath.Join(os.Getenv("HOME"), ".config")
		}
	}
	return filepath.Join(configDir, "ralph-o-matic", "token.json")
}

// loadToken loads a cached token from disk. Returns nil if the file doesn't
// exist or is corrupted (corrupted files are silently deleted).
func loadToken(path string) (*CachedToken, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var token CachedToken
	if err := json.Unmarshal(data, &token); err != nil {
		// Corrupted file — delete and treat as missing
		os.Remove(path)
		return nil, nil
	}

	return &token, nil
}

// saveToken saves a token to disk with 0600 permissions
func saveToken(path string, token *CachedToken) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
}

// deleteToken removes the cached token file
func deleteToken(path string) error {
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
```

**Step 4: Run test to verify it passes**

Run: `go test -v -run TestTokenCache ./internal/cli/`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/cli/auth.go internal/cli/auth_test.go
git commit -m "feat(cli): add token cache for CLI auth"
```

---

### Task 3: Create auth config discovery client

**Files:**
- Modify: `internal/cli/auth.go`
- Test: `internal/cli/auth_test.go`

**Step 1: Write the failing test**

Add to `internal/cli/auth_test.go`:

```go
import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
)

func TestDiscoverAuthConfig_ModeNone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/auth/config", r.URL.Path)
		json.NewEncoder(w).Encode(map[string]string{"mode": "none"})
	}))
	defer srv.Close()

	cfg, err := discoverAuthConfig(srv.URL)
	require.NoError(t, err)
	assert.Equal(t, "none", cfg.Mode)
}

func TestDiscoverAuthConfig_ModeEntra(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"mode":      "entra",
			"client_id": "cid-123",
			"tenant_id": "tid-456",
		})
	}))
	defer srv.Close()

	cfg, err := discoverAuthConfig(srv.URL)
	require.NoError(t, err)
	assert.Equal(t, "entra", cfg.Mode)
	assert.Equal(t, "cid-123", cfg.ClientID)
	assert.Equal(t, "tid-456", cfg.TenantID)
}

func TestDiscoverAuthConfig_ServerUnreachable(t *testing.T) {
	_, err := discoverAuthConfig("http://localhost:1")
	assert.Error(t, err)
}

func TestDiscoverAuthConfig_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := discoverAuthConfig(srv.URL)
	assert.Error(t, err)
}

func TestDiscoverAuthConfig_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	_, err := discoverAuthConfig(srv.URL)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "rate limit")
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v -run TestDiscoverAuthConfig ./internal/cli/`
Expected: FAIL — `discoverAuthConfig` not defined

**Step 3: Write minimal implementation**

Add to `internal/cli/auth.go`:

```go
import (
	"fmt"
	"net/http"
)

// authConfigResponse mirrors the server's /auth/config response
type authConfigResponse struct {
	Mode     string `json:"mode"`
	ClientID string `json:"client_id"`
	TenantID string `json:"tenant_id"`
}

// discoverAuthConfig queries the server's auth configuration
func discoverAuthConfig(serverURL string) (*authConfigResponse, error) {
	resp, err := http.Get(serverURL + "/auth/config")
	if err != nil {
		return nil, fmt.Errorf("failed to reach server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("rate limited by server; try again later")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d", resp.StatusCode)
	}

	var cfg authConfigResponse
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("failed to parse auth config: %w", err)
	}

	return &cfg, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test -v -run TestDiscoverAuthConfig ./internal/cli/`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/cli/auth.go internal/cli/auth_test.go
git commit -m "feat(cli): add auth config discovery from server"
```

---

### Task 4: Implement PKCE helpers

**Files:**
- Modify: `internal/cli/auth.go`
- Test: `internal/cli/auth_test.go`

**Step 1: Write the failing test**

Add to `internal/cli/auth_test.go`:

```go
import (
	"crypto/sha256"
	"encoding/base64"
)

func TestGeneratePKCE(t *testing.T) {
	verifier, challenge, err := generatePKCE()
	require.NoError(t, err)

	// Verifier should be 43-128 chars per RFC 7636
	assert.GreaterOrEqual(t, len(verifier), 43)
	assert.LessOrEqual(t, len(verifier), 128)

	// Challenge should be base64url(sha256(verifier))
	h := sha256.Sum256([]byte(verifier))
	expectedChallenge := base64.RawURLEncoding.EncodeToString(h[:])
	assert.Equal(t, expectedChallenge, challenge)
}

func TestGeneratePKCE_Unique(t *testing.T) {
	v1, _, _ := generatePKCE()
	v2, _, _ := generatePKCE()
	assert.NotEqual(t, v1, v2)
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v -run TestGeneratePKCE ./internal/cli/`
Expected: FAIL — `generatePKCE` not defined

**Step 3: Write minimal implementation**

Add to `internal/cli/auth.go`:

```go
import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// generatePKCE creates a PKCE code verifier and challenge per RFC 7636
func generatePKCE() (verifier, challenge string, err error) {
	b := make([]byte, 32) // 32 bytes = 43 chars base64url
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("failed to generate PKCE verifier: %w", err)
	}

	verifier = base64.RawURLEncoding.EncodeToString(b)

	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])

	return verifier, challenge, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test -v -run TestGeneratePKCE ./internal/cli/`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/cli/auth.go internal/cli/auth_test.go
git commit -m "feat(cli): add PKCE verifier/challenge generation"
```

---

### Task 5: Implement browser auth flow with localhost listener

**Files:**
- Modify: `internal/cli/auth.go`
- Test: `internal/cli/auth_test.go`

**Step 1: Write the failing test**

Add to `internal/cli/auth_test.go`:

```go
import (
	"context"
	"net/url"
)

func TestBrowserAuthFlow_Success(t *testing.T) {
	// Mock token endpoint
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token":  "access-abc",
				"refresh_token": "refresh-def",
				"expires_in":    3600,
				"token_type":    "Bearer",
			})
			return
		}
	}))
	defer tokenSrv.Close()

	// Mock browser opener — simulates EntraID callback
	openBrowser := func(authURL string) error {
		// Parse the auth URL to extract redirect_uri and state
		u, err := url.Parse(authURL)
		require.NoError(t, err)
		redirectURI := u.Query().Get("redirect_uri")
		state := u.Query().Get("state")

		// Simulate EntraID calling back with an auth code
		callbackURL := fmt.Sprintf("%s?code=test-code&state=%s", redirectURI, state)
		resp, err := http.Get(callbackURL)
		require.NoError(t, err)
		resp.Body.Close()
		return nil
	}

	flow := &BrowserAuthFlow{
		AuthorizeURL: tokenSrv.URL + "/authorize",
		TokenURL:     tokenSrv.URL + "/token",
		ClientID:     "test-client",
		OpenBrowser:  openBrowser,
	}

	token, err := flow.Run(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "access-abc", token.AccessToken)
	assert.Equal(t, "refresh-def", token.RefreshToken)
}

func TestBrowserAuthFlow_Timeout(t *testing.T) {
	flow := &BrowserAuthFlow{
		AuthorizeURL: "https://login.microsoftonline.com/authorize",
		TokenURL:     "https://login.microsoftonline.com/token",
		ClientID:     "test-client",
		OpenBrowser:  func(url string) error { return nil }, // never calls back
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := flow.Run(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v -run TestBrowserAuthFlow ./internal/cli/`
Expected: FAIL — `BrowserAuthFlow` not defined

**Step 3: Write minimal implementation**

Add to `internal/cli/auth.go`:

```go
import (
	"context"
	"net"
	"strings"
)

// BrowserAuthFlow handles the OAuth2 browser-based authorization code flow with PKCE
type BrowserAuthFlow struct {
	AuthorizeURL string
	TokenURL     string
	ClientID     string
	OpenBrowser  func(url string) error // injectable for testing
}

// Run executes the browser auth flow. It starts a localhost listener,
// opens the browser, waits for the callback, and exchanges the code for tokens.
func (f *BrowserAuthFlow) Run(ctx context.Context) (*CachedToken, error) {
	verifier, challenge, err := generatePKCE()
	if err != nil {
		return nil, err
	}

	// Start temporary localhost listener on a random port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to start localhost listener: %w", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://localhost:%d/callback", port)

	// Generate state for CSRF protection
	stateBytes := make([]byte, 16)
	rand.Read(stateBytes)
	state := base64.RawURLEncoding.EncodeToString(stateBytes)

	// Build authorize URL
	authURL := fmt.Sprintf("%s?client_id=%s&response_type=code&redirect_uri=%s&scope=%s&state=%s&code_challenge=%s&code_challenge_method=S256",
		f.AuthorizeURL,
		f.ClientID,
		url.QueryEscape(redirectURI),
		url.QueryEscape("openid profile offline_access"),
		state,
		challenge,
	)

	// Channel to receive the auth code
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	// Set up callback handler
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			errCh <- fmt.Errorf("state mismatch")
			http.Error(w, "State mismatch", http.StatusBadRequest)
			return
		}

		code := r.URL.Query().Get("code")
		if code == "" {
			errCh <- fmt.Errorf("no authorization code received")
			http.Error(w, "Missing code", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><body><h1>Authentication successful!</h1><p>You can close this tab.</p></body></html>"))
		codeCh <- code
	})

	srv := &http.Server{Handler: mux}
	go srv.Serve(listener)
	defer srv.Close()

	// Open browser
	if err := f.OpenBrowser(authURL); err != nil {
		return nil, fmt.Errorf("failed to open browser: %w", err)
	}

	// Wait for callback or timeout
	var code string
	select {
	case code = <-codeCh:
	case err := <-errCh:
		return nil, err
	case <-ctx.Done():
		return nil, fmt.Errorf("authentication timed out waiting for browser callback")
	}

	// Exchange code for token
	tokenReqBody := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {f.ClientID},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	}

	tokenResp, err := http.Post(f.TokenURL, "application/x-www-form-urlencoded", strings.NewReader(tokenReqBody.Encode()))
	if err != nil {
		return nil, fmt.Errorf("token exchange failed: %w", err)
	}
	defer tokenResp.Body.Close()

	if tokenResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange returned %d", tokenResp.StatusCode)
	}

	var tokenData struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(tokenResp.Body).Decode(&tokenData); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	return &CachedToken{
		AccessToken:  tokenData.AccessToken,
		RefreshToken: tokenData.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(tokenData.ExpiresIn) * time.Second),
	}, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test -v -run TestBrowserAuthFlow ./internal/cli/`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/cli/auth.go internal/cli/auth_test.go
git commit -m "feat(cli): implement browser auth flow with PKCE"
```

---

### Task 6: Integrate auth into CLI Client

**Files:**
- Modify: `internal/cli/client.go`
- Test: `internal/cli/auth_test.go`

**Step 1: Write the failing test**

Add to `internal/cli/auth_test.go`:

```go
func TestClient_AddsAuthHeader_WhenTokenAvailable(t *testing.T) {
	var capturedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer srv.Close()

	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "token.json")
	require.NoError(t, saveToken(tokenPath, &CachedToken{
		AccessToken: "test-bearer-token",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
		Server:      srv.URL,
	}))

	client := NewClient(srv.URL)
	client.SetTokenPath(tokenPath)

	err := client.Ping()
	require.NoError(t, err)
	assert.Equal(t, "Bearer test-bearer-token", capturedAuth)
}

func TestClient_NoAuthHeader_WhenNoToken(t *testing.T) {
	var capturedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	// No token path set

	err := client.Ping()
	require.NoError(t, err)
	assert.Empty(t, capturedAuth)
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v -run "TestClient_AddsAuthHeader|TestClient_NoAuthHeader" ./internal/cli/`
Expected: FAIL — `SetTokenPath` not defined

**Step 3: Modify Client to support auth**

Update `internal/cli/client.go`:

```go
type Client struct {
	baseURL    string
	httpClient *http.Client
	tokenPath  string // path to cached token file
}

// SetTokenPath sets the path to the token cache file
func (c *Client) SetTokenPath(path string) {
	c.tokenPath = path
}

// Update the request method to add Authorization header:
func (c *Client) request(method, path string, body, result interface{}) error {
	// ... existing request building ...

	req, err := http.NewRequest(method, c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	// Add auth token if available
	if c.tokenPath != "" {
		token, err := loadToken(c.tokenPath)
		if err == nil && token != nil && !token.IsExpired() && token.Server == c.baseURL {
			req.Header.Set("Authorization", "Bearer "+token.AccessToken)
		}
	}

	// ... rest of existing method ...
}
```

**Step 4: Run test to verify it passes**

Run: `go test -v -run "TestClient_AddsAuthHeader|TestClient_NoAuthHeader" ./internal/cli/`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/cli/client.go internal/cli/auth_test.go
git commit -m "feat(cli): add Bearer token to API requests from cached token"
```

---

### Task 7: Run full test suite

**Step 1: Run all tests**

Run: `go test -v -short -race ./...`
Expected: All PASS

**Step 2: Run linter**

Run: `make lint`
Expected: No lint errors

---

## Dependencies

- **Depends on:** Phase 4 (Server exposes `/auth/config` endpoint)
- **Blocks:** Nothing (CLI auth is a leaf dependency)

## Reference Files

- Design: `docs/plans/2026-02-03-entraid-sso-design.md` (lines 71-107, "OAuth2 Flows" — CLI flow)
- Design: `docs/plans/2026-02-03-entraid-sso-design.md` (lines 176-199, "CLI Token Caching")
- Design: `docs/plans/2026-02-03-entraid-sso-design.md` (lines 206-244, "CLI Auth User Experience")
- Existing CLI config: `internal/cli/config.go` (path conventions, file permissions)
- Existing CLI client: `internal/cli/client.go` (request pattern)

## Notes for Implementer

- The `BrowserAuthFlow` test mocks the browser open function to simulate the callback directly via HTTP. This avoids needing a real browser in CI.
- Token refresh flow (using refresh_token to get a new access_token silently) should be added once the basic flow works. It follows the same token endpoint POST but with `grant_type=refresh_token`.
- The `pkg/browser` package is only called in production code paths. Tests inject a mock. Import it conditionally or in the CLI commands file.
- For the Cobra command integration (the actual `ralph login` command), that wiring should happen in `cmd/cli/commands.go` — but that's plumbing, not new logic.
