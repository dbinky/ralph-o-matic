package auth

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

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testRSAKey generates a 2048-bit RSA key for testing.
func testRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err, "generating RSA key")
	return key
}

// testOIDCServer creates a mock OIDC provider with discovery and JWKS endpoints.
func testOIDCServer(t *testing.T, key *rsa.PrivateKey) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		// We need the server URL, but we don't have it yet when registering handlers.
		// Use the Host header to reconstruct it.
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

// signTestJWT signs a JWT with the given RSA key and claims map.
func signTestJWT(t *testing.T, key *rsa.PrivateKey, claims map[string]interface{}) string {
	t.Helper()

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithHeader(jose.HeaderKey("kid"), "test-key-1"),
	)
	require.NoError(t, err, "creating JWT signer")

	builder := jwt.Signed(signer)
	builder = builder.Claims(claims)

	raw, err := builder.Serialize()
	require.NoError(t, err, "serializing JWT")
	return raw
}

// --- NewEntraProvider tests ---

func TestNewEntraProvider_Success(t *testing.T) {
	key := testRSAKey(t)
	srv := testOIDCServer(t, key)

	cfg := EntraConfig{
		TenantID:     "test-tenant",
		ClientID:     "test-client",
		ClientSecret: "test-secret",
	}

	provider, err := NewEntraProvider(context.Background(), cfg, srv.URL)
	require.NoError(t, err)
	assert.NotNil(t, provider)
	assert.Equal(t, "test-client", provider.ClientID())
	assert.Equal(t, "test-tenant", provider.TenantID())
}

func TestNewEntraProvider_DiscoveryFails(t *testing.T) {
	// Server that returns 500 for discovery
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	cfg := EntraConfig{
		TenantID:     "test-tenant",
		ClientID:     "test-client",
		ClientSecret: "test-secret",
	}

	provider, err := NewEntraProvider(context.Background(), cfg, srv.URL)
	assert.Error(t, err)
	assert.Nil(t, provider)
}

func TestNewEntraProvider_DiscoveryTimeout(t *testing.T) {
	// Server that sleeps longer than the context timeout
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(5 * time.Second):
		case <-r.Context().Done():
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	cfg := EntraConfig{
		TenantID:     "test-tenant",
		ClientID:     "test-client",
		ClientSecret: "test-secret",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	provider, err := NewEntraProvider(ctx, cfg, srv.URL)
	assert.Error(t, err)
	assert.Nil(t, provider)
}

// --- ValidateToken tests ---

func newTestProvider(t *testing.T) (*EntraProvider, *rsa.PrivateKey, *httptest.Server) {
	t.Helper()
	key := testRSAKey(t)
	srv := testOIDCServer(t, key)

	cfg := EntraConfig{
		TenantID:     "test-tenant",
		ClientID:     "test-client",
		ClientSecret: "test-secret",
	}

	provider, err := NewEntraProvider(context.Background(), cfg, srv.URL)
	require.NoError(t, err)
	return provider, key, srv
}

func TestValidateToken_ValidJWT_AllClaims(t *testing.T) {
	provider, key, srv := newTestProvider(t)

	claims := map[string]interface{}{
		"iss":                srv.URL,
		"aud":                "test-client",
		"exp":                time.Now().Add(1 * time.Hour).Unix(),
		"iat":                time.Now().Unix(),
		"oid":                "user-oid-123",
		"preferred_username": "user@example.com",
		"name":               "Test User",
		"roles":              []string{"Admin"},
	}

	token := signTestJWT(t, key, claims)
	user, err := provider.ValidateToken(context.Background(), token)
	require.NoError(t, err)
	assert.Equal(t, "user-oid-123", user.ID)
	assert.Equal(t, "user@example.com", user.Email)
	assert.Equal(t, "Test User", user.Name)
	assert.Equal(t, []string{"Admin"}, user.Roles)
}

func TestValidateToken_MultipleRoles(t *testing.T) {
	provider, key, srv := newTestProvider(t)

	claims := map[string]interface{}{
		"iss":                srv.URL,
		"aud":                "test-client",
		"exp":                time.Now().Add(1 * time.Hour).Unix(),
		"iat":                time.Now().Unix(),
		"oid":                "user-oid-456",
		"preferred_username": "admin@example.com",
		"name":               "Admin User",
		"roles":              []string{"User", "Admin"},
	}

	token := signTestJWT(t, key, claims)
	user, err := provider.ValidateToken(context.Background(), token)
	require.NoError(t, err)
	assert.Equal(t, []string{"User", "Admin"}, user.Roles)
}

func TestValidateToken_NoRoles(t *testing.T) {
	provider, key, srv := newTestProvider(t)

	claims := map[string]interface{}{
		"iss":                srv.URL,
		"aud":                "test-client",
		"exp":                time.Now().Add(1 * time.Hour).Unix(),
		"iat":                time.Now().Unix(),
		"oid":                "user-oid-789",
		"preferred_username": "norole@example.com",
		"name":               "No Role User",
	}

	token := signTestJWT(t, key, claims)
	user, err := provider.ValidateToken(context.Background(), token)
	require.NoError(t, err)
	assert.Equal(t, "user-oid-789", user.ID)
	assert.Empty(t, user.Roles)
}

func TestValidateToken_ExpiredJWT(t *testing.T) {
	provider, key, srv := newTestProvider(t)

	claims := map[string]interface{}{
		"iss": srv.URL,
		"aud": "test-client",
		"exp": time.Now().Add(-1 * time.Hour).Unix(),
		"iat": time.Now().Add(-2 * time.Hour).Unix(),
		"oid": "user-oid-expired",
	}

	token := signTestJWT(t, key, claims)
	user, err := provider.ValidateToken(context.Background(), token)
	assert.Error(t, err)
	assert.Nil(t, user)
}

func TestValidateToken_WrongAudience(t *testing.T) {
	provider, key, srv := newTestProvider(t)

	claims := map[string]interface{}{
		"iss": srv.URL,
		"aud": "wrong-client-id",
		"exp": time.Now().Add(1 * time.Hour).Unix(),
		"iat": time.Now().Unix(),
		"oid": "user-oid-wrongaud",
	}

	token := signTestJWT(t, key, claims)
	user, err := provider.ValidateToken(context.Background(), token)
	assert.Error(t, err)
	assert.Nil(t, user)
}

func TestValidateToken_WrongSigningKey(t *testing.T) {
	provider, _, _ := newTestProvider(t)

	// Sign with a different key
	wrongKey := testRSAKey(t)

	claims := map[string]interface{}{
		"iss": "http://localhost",
		"aud": "test-client",
		"exp": time.Now().Add(1 * time.Hour).Unix(),
		"iat": time.Now().Unix(),
		"oid": "user-oid-wrongkey",
	}

	token := signTestJWT(t, wrongKey, claims)
	user, err := provider.ValidateToken(context.Background(), token)
	assert.Error(t, err)
	assert.Nil(t, user)
}

func TestValidateToken_MissingOID(t *testing.T) {
	provider, key, srv := newTestProvider(t)

	claims := map[string]interface{}{
		"iss":                srv.URL,
		"aud":                "test-client",
		"exp":                time.Now().Add(1 * time.Hour).Unix(),
		"iat":                time.Now().Unix(),
		"preferred_username": "nooid@example.com",
		"name":               "No OID User",
	}

	token := signTestJWT(t, key, claims)
	user, err := provider.ValidateToken(context.Background(), token)
	assert.Error(t, err)
	assert.Nil(t, user)
	assert.Contains(t, err.Error(), "oid")
}

func TestValidateToken_GarbageInput(t *testing.T) {
	provider, _, _ := newTestProvider(t)

	user, err := provider.ValidateToken(context.Background(), "not-a-jwt")
	assert.Error(t, err)
	assert.Nil(t, user)
}

// --- OAuth2Config test ---

func TestOAuth2Config_ReturnsCorrectConfig(t *testing.T) {
	provider, _, _ := newTestProvider(t)

	redirectURL := "http://localhost:9090/auth/callback"
	cfg := provider.OAuth2Config(redirectURL)

	assert.Equal(t, redirectURL, cfg.RedirectURL)
	assert.Equal(t, "test-client", cfg.ClientID)
	assert.Equal(t, "test-secret", cfg.ClientSecret)
	assert.Contains(t, cfg.Scopes, "openid")
	assert.Contains(t, cfg.Scopes, "profile")
	assert.Contains(t, cfg.Scopes, "offline_access")
}
