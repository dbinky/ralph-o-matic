package auth

import (
	"context"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// tokenClaims represents the JWT claims extracted from an EntraID token.
type tokenClaims struct {
	OID               string   `json:"oid"`
	PreferredUsername string   `json:"preferred_username"`
	Name              string   `json:"name"`
	Roles             []string `json:"roles"`
}

// EntraProvider wraps go-oidc for OIDC discovery, JWKS-based JWT validation,
// and OAuth2 config generation for EntraID (Azure AD).
type EntraProvider struct {
	oidcProvider *oidc.Provider
	verifier     *oidc.IDTokenVerifier
	oauth2Config oauth2.Config
	config       EntraConfig
}

// NewEntraProvider creates a new EntraProvider by fetching the OIDC discovery
// document from the issuer. If issuerOverride is non-empty it is used as the
// issuer URL; otherwise the standard Microsoft v2.0 endpoint is derived from
// the tenant ID.
func NewEntraProvider(ctx context.Context, cfg EntraConfig, issuerOverride string) (*EntraProvider, error) {
	issuer := issuerOverride
	if issuer == "" {
		issuer = fmt.Sprintf("https://login.microsoftonline.com/%s/v2.0", cfg.TenantID)
	}

	oidcProvider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery failed for %s: %w", issuer, err)
	}

	verifier := oidcProvider.Verifier(&oidc.Config{
		ClientID: cfg.ClientID,
	})

	oauth2Cfg := oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint:     oidcProvider.Endpoint(),
		Scopes:       []string{"openid", "profile", "offline_access"},
	}

	return &EntraProvider{
		oidcProvider: oidcProvider,
		verifier:     verifier,
		oauth2Config: oauth2Cfg,
		config:       cfg,
	}, nil
}

// OAuth2Config returns a copy of the OAuth2 config with the given redirect URL.
func (p *EntraProvider) OAuth2Config(redirectURL string) oauth2.Config {
	cfg := p.oauth2Config
	cfg.RedirectURL = redirectURL
	return cfg
}

// ClientID returns the configured client ID.
func (p *EntraProvider) ClientID() string {
	return p.config.ClientID
}

// TenantID returns the configured tenant ID.
func (p *EntraProvider) TenantID() string {
	return p.config.TenantID
}

// ValidateToken verifies a raw JWT string using the OIDC provider's JWKS and
// extracts user information from the token claims. The "oid" claim is required;
// all other claims are optional.
func (p *EntraProvider) ValidateToken(ctx context.Context, rawToken string) (*User, error) {
	idToken, err := p.verifier.Verify(ctx, rawToken)
	if err != nil {
		return nil, fmt.Errorf("token verification failed: %w", err)
	}

	var claims tokenClaims
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("failed to extract token claims: %w", err)
	}

	if claims.OID == "" {
		return nil, fmt.Errorf("token missing required claim: oid")
	}

	user := &User{
		ID:    claims.OID,
		Email: claims.PreferredUsername,
		Name:  claims.Name,
		Roles: claims.Roles,
	}

	// Ensure Roles is never nil -- use empty slice for consistency.
	if user.Roles == nil {
		user.Roles = []string{}
	}

	return user, nil
}
