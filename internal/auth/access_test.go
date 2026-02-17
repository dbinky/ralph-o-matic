package auth

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCanAccessJob_NoUser_ReturnsTrue(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	assert.True(t, CanAccessJob(req, "owner-1"))
}

func TestCanAccessJob_Admin_ReturnsTrue(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(ContextWithUser(req.Context(), &User{
		ID: "admin-1", Roles: []string{"Admin"},
	}))
	assert.True(t, CanAccessJob(req, "other-owner"))
}

func TestCanAccessJob_OwnerMatch_ReturnsTrue(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(ContextWithUser(req.Context(), &User{
		ID: "user-a", Roles: []string{"User"},
	}))
	assert.True(t, CanAccessJob(req, "user-a"))
}

func TestCanAccessJob_OwnerMismatch_ReturnsFalse(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(ContextWithUser(req.Context(), &User{
		ID: "user-a", Roles: []string{"User"},
	}))
	assert.False(t, CanAccessJob(req, "user-b"))
}

func TestCanAccessJob_EmptyOwner_ReturnsTrue(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(ContextWithUser(req.Context(), &User{
		ID: "user-a", Roles: []string{"User"},
	}))
	assert.True(t, CanAccessJob(req, ""))
}
