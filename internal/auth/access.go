package auth

import "net/http"

// CanAccessJob reports whether the request's user is allowed to access a job
// with the given ownerID. Returns true when: auth is disabled (no user in
// context), user is admin, job has no owner (pre-auth job), or user owns the job.
func CanAccessJob(r *http.Request, jobOwnerID string) bool {
	user := UserFromContext(r.Context())
	if user == nil {
		return true // auth mode none
	}
	if user.IsAdmin() {
		return true
	}
	if jobOwnerID == "" {
		return true // pre-auth job
	}
	return jobOwnerID == user.ID
}
