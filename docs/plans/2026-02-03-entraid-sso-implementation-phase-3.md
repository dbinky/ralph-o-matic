# Phase 3: EntraID OIDC Integration Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build the EntraID-specific OIDC integration: discovery document fetching, JWKS caching with rotation support, and JWT validation for ID tokens with role extraction.

**Architecture:** `internal/auth/entra.go` wraps `coreos/go-oidc/v3` for OIDC discovery and ID token verification. `golang.org/x/oauth2` handles OAuth2 flow configuration. The `EntraProvider` struct initializes at startup by fetching the OIDC discovery document and JWKS from `login.microsoftonline.com/{tenant_id}`. Token validation checks `aud` (client ID), `iss` (tenant), `exp`, `nbf`, and extracts `roles`, `preferred_username`, `name`, and `oid` claims.

**Tech Stack:** `github.com/coreos/go-oidc/v3`, `golang.org/x/oauth2`, Go stdlib

---

### Task 1: Add OIDC dependencies

**Files:**
- Modify: `go.mod`

**Step 1: Add the dependencies**

Run:
```bash
go get github.com/coreos/go-oidc/v3@latest
go get golang.org/x/oauth2@latest
```

**Step 2: Verify dependencies resolve**

Run: `go mod tidy`
Expected: Clean exit, `go.mod` and `go.sum` updated

**Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add go-oidc/v3 and golang.org/x/oauth2"
```

---

### Task 2: Create EntraProvider with OIDC discovery

**Files:**
- Create: `internal/auth/entra.go`
- Test: `internal/auth/entra_test.go`

**Step 1: Write the failing test**

Create `internal/auth/entra_test.go`:

```go
package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/go-jose/go-jose.v2"
	jwtlib "gopkg.in/go-jose/go-jose.v2/jwt"
)

// testOIDCServer creates a mock OIDC provider with discovery + JWKS endpoints
func testOIDCServer(t *testing.T, key *rsa.PrivateKey) *httptest.Server {
	t.Helper()

	kid := "test-key-1"

	mux := http.NewServeMux()
	var serverURL string

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"issuer":                 serverURL,
			"authorization_endpoint": serverURL + "/authorize",
			"token_endpoint":         serverURL + "/token",
			"jwks_uri":               serverURL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})

	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		jwks := jose.JSONWebKeySet{
			Keys: []jose.JSONWebKey{
				{
					Key:       &key.PublicKey,
					KeyID:     kid,
					Algorithm: string(jose.RS256),
					Use:       "sig",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jwks)
	})

	srv := httptest.NewServer(mux)
	serverURL = srv.URL
	t.Cleanup(srv.Close)
	return srv
}

// signTestJWT creates a signed JWT for testing
func signTestJWT(t *testing.T, key *rsa.PrivateKey, claims map[string]interface{}) string {
	t.Helper()

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: key},
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

func testRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return key
}

func TestNewEntraProvider(t *testing.T) {
	key := testRSAKey(t)
	srv := testOIDCServer(t, key)

	provider, err := NewEntraProvider(context.Background(), EntraConfig{
		TenantID:     "test-tenant",
		ClientID:     "test-client",
		ClientSecret: "test-secret",
	}, srv.URL)
	require.NoError(t, err)
	assert.NotNil(t, provider)
}

func TestNewEntraProvider_DiscoveryFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := NewEntraProvider(context.Background(), EntraConfig{
		TenantID:     "test-tenant",
		ClientID:     "test-client",
		ClientSecret: "test-secret",
	}, srv.URL)
	assert.Error(t, err)
}

func TestNewEntraProvider_DiscoveryTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second) // hang
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := NewEntraProvider(ctx, EntraConfig{
		TenantID:     "test-tenant",
		ClientID:     "test-client",
		ClientSecret: "test-secret",
	}, srv.URL)
	assert.Error(t, err)
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v -run TestNewEntraProvider ./internal/auth/`
Expected: FAIL — `NewEntraProvider` not defined

**Step 3: Write minimal implementation**

Create `internal/auth/entra.go`:

```go
package auth

import (
	"context"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// EntraProvider wraps OIDC discovery and token validation for EntraID
type EntraProvider struct {
	oidcProvider *oidc.Provider
	verifier     *oidc.IDTokenVerifier
	oauth2Config oauth2.Config
	config       EntraConfig
}

// NewEntraProvider initializes the OIDC provider by fetching the discovery document.
// issuerOverride allows overriding the issuer URL for testing (pass empty for production).
func NewEntraProvider(ctx context.Context, cfg EntraConfig, issuerOverride string) (*EntraProvider, error) {
	issuer := issuerOverride
	if issuer == "" {
		issuer = fmt.Sprintf("https://login.microsoftonline.com/%s/v2.0", cfg.TenantID)
	}

	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("OIDC discovery failed for %s: %w", issuer, err)
	}

	verifier := provider.Verifier(&oidc.Config{
		ClientID: cfg.ClientID,
	})

	oauth2Cfg := oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "offline_access"},
	}

	return &EntraProvider{
		oidcProvider: provider,
		verifier:     verifier,
		oauth2Config: oauth2Cfg,
		config:       cfg,
	}, nil
}

// OAuth2Config returns the OAuth2 config for redirect flows.
// redirectURL must be set by the caller (different for browser vs CLI).
func (p *EntraProvider) OAuth2Config(redirectURL string) oauth2.Config {
	cfg := p.oauth2Config
	cfg.RedirectURL = redirectURL
	return cfg
}

// ClientID returns the configured client ID
func (p *EntraProvider) ClientID() string {
	return p.config.ClientID
}

// TenantID returns the configured tenant ID
func (p *EntraProvider) TenantID() string {
	return p.config.TenantID
}
```

**Step 4: Run test to verify it passes**

Run: `go test -v -run TestNewEntraProvider ./internal/auth/`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/auth/entra.go internal/auth/entra_test.go
git commit -m "feat(auth): add EntraProvider with OIDC discovery"
```

---

### Task 3: Implement JWT validation with role extraction

**Files:**
- Modify: `internal/auth/entra.go`
- Test: `internal/auth/entra_test.go`

**Step 1: Write the failing test**

Add to `internal/auth/entra_test.go`:

```go
func TestEntraProvider_ValidateToken_ValidWithRoles(t *testing.T) {
	key := testRSAKey(t)
	srv := testOIDCServer(t, key)

	provider, err := NewEntraProvider(context.Background(), EntraConfig{
		TenantID:     "test-tenant",
		ClientID:     "test-client",
		ClientSecret: "test-secret",
	}, srv.URL)
	require.NoError(t, err)

	token := signTestJWT(t, key, map[string]interface{}{
		"iss":                srv.URL,
		"aud":                "test-client",
		"exp":                time.Now().Add(1 * time.Hour).Unix(),
		"iat":                time.Now().Unix(),
		"oid":                "user-oid-123",
		"preferred_username": "ryan@contoso.com",
		"name":               "Ryan",
		"roles":              []string{"Admin"},
	})

	user, err := provider.ValidateToken(context.Background(), token)
	require.NoError(t, err)
	assert.Equal(t, "user-oid-123", user.ID)
	assert.Equal(t, "Ryan", user.Name)
	assert.Equal(t, "ryan@contoso.com", user.Email)
	assert.Equal(t, []string{"Admin"}, user.Roles)
}

func TestEntraProvider_ValidateToken_MultipleRoles(t *testing.T) {
	key := testRSAKey(t)
	srv := testOIDCServer(t, key)

	provider, err := NewEntraProvider(context.Background(), EntraConfig{
		TenantID:     "test-tenant",
		ClientID:     "test-client",
		ClientSecret: "test-secret",
	}, srv.URL)
	require.NoError(t, err)

	token := signTestJWT(t, key, map[string]interface{}{
		"iss":                srv.URL,
		"aud":                "test-client",
		"exp":                time.Now().Add(1 * time.Hour).Unix(),
		"iat":                time.Now().Unix(),
		"oid":                "user-oid-123",
		"preferred_username": "ryan@contoso.com",
		"name":               "Ryan",
		"roles":              []string{"User", "Admin"},
	})

	user, err := provider.ValidateToken(context.Background(), token)
	require.NoError(t, err)
	assert.Contains(t, user.Roles, "User")
	assert.Contains(t, user.Roles, "Admin")
}

func TestEntraProvider_ValidateToken_NoRoles(t *testing.T) {
	key := testRSAKey(t)
	srv := testOIDCServer(t, key)

	provider, err := NewEntraProvider(context.Background(), EntraConfig{
		TenantID:     "test-tenant",
		ClientID:     "test-client",
		ClientSecret: "test-secret",
	}, srv.URL)
	require.NoError(t, err)

	token := signTestJWT(t, key, map[string]interface{}{
		"iss":                srv.URL,
		"aud":                "test-client",
		"exp":                time.Now().Add(1 * time.Hour).Unix(),
		"iat":                time.Now().Unix(),
		"oid":                "user-oid-123",
		"preferred_username": "ryan@contoso.com",
		"name":               "Ryan",
	})

	user, err := provider.ValidateToken(context.Background(), token)
	require.NoError(t, err)
	assert.Empty(t, user.Roles)
}

func TestEntraProvider_ValidateToken_ExpiredToken(t *testing.T) {
	key := testRSAKey(t)
	srv := testOIDCServer(t, key)

	provider, err := NewEntraProvider(context.Background(), EntraConfig{
		TenantID:     "test-tenant",
		ClientID:     "test-client",
		ClientSecret: "test-secret",
	}, srv.URL)
	require.NoError(t, err)

	token := signTestJWT(t, key, map[string]interface{}{
		"iss": srv.URL,
		"aud": "test-client",
		"exp": time.Now().Add(-1 * time.Hour).Unix(), // expired
		"iat": time.Now().Add(-2 * time.Hour).Unix(),
		"oid": "user-oid-123",
	})

	_, err = provider.ValidateToken(context.Background(), token)
	assert.Error(t, err)
}

func TestEntraProvider_ValidateToken_WrongAudience(t *testing.T) {
	key := testRSAKey(t)
	srv := testOIDCServer(t, key)

	provider, err := NewEntraProvider(context.Background(), EntraConfig{
		TenantID:     "test-tenant",
		ClientID:     "test-client",
		ClientSecret: "test-secret",
	}, srv.URL)
	require.NoError(t, err)

	token := signTestJWT(t, key, map[string]interface{}{
		"iss": srv.URL,
		"aud": "wrong-client", // wrong audience
		"exp": time.Now().Add(1 * time.Hour).Unix(),
		"iat": time.Now().Unix(),
		"oid": "user-oid-123",
	})

	_, err = provider.ValidateToken(context.Background(), token)
	assert.Error(t, err)
}

func TestEntraProvider_ValidateToken_WrongSigningKey(t *testing.T) {
	key := testRSAKey(t)
	wrongKey := testRSAKey(t) // different key
	srv := testOIDCServer(t, key)

	provider, err := NewEntraProvider(context.Background(), EntraConfig{
		TenantID:     "test-tenant",
		ClientID:     "test-client",
		ClientSecret: "test-secret",
	}, srv.URL)
	require.NoError(t, err)

	token := signTestJWT(t, wrongKey, map[string]interface{}{
		"iss": srv.URL,
		"aud": "test-client",
		"exp": time.Now().Add(1 * time.Hour).Unix(),
		"iat": time.Now().Unix(),
		"oid": "user-oid-123",
	})

	_, err = provider.ValidateToken(context.Background(), token)
	assert.Error(t, err)
}

func TestEntraProvider_ValidateToken_MissingOID(t *testing.T) {
	key := testRSAKey(t)
	srv := testOIDCServer(t, key)

	provider, err := NewEntraProvider(context.Background(), EntraConfig{
		TenantID:     "test-tenant",
		ClientID:     "test-client",
		ClientSecret: "test-secret",
	}, srv.URL)
	require.NoError(t, err)

	token := signTestJWT(t, key, map[string]interface{}{
		"iss":                srv.URL,
		"aud":                "test-client",
		"exp":                time.Now().Add(1 * time.Hour).Unix(),
		"iat":                time.Now().Unix(),
		"preferred_username": "ryan@contoso.com",
		// no oid claim
	})

	_, err = provider.ValidateToken(context.Background(), token)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "oid")
}

func TestEntraProvider_ValidateToken_GarbageInput(t *testing.T) {
	key := testRSAKey(t)
	srv := testOIDCServer(t, key)

	provider, err := NewEntraProvider(context.Background(), EntraConfig{
		TenantID:     "test-tenant",
		ClientID:     "test-client",
		ClientSecret: "test-secret",
	}, srv.URL)
	require.NoError(t, err)

	_, err = provider.ValidateToken(context.Background(), "not-a-jwt")
	assert.Error(t, err)
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v -run TestEntraProvider_ValidateToken ./internal/auth/`
Expected: FAIL — `ValidateToken` not defined

**Step 3: Write minimal implementation**

Add to `internal/auth/entra.go`:

```go
// tokenClaims represents the claims we extract from EntraID ID tokens
type tokenClaims struct {
	OID               string   `json:"oid"`
	PreferredUsername  string   `json:"preferred_username"`
	Name              string   `json:"name"`
	Roles             []string `json:"roles"`
}

// ValidateToken verifies a JWT and extracts the user identity.
// Returns an error if the token is invalid, expired, or missing required claims.
func (p *EntraProvider) ValidateToken(ctx context.Context, rawToken string) (*User, error) {
	idToken, err := p.verifier.Verify(ctx, rawToken)
	if err != nil {
		return nil, fmt.Errorf("token verification failed: %w", err)
	}

	var claims tokenClaims
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("failed to parse token claims: %w", err)
	}

	if claims.OID == "" {
		return nil, fmt.Errorf("token missing required claim: oid")
	}

	return &User{
		ID:    claims.OID,
		Name:  claims.Name,
		Email: claims.PreferredUsername,
		Roles: claims.Roles,
	}, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test -v -run TestEntraProvider_ValidateToken ./internal/auth/`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/auth/entra.go internal/auth/entra_test.go
git commit -m "feat(auth): add JWT validation with role extraction"
```

---

### Task 4: Add OAuth2 token exchange helper

**Files:**
- Modify: `internal/auth/entra.go`
- Test: `internal/auth/entra_test.go`

**Step 1: Write the failing test**

Add to `internal/auth/entra_test.go`:

```go
func TestEntraProvider_ExchangeCode(t *testing.T) {
	key := testRSAKey(t)

	// Set up a mock token endpoint
	tokenHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idToken := signTestJWT(t, key, map[string]interface{}{
			"iss":                "", // will be replaced
			"aud":                "test-client",
			"exp":                time.Now().Add(1 * time.Hour).Unix(),
			"iat":                time.Now().Unix(),
			"oid":                "user-oid-456",
			"preferred_username": "test@contoso.com",
			"name":               "Test User",
			"roles":              []string{"User"},
		})
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token":  "access-token-xyz",
			"token_type":    "Bearer",
			"refresh_token": "refresh-token-xyz",
			"expires_in":    3600,
			"id_token":      idToken,
		})
	})

	mux := http.NewServeMux()
	var serverURL string
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"issuer":                 serverURL,
			"authorization_endpoint": serverURL + "/authorize",
			"token_endpoint":         serverURL + "/token",
			"jwks_uri":               serverURL + "/jwks",
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		jwks := jose.JSONWebKeySet{
			Keys: []jose.JSONWebKey{
				{Key: &key.PublicKey, KeyID: "test-key-1", Algorithm: string(jose.RS256), Use: "sig"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jwks)
	})
	mux.Handle("/token", tokenHandler)

	srv := httptest.NewServer(mux)
	serverURL = srv.URL
	defer srv.Close()

	provider, err := NewEntraProvider(context.Background(), EntraConfig{
		TenantID:     "test-tenant",
		ClientID:     "test-client",
		ClientSecret: "test-secret",
	}, srv.URL)
	require.NoError(t, err)

	oauthCfg := provider.OAuth2Config(srv.URL + "/callback")
	assert.Equal(t, "test-client", oauthCfg.ClientID)
	assert.Equal(t, srv.URL+"/callback", oauthCfg.RedirectURL)
	assert.Contains(t, oauthCfg.Scopes, "openid")
}
```

**Step 2: Run test to verify it passes**

Run: `go test -v -run TestEntraProvider_ExchangeCode ./internal/auth/`
Expected: PASS (OAuth2Config already implemented)

**Step 3: Commit**

```bash
git add internal/auth/entra.go internal/auth/entra_test.go
git commit -m "test(auth): add OAuth2 config test"
```

---

### Task 5: Run full test suite

**Step 1: Run all auth tests with race detector**

Run: `go test -v -race ./internal/auth/`
Expected: All PASS

**Step 2: Run all tests for regressions**

Run: `go test -v -short -race ./...`
Expected: All PASS

**Step 3: Run linter**

Run: `make lint`
Expected: No lint errors

---

## Dependencies

- **Depends on:** Phase 1 (Config types), Phase 2 (User type, context helpers)
- **Blocks:** Phase 4 (Middleware uses EntraProvider for validation)

## Reference Files

- Design: `docs/plans/2026-02-03-entraid-sso-design.md` (lines 247-285, "EntraID App Registration")
- Design: `docs/plans/2026-02-03-entraid-sso-design.md` (lines 593-632, "Testing 11e: EntraID Integration")
- `go-oidc` docs: https://pkg.go.dev/github.com/coreos/go-oidc/v3/oidc
- `golang.org/x/oauth2` docs: https://pkg.go.dev/golang.org/x/oauth2

## Notes for Implementer

- The `go-jose` library used for test JWT signing may need `gopkg.in/go-jose/go-jose.v2` (the version `go-oidc` uses internally). Check `go.mod` after `go get` to see which version was pulled in and use that for test helpers.
- The mock OIDC server in tests uses `httptest.Server` so the issuer URL is dynamic. `NewEntraProvider` accepts an `issuerOverride` parameter specifically for this.
- In production, the issuer is always `https://login.microsoftonline.com/{tenant_id}/v2.0`.
