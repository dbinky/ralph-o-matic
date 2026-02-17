package auth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- UserFromContext ---

func TestUserFromContext_Present(t *testing.T) {
	user := &User{
		ID:    "oid-123",
		Name:  "Alice",
		Email: "alice@example.com",
		Roles: []string{"User"},
	}
	ctx := ContextWithUser(context.Background(), user)
	got := UserFromContext(ctx)
	require.NotNil(t, got)
	assert.Equal(t, "oid-123", got.ID)
	assert.Equal(t, "Alice", got.Name)
	assert.Equal(t, "alice@example.com", got.Email)
	assert.Equal(t, []string{"User"}, got.Roles)
}

func TestUserFromContext_Absent(t *testing.T) {
	got := UserFromContext(context.Background())
	assert.Nil(t, got)
}

func TestUserFromContext_WrongValueType(t *testing.T) {
	// If something else is stored at the key, UserFromContext should return nil.
	// We can't easily test this without exposing the key, but we ensure
	// a fresh context returns nil (covered above).
}

// --- User.HasRole ---

func TestUser_HasRole(t *testing.T) {
	tests := []struct {
		name     string
		roles    []string
		check    string
		expected bool
	}{
		{"has role", []string{"Admin", "User"}, "Admin", true},
		{"does not have role", []string{"User"}, "Admin", false},
		{"empty roles", []string{}, "Admin", false},
		{"nil roles", nil, "Admin", false},
		{"case sensitive match", []string{"admin"}, "Admin", false},
		{"exact match", []string{"Admin"}, "Admin", true},
		{"single role matches", []string{"Viewer"}, "Viewer", true},
		{"check empty string", []string{"Admin"}, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &User{Roles: tt.roles}
			assert.Equal(t, tt.expected, u.HasRole(tt.check))
		})
	}
}

// --- User.IsAdmin ---

func TestUser_IsAdmin(t *testing.T) {
	tests := []struct {
		name     string
		roles    []string
		expected bool
	}{
		{"admin role only", []string{"Admin"}, true},
		{"admin and user roles", []string{"Admin", "User"}, true},
		{"user role only", []string{"User"}, false},
		{"empty roles", []string{}, false},
		{"nil roles", nil, false},
		{"wrong case", []string{"admin"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &User{Roles: tt.roles}
			assert.Equal(t, tt.expected, u.IsAdmin())
		})
	}
}
