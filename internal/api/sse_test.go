package api

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
	srv, _, b := newTestServerWithBroadcaster(t)

	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/jobs/42/events")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	payload := `{"type":"log","iteration":1,"message":"hello"}`
	b.Publish("job:42", []byte(payload))

	scanner := bufio.NewScanner(resp.Body)
	require.True(t, scanner.Scan(), "should read a line")
	line := scanner.Text()
	assert.Equal(t, "data: "+payload, line)
}

func TestSSE_NonexistentJob(t *testing.T) {
	srv, _, _ := newTestServerWithBroadcaster(t)

	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	// Should connect successfully even for nonexistent job
	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get(ts.URL + "/api/jobs/999/events")
	// Either timeout (no events) or successful connect
	if err == nil {
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
	}
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
