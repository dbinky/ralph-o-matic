package api

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ryan/ralph-o-matic/internal/auth"
	"github.com/ryan/ralph-o-matic/internal/broadcast"
	"github.com/ryan/ralph-o-matic/internal/db"
	"github.com/ryan/ralph-o-matic/internal/queue"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestServerWithBroadcaster(t *testing.T) (*Server, *db.DB, *broadcast.Broadcaster) {
	t.Helper()
	database, err := db.New(":memory:")
	require.NoError(t, err)
	require.NoError(t, database.Migrate())
	t.Cleanup(func() { database.Close() })

	b := broadcast.New()
	q := queue.New(database)
	q.SetBroadcaster(b)

	srv := NewServer(database, q, ":9090", &ServerOptions{Broadcaster: b})
	return srv, database, b
}

func TestSSE_GlobalEvents(t *testing.T) {
	srv, _, b := newTestServerWithBroadcaster(t)

	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/events")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
	assert.Equal(t, "no-cache", resp.Header.Get("Cache-Control"))

	// Publish an event
	b.Publish("global", []byte("{}"))

	// Read the SSE line
	scanner := bufio.NewScanner(resp.Body)
	require.True(t, scanner.Scan(), "should read a line")
	line := scanner.Text()
	assert.Equal(t, "data: {}", line)
}

func TestSSE_JobEvents(t *testing.T) {
	srv, database, b := newTestServerWithBroadcaster(t)

	// Create a real job so the ownership check passes (auth mode none)
	job := createJobWithOwner(t, database, "", "")
	jobIDStr := strconv.FormatInt(job.ID, 10)

	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/jobs/" + jobIDStr + "/events")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	payload := `{"type":"log","iteration":1,"message":"hello"}`
	b.Publish("job:"+jobIDStr, []byte(payload))

	scanner := bufio.NewScanner(resp.Body)
	require.True(t, scanner.Scan(), "should read a line")
	line := scanner.Text()
	assert.Equal(t, "data: "+payload, line)
}

func TestSSE_NonexistentJob(t *testing.T) {
	srv, _, _ := newTestServerWithBroadcaster(t)

	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	// Nonexistent job should return 404 now that ownership check fetches the job
	resp, err := http.Get(ts.URL + "/api/jobs/999/events")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestSSE_MultipleClients(t *testing.T) {
	srv, _, b := newTestServerWithBroadcaster(t)

	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	resp1, err := http.Get(ts.URL + "/api/events")
	require.NoError(t, err)
	defer resp1.Body.Close()

	resp2, err := http.Get(ts.URL + "/api/events")
	require.NoError(t, err)
	defer resp2.Body.Close()

	b.Publish("global", []byte("{}"))

	for i, resp := range []*http.Response{resp1, resp2} {
		scanner := bufio.NewScanner(resp.Body)
		require.True(t, scanner.Scan(), "client %d should read a line", i)
		assert.Equal(t, "data: {}", scanner.Text())
	}
}

func TestSSE_ClientDisconnect(t *testing.T) {
	srv, _, b := newTestServerWithBroadcaster(t)

	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/events")
	require.NoError(t, err)

	// Close immediately to simulate disconnect
	resp.Body.Close()

	// Publish should not panic or block
	b.Publish("global", []byte("{}"))
}

func TestSSE_FormatCorrectness(t *testing.T) {
	srv, _, b := newTestServerWithBroadcaster(t)

	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/events")
	require.NoError(t, err)
	defer resp.Body.Close()

	b.Publish("global", []byte("{}"))

	// Read raw bytes to verify exact SSE framing: "data: {}\n\n"
	buf := make([]byte, 256)
	n, err := resp.Body.Read(buf)
	require.NoError(t, err)

	raw := string(buf[:n])
	assert.True(t, strings.HasPrefix(raw, "data: {}"), "should start with 'data: {}'")
	assert.True(t, strings.Contains(raw, "\n\n"), "should contain double newline")
}

// --- SSE Job Events ownership tests ---

func TestSSE_JobEvents_UserCanAccessOwnJob(t *testing.T) {
	srv, database, _ := newTestServerWithBroadcaster(t)
	job := createJobWithOwner(t, database, "user-a", "Alice")

	req := httptest.NewRequest("GET", "/api/jobs/"+strconv.FormatInt(job.ID, 10)+"/events", nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), &auth.User{
		ID: "user-a", Name: "Alice", Roles: []string{"User"},
	}))
	w := httptest.NewRecorder()

	// Run in goroutine since SSE handler blocks; cancel context to unblock
	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()
	req = req.WithContext(ctx)

	done := make(chan struct{})
	go func() {
		srv.Router().ServeHTTP(w, req)
		close(done)
	}()

	// Give handler time to start, then cancel
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestSSE_JobEvents_UserCannotAccessOtherJob(t *testing.T) {
	srv, database, _ := newTestServerWithBroadcaster(t)
	job := createJobWithOwner(t, database, "user-b", "Bob")

	req := httptest.NewRequest("GET", "/api/jobs/"+strconv.FormatInt(job.ID, 10)+"/events", nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), &auth.User{
		ID: "user-a", Name: "Alice", Roles: []string{"User"},
	}))
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestSSE_JobEvents_AdminCanAccessAnyJob(t *testing.T) {
	srv, database, _ := newTestServerWithBroadcaster(t)
	job := createJobWithOwner(t, database, "user-b", "Bob")

	req := httptest.NewRequest("GET", "/api/jobs/"+strconv.FormatInt(job.ID, 10)+"/events", nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), &auth.User{
		ID: "admin-1", Name: "Admin", Roles: []string{"Admin"},
	}))
	w := httptest.NewRecorder()

	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()
	req = req.WithContext(ctx)

	done := make(chan struct{})
	go func() {
		srv.Router().ServeHTTP(w, req)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestSSE_JobEvents_NotFound(t *testing.T) {
	srv, _, _ := newTestServerWithBroadcaster(t)

	req := httptest.NewRequest("GET", "/api/jobs/99999/events", nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), &auth.User{
		ID: "user-a", Name: "Alice", Roles: []string{"User"},
	}))
	w := httptest.NewRecorder()

	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// --- SSE Global Events admin-only tests ---

func TestSSE_GlobalEvents_AdminCanAccess(t *testing.T) {
	srv, _, _ := newTestServerWithBroadcaster(t)

	req := httptest.NewRequest("GET", "/api/events", nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), &auth.User{
		ID: "admin-1", Name: "Admin", Roles: []string{"Admin"},
	}))
	w := httptest.NewRecorder()

	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()
	req = req.WithContext(ctx)

	done := make(chan struct{})
	go func() {
		srv.Router().ServeHTTP(w, req)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestSSE_GlobalEvents_NonAdminCanAccess(t *testing.T) {
	srv, _, _ := newTestServerWithBroadcaster(t)

	req := httptest.NewRequest("GET", "/api/events", nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), &auth.User{
		ID: "user-a", Name: "Alice", Roles: []string{"User"},
	}))
	w := httptest.NewRecorder()

	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()
	req = req.WithContext(ctx)

	done := make(chan struct{})
	go func() {
		srv.Router().ServeHTTP(w, req)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestSSE_GlobalEvents_NoAuth_CanAccess(t *testing.T) {
	srv, _, _ := newTestServerWithBroadcaster(t)

	req := httptest.NewRequest("GET", "/api/events", nil)
	// No auth context — auth mode none
	w := httptest.NewRecorder()

	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()
	req = req.WithContext(ctx)

	done := make(chan struct{})
	go func() {
		srv.Router().ServeHTTP(w, req)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	assert.Equal(t, http.StatusOK, w.Code)
}
