package cli

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Token cache tests ---

func TestTokenCache_SaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	tokenPath := filepath.Join(tmpDir, "token.json")

	token := &CachedToken{
		AccessToken:  "test-access-token",
		RefreshToken: "test-refresh-token",
		ExpiresAt:    time.Now().Add(1 * time.Hour).Truncate(time.Second),
		Server:       "http://localhost:9090",
	}

	err := saveToken(tokenPath, token)
	require.NoError(t, err)

	// Verify file permissions
	info, err := os.Stat(tokenPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())

	// Load and verify round-trip
	loaded, err := loadToken(tokenPath)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, token.AccessToken, loaded.AccessToken)
	assert.Equal(t, token.RefreshToken, loaded.RefreshToken)
	assert.Equal(t, token.ExpiresAt.UTC(), loaded.ExpiresAt.UTC())
	assert.Equal(t, token.Server, loaded.Server)
}

func TestTokenCache_Load_FileNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	tokenPath := filepath.Join(tmpDir, "nonexistent", "token.json")

	loaded, err := loadToken(tokenPath)
	assert.NoError(t, err)
	assert.Nil(t, loaded)
}

func TestTokenCache_Load_CorruptedFile(t *testing.T) {
	tmpDir := t.TempDir()
	tokenPath := filepath.Join(tmpDir, "token.json")

	err := os.WriteFile(tokenPath, []byte("not json at all {{{"), 0600)
	require.NoError(t, err)

	loaded, err := loadToken(tokenPath)
	assert.NoError(t, err)
	assert.Nil(t, loaded)

	// File should be deleted
	_, statErr := os.Stat(tokenPath)
	assert.True(t, os.IsNotExist(statErr))
}

func TestTokenCache_IsExpired(t *testing.T) {
	// Token that expired in the past
	expired := &CachedToken{
		ExpiresAt: time.Now().Add(-1 * time.Hour),
	}
	assert.True(t, expired.IsExpired())

	// Token that expires far in the future
	valid := &CachedToken{
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	assert.False(t, valid.IsExpired())
}

func TestTokenCache_IsExpired_WithBuffer(t *testing.T) {
	// Token expiring in 30 seconds should be treated as expired (1-minute buffer)
	almostExpired := &CachedToken{
		ExpiresAt: time.Now().Add(30 * time.Second),
	}
	assert.True(t, almostExpired.IsExpired())

	// Token expiring in 2 minutes should not be expired
	stillValid := &CachedToken{
		ExpiresAt: time.Now().Add(2 * time.Minute),
	}
	assert.False(t, stillValid.IsExpired())
}

func TestTokenCachePath(t *testing.T) {
	path := TokenCachePath()
	assert.NotEmpty(t, path)
	assert.Contains(t, path, "ralph-o-matic")
	assert.Contains(t, path, "token.json")
}

// --- Auth config discovery tests ---

func TestDiscoverAuthConfig_ModeNone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/auth/config", r.URL.Path)
		json.NewEncoder(w).Encode(map[string]string{"mode": "none"})
	}))
	defer server.Close()

	cfg, err := discoverAuthConfig(context.Background(), &http.Client{Timeout: 5 * time.Second}, server.URL)
	require.NoError(t, err)
	assert.Equal(t, "none", cfg.Mode)
	assert.Empty(t, cfg.ClientID)
	assert.Empty(t, cfg.TenantID)
}

func TestDiscoverAuthConfig_ModeEntra(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/auth/config", r.URL.Path)
		json.NewEncoder(w).Encode(map[string]string{
			"mode":      "entra",
			"client_id": "test-client-id",
			"tenant_id": "test-tenant-id",
		})
	}))
	defer server.Close()

	cfg, err := discoverAuthConfig(context.Background(), &http.Client{Timeout: 5 * time.Second}, server.URL)
	require.NoError(t, err)
	assert.Equal(t, "entra", cfg.Mode)
	assert.Equal(t, "test-client-id", cfg.ClientID)
	assert.Equal(t, "test-tenant-id", cfg.TenantID)
}

func TestDiscoverAuthConfig_ServerUnreachable(t *testing.T) {
	// Port 1 should not be listening
	_, err := discoverAuthConfig(context.Background(), &http.Client{Timeout: 2 * time.Second}, "http://127.0.0.1:1")
	assert.Error(t, err)
}

func TestDiscoverAuthConfig_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	_, err := discoverAuthConfig(context.Background(), &http.Client{Timeout: 5 * time.Second}, server.URL)
	assert.Error(t, err)
}

func TestDiscoverAuthConfig_RateLimited(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	_, err := discoverAuthConfig(context.Background(), &http.Client{Timeout: 5 * time.Second}, server.URL)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "rate limit")
}

// --- PKCE tests ---

func TestGeneratePKCE(t *testing.T) {
	verifier, challenge, err := generatePKCE()
	require.NoError(t, err)

	// Verifier should be at least 43 characters (32 bytes base64url = 43 chars)
	assert.GreaterOrEqual(t, len(verifier), 43)

	// Challenge should be base64url(sha256(verifier))
	h := sha256.Sum256([]byte(verifier))
	expectedChallenge := base64.RawURLEncoding.EncodeToString(h[:])
	assert.Equal(t, expectedChallenge, challenge)
}

func TestGeneratePKCE_Unique(t *testing.T) {
	v1, _, err := generatePKCE()
	require.NoError(t, err)

	v2, _, err := generatePKCE()
	require.NoError(t, err)

	assert.NotEqual(t, v1, v2)
}

// --- Browser auth flow tests ---

func TestBrowserAuthFlow_Success(t *testing.T) {
	// Mock token endpoint
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))

		err := r.ParseForm()
		require.NoError(t, err)
		assert.Equal(t, "authorization_code", r.Form.Get("grant_type"))
		assert.Equal(t, "test-client-id", r.Form.Get("client_id"))
		assert.NotEmpty(t, r.Form.Get("code"))
		assert.NotEmpty(t, r.Form.Get("redirect_uri"))
		assert.NotEmpty(t, r.Form.Get("code_verifier"))

		resp := map[string]interface{}{
			"access_token":  "test-access-token-from-flow",
			"refresh_token": "test-refresh-token-from-flow",
			"expires_in":    3600,
			"token_type":    "Bearer",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer tokenServer.Close()

	flow := &BrowserAuthFlow{
		AuthorizeURL: "http://127.0.0.1:0/authorize", // placeholder; we parse redirect_uri from the actual URL
		TokenURL:     tokenServer.URL + "/token",
		ClientID:     "test-client-id",
		OpenBrowser: func(authURL string) error {
			// Parse the auth URL to extract redirect_uri and state
			parsed, err := url.Parse(authURL)
			if err != nil {
				return err
			}

			redirectURI := parsed.Query().Get("redirect_uri")
			state := parsed.Query().Get("state")

			// Simulate the browser callback
			callbackURL := fmt.Sprintf("%s?code=test-auth-code&state=%s", redirectURI, url.QueryEscape(state))

			resp, err := http.Get(callbackURL)
			if err != nil {
				return err
			}
			resp.Body.Close()
			return nil
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	token, err := flow.Run(ctx)
	require.NoError(t, err)
	require.NotNil(t, token)
	assert.Equal(t, "test-access-token-from-flow", token.AccessToken)
	assert.Equal(t, "test-refresh-token-from-flow", token.RefreshToken)
	assert.False(t, token.IsExpired())
}

func TestBrowserAuthFlow_Timeout(t *testing.T) {
	flow := &BrowserAuthFlow{
		AuthorizeURL: "http://127.0.0.1:0/authorize",
		TokenURL:     "http://127.0.0.1:0/token",
		ClientID:     "test-client-id",
		OpenBrowser: func(url string) error {
			// Do nothing - simulate user not completing auth
			return nil
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := flow.Run(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
}

// --- Client auth header tests ---

func TestClient_AddsAuthHeader_WhenTokenAvailable(t *testing.T) {
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode(map[string]interface{}{})
	}))
	defer server.Close()

	// Save a valid token
	tmpDir := t.TempDir()
	tokenPath := filepath.Join(tmpDir, "token.json")
	token := &CachedToken{
		AccessToken:  "my-bearer-token",
		RefreshToken: "my-refresh-token",
		ExpiresAt:    time.Now().Add(1 * time.Hour),
		Server:       server.URL,
	}
	err := saveToken(tokenPath, token)
	require.NoError(t, err)

	client := NewClient(server.URL)
	client.SetTokenPath(tokenPath)

	err = client.get("/health", nil)
	require.NoError(t, err)

	assert.Equal(t, "Bearer my-bearer-token", authHeader)
}

func TestClient_NoAuthHeader_WhenNoToken(t *testing.T) {
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode(map[string]interface{}{})
	}))
	defer server.Close()

	client := NewClient(server.URL)
	// No token path set

	err := client.get("/health", nil)
	require.NoError(t, err)

	assert.Empty(t, authHeader)
}

func TestClient_NoAuthHeader_WhenTokenExpired(t *testing.T) {
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode(map[string]interface{}{})
	}))
	defer server.Close()

	// Save an expired token
	tmpDir := t.TempDir()
	tokenPath := filepath.Join(tmpDir, "token.json")
	token := &CachedToken{
		AccessToken:  "expired-token",
		RefreshToken: "my-refresh-token",
		ExpiresAt:    time.Now().Add(-1 * time.Hour),
		Server:       server.URL,
	}
	err := saveToken(tokenPath, token)
	require.NoError(t, err)

	client := NewClient(server.URL)
	client.SetTokenPath(tokenPath)

	err = client.get("/health", nil)
	require.NoError(t, err)

	assert.Empty(t, authHeader)
}

func TestClient_AddsAPIKey_WhenSet(t *testing.T) {
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode(map[string]interface{}{})
	}))
	defer server.Close()

	client := NewClient(server.URL)
	client.SetAPIKey("my-static-api-key")

	err := client.get("/health", nil)
	require.NoError(t, err)

	assert.Equal(t, "Bearer my-static-api-key", authHeader)
}

func TestClient_APIKey_TakesPrecedenceOverToken(t *testing.T) {
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode(map[string]interface{}{})
	}))
	defer server.Close()

	// Save a valid Entra token
	tmpDir := t.TempDir()
	tokenPath := filepath.Join(tmpDir, "token.json")
	token := &CachedToken{
		AccessToken: "entra-token",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
		Server:      server.URL,
	}
	require.NoError(t, saveToken(tokenPath, token))

	client := NewClient(server.URL)
	client.SetTokenPath(tokenPath)
	client.SetAPIKey("static-key") // should win

	err := client.get("/health", nil)
	require.NoError(t, err)

	assert.Equal(t, "Bearer static-key", authHeader)
}

func TestClient_NoAuthHeader_WhenServerMismatch(t *testing.T) {
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode(map[string]interface{}{})
	}))
	defer server.Close()

	// Save a token for a different server
	tmpDir := t.TempDir()
	tokenPath := filepath.Join(tmpDir, "token.json")
	token := &CachedToken{
		AccessToken:  "wrong-server-token",
		RefreshToken: "my-refresh-token",
		ExpiresAt:    time.Now().Add(1 * time.Hour),
		Server:       "http://other-server:9090",
	}
	err := saveToken(tokenPath, token)
	require.NoError(t, err)

	client := NewClient(server.URL)
	client.SetTokenPath(tokenPath)

	err = client.get("/health", nil)
	require.NoError(t, err)

	assert.Empty(t, authHeader)
}

func TestDeleteToken(t *testing.T) {
	tmpDir := t.TempDir()
	tokenPath := filepath.Join(tmpDir, "token.json")

	// Save a token first
	token := &CachedToken{
		AccessToken: "to-delete",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
		Server:      "http://localhost:9090",
	}
	err := saveToken(tokenPath, token)
	require.NoError(t, err)

	// Delete it
	err = deleteToken(tokenPath)
	require.NoError(t, err)

	// Verify it's gone
	_, statErr := os.Stat(tokenPath)
	assert.True(t, os.IsNotExist(statErr))

	// Deleting again should not error
	err = deleteToken(tokenPath)
	assert.NoError(t, err)
}

func TestDiscoverAuthConfig_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer server.Close()

	_, err := discoverAuthConfig(context.Background(), &http.Client{Timeout: 5 * time.Second}, server.URL)
	assert.Error(t, err)
}

func TestSaveToken_CreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	tokenPath := filepath.Join(tmpDir, "nested", "dir", "token.json")

	token := &CachedToken{
		AccessToken: "test",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
		Server:      "http://localhost:9090",
	}
	err := saveToken(tokenPath, token)
	require.NoError(t, err)

	loaded, err := loadToken(tokenPath)
	require.NoError(t, err)
	assert.Equal(t, "test", loaded.AccessToken)
}

func TestBrowserAuthFlow_InvalidState(t *testing.T) {
	flow := &BrowserAuthFlow{
		AuthorizeURL: "http://127.0.0.1:0/authorize",
		TokenURL:     "http://127.0.0.1:0/token",
		ClientID:     "test-client-id",
		OpenBrowser: func(authURL string) error {
			parsed, err := url.Parse(authURL)
			if err != nil {
				return err
			}
			redirectURI := parsed.Query().Get("redirect_uri")
			// Send wrong state
			callbackURL := fmt.Sprintf("%s?code=test-auth-code&state=wrong-state", redirectURI)
			resp, err := http.Get(callbackURL)
			if err != nil {
				return err
			}
			resp.Body.Close()
			return nil
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := flow.Run(ctx)
	assert.Error(t, err)
	// Should time out because the invalid state callback is rejected
	assert.True(t, strings.Contains(err.Error(), "timed out") || strings.Contains(err.Error(), "state"))
}
