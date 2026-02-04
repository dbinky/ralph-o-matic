package cli

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CachedToken represents a cached OAuth2 token on disk.
type CachedToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at"`
	Server       string    `json:"server"`
}

// IsExpired returns true if the token expires within 1 minute (buffer for clock skew).
func (t *CachedToken) IsExpired() bool {
	return time.Now().Add(1 * time.Minute).After(t.ExpiresAt)
}

// TokenCachePath returns the path to the token cache file.
func TokenCachePath() string {
	return filepath.Join(configDir(), "token.json")
}

// loadToken loads a cached token from disk.
// Returns nil, nil if the file does not exist.
// Deletes and returns nil, nil if the file contains corrupted JSON.
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
		// Corrupted file — delete and return nil
		os.Remove(path)
		return nil, nil
	}

	return &token, nil
}

// saveToken saves a token to disk with 0600 permissions.
// Creates the directory if it does not exist.
func saveToken(path string, token *CachedToken) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.Marshal(token)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
}

// deleteToken removes the token cache file.
// Returns nil if the file does not exist.
func deleteToken(path string) error {
	err := os.Remove(path)
	if err != nil && os.IsNotExist(err) {
		return nil
	}
	return err
}

// authConfigResponse is the response from GET /auth/config.
type authConfigResponse struct {
	Mode     string `json:"mode"`
	ClientID string `json:"client_id"`
	TenantID string `json:"tenant_id"`
}

// discoverAuthConfig fetches the auth configuration from the server.
func discoverAuthConfig(ctx context.Context, client *http.Client, serverURL string) (*authConfigResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimSuffix(serverURL, "/")+"/auth/config", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to reach server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("rate limit: server returned 429")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	var cfg authConfigResponse
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("failed to parse auth config: %w", err)
	}

	return &cfg, nil
}

// generatePKCE generates a PKCE verifier and challenge per RFC 7636.
// The verifier is 32 random bytes base64url-encoded (43 characters).
// The challenge is base64url(sha256(verifier)).
func generatePKCE() (verifier, challenge string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("failed to generate random bytes: %w", err)
	}

	verifier = base64.RawURLEncoding.EncodeToString(buf)
	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])

	return verifier, challenge, nil
}

// BrowserAuthFlow performs an OAuth2 authorization code flow with PKCE
// using a local browser and a temporary localhost callback server.
type BrowserAuthFlow struct {
	AuthorizeURL string
	TokenURL     string
	ClientID     string
	OpenBrowser  func(url string) error // injectable for testing
}

// tokenResponse is the OAuth2 token endpoint response.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

// Run executes the browser auth flow:
//  1. Generate PKCE verifier + challenge
//  2. Start a temp localhost listener on a random port
//  3. Generate random state
//  4. Build the authorize URL
//  5. Open the browser
//  6. Wait for callback or context cancellation
//  7. Exchange code for token
//  8. Return CachedToken
func (f *BrowserAuthFlow) Run(ctx context.Context) (*CachedToken, error) {
	// 1. Generate PKCE
	verifier, challenge, err := generatePKCE()
	if err != nil {
		return nil, err
	}

	// 2. Start localhost listener
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to start callback listener: %w", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", port)

	// 3. Generate random state
	stateBuf := make([]byte, 16)
	if _, err := rand.Read(stateBuf); err != nil {
		return nil, fmt.Errorf("failed to generate state: %w", err)
	}
	state := base64.RawURLEncoding.EncodeToString(stateBuf)

	// 4. Build authorize URL
	authURL, err := url.Parse(f.AuthorizeURL)
	if err != nil {
		return nil, fmt.Errorf("invalid authorize URL: %w", err)
	}
	q := authURL.Query()
	q.Set("client_id", f.ClientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", redirectURI)
	q.Set("scope", "openid profile offline_access")
	q.Set("state", state)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	authURL.RawQuery = q.Encode()

	// Channel to receive the authorization code
	type callbackResult struct {
		code string
		err  error
	}
	codeCh := make(chan callbackResult, 1)

	// 5. Set up callback handler
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		// Validate state
		if r.URL.Query().Get("state") != state {
			http.Error(w, "invalid state", http.StatusBadRequest)
			return
		}

		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			codeCh <- callbackResult{err: fmt.Errorf("callback missing authorization code")}
			return
		}

		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<html><body><h1>Authentication successful</h1><p>You may close this window.</p></body></html>")

		codeCh <- callbackResult{code: code}
	})

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	// 6. Open browser
	if err := f.OpenBrowser(authURL.String()); err != nil {
		return nil, fmt.Errorf("failed to open browser: %w", err)
	}

	// 7. Wait for callback or context cancellation
	var authCode string
	select {
	case result := <-codeCh:
		if result.err != nil {
			return nil, result.err
		}
		authCode = result.code
	case <-ctx.Done():
		return nil, fmt.Errorf("authentication timed out")
	}

	// 8. Exchange code for token
	formData := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {f.ClientID},
		"code":          {authCode},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	}

	tokenReq, err := http.NewRequestWithContext(ctx, "POST", f.TokenURL, strings.NewReader(formData.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create token request: %w", err)
	}
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	tokenClient := &http.Client{Timeout: 30 * time.Second}
	tokenResp, err := tokenClient.Do(tokenReq)
	if err != nil {
		return nil, fmt.Errorf("token exchange failed: %w", err)
	}
	defer tokenResp.Body.Close()

	if tokenResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token endpoint returned status %d", tokenResp.StatusCode)
	}

	var tok tokenResponse
	if err := json.NewDecoder(tokenResp.Body).Decode(&tok); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	return &CachedToken{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second),
	}, nil
}
