package auth

import "context"

// User represents an authenticated user from EntraID.
type User struct {
	ID    string   // EntraID OID (stable GUID)
	Name  string   // Display name
	Email string   // Email / preferred_username
	Roles []string // App roles from token claims
}

// HasRole reports whether the user has the specified role.
func (u *User) HasRole(role string) bool {
	for _, r := range u.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// IsAdmin reports whether the user has the "Admin" role.
func (u *User) IsAdmin() bool {
	return u.HasRole("Admin")
}

// contextKey is an unexported type for context keys in this package,
// preventing collisions with keys from other packages.
type contextKey struct{}

// ContextWithUser returns a new context carrying the given user.
func ContextWithUser(ctx context.Context, user *User) context.Context {
	return context.WithValue(ctx, contextKey{}, user)
}

// UserFromContext extracts the user from the context, or nil if absent.
func UserFromContext(ctx context.Context) *User {
	user, _ := ctx.Value(contextKey{}).(*User)
	return user
}
