# SSE Live Updates Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Wire up SSE endpoints so the dashboard auto-refreshes on job state changes and job detail pages stream logs in real time.

**Architecture:** A new `internal/broadcast` package provides a topic-based in-memory pub/sub `Broadcaster`. Queue and LogRepo get an optional Broadcaster field and publish after successful DB writes. Two new SSE HTTP handlers (`/api/events`, `/api/jobs/{id}/events`) subscribe and stream to clients. No new dependencies — stdlib only.

**Tech Stack:** Go stdlib (`sync`, `sync/atomic`, `net/http`, `fmt`, `encoding/json`)

**Design doc:** `docs/plans/2026-02-04-sse-live-updates-design.md` (the original brainstorm output — refer to it for rationale on rejected alternatives, scale targets, and nil-safety decisions)

---

### Task 1: Broadcaster — Failing Tests

**Files:**
- Create: `internal/broadcast/broadcast_test.go`
- Create: `internal/broadcast/broadcast.go` (empty struct only — enough for tests to compile)

**Context:** The Broadcaster is the core pub/sub primitive. We write all tests first, then implement. The test file covers: happy path, multi-subscriber fan-out, topic isolation, no-subscriber publish, slow-client drop, double-unsubscribe, and concurrent access.

**Step 1: Create minimal broadcast.go so tests compile**

```go
package broadcast

import "sync"

// Broadcaster is an in-memory topic-based pub/sub for SSE events.
type Broadcaster struct {
	subscribers map[string]map[uint64]chan []byte
	nextID      uint64
	mu          sync.RWMutex
}

// New creates a new Broadcaster.
func New() *Broadcaster {
	return &Broadcaster{
		subscribers: make(map[string]map[uint64]chan []byte),
	}
}

// Subscribe registers a client for a topic. Returns a client ID and a receive-only channel.
func (b *Broadcaster) Subscribe(topic string) (uint64, <-chan []byte) {
	return 0, nil
}

// Unsubscribe removes a client from a topic.
func (b *Broadcaster) Unsubscribe(topic string, clientID uint64) {}

// Publish sends data to all subscribers of a topic (non-blocking).
func (b *Broadcaster) Publish(topic string, data []byte) {}
```

**Step 2: Write the full test file**

```go
package broadcast

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBroadcaster_SubscribePublishReceive(t *testing.T) {
	b := New()
	_, ch := b.Subscribe("global")

	b.Publish("global", []byte("{}"))

	select {
	case msg := <-ch:
		assert.Equal(t, []byte("{}"), msg)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for message")
	}
}

func TestBroadcaster_MultipleSubscribers(t *testing.T) {
	b := New()
	_, ch1 := b.Subscribe("global")
	_, ch2 := b.Subscribe("global")

	b.Publish("global", []byte("{}"))

	for _, ch := range []<-chan []byte{ch1, ch2} {
		select {
		case msg := <-ch:
			assert.Equal(t, []byte("{}"), msg)
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for message")
		}
	}
}

func TestBroadcaster_TopicIsolation(t *testing.T) {
	b := New()
	_, ch1 := b.Subscribe("job:1")
	_, ch2 := b.Subscribe("job:2")

	b.Publish("job:1", []byte("for-job-1"))

	select {
	case msg := <-ch1:
		assert.Equal(t, []byte("for-job-1"), msg)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for message on job:1")
	}

	select {
	case <-ch2:
		t.Fatal("job:2 subscriber should not receive job:1 event")
	case <-time.After(50 * time.Millisecond):
		// Expected: no message
	}
}

func TestBroadcaster_PublishNoSubscribers(t *testing.T) {
	b := New()
	// Should not panic
	b.Publish("global", []byte("{}"))
}

func TestBroadcaster_SlowClientDropped(t *testing.T) {
	b := New()
	_, slowCh := b.Subscribe("global")
	_, fastCh := b.Subscribe("global")

	// Fill the slow client's buffer (buffer size is 16)
	for i := 0; i < 20; i++ {
		b.Publish("global", []byte("{}"))
	}

	// Fast client should have received messages (up to buffer size)
	received := 0
	for {
		select {
		case <-fastCh:
			received++
		default:
			goto donefast
		}
	}
donefast:
	assert.Equal(t, 16, received, "fast client should receive up to buffer size")

	// Slow client should also have buffer-size messages (first 16)
	received = 0
	for {
		select {
		case <-slowCh:
			received++
		default:
			goto doneslow
		}
	}
doneslow:
	assert.Equal(t, 16, received, "slow client should have buffer-size messages")
}

func TestBroadcaster_Unsubscribe(t *testing.T) {
	b := New()
	id, ch := b.Subscribe("global")

	b.Unsubscribe("global", id)
	b.Publish("global", []byte("{}"))

	select {
	case <-ch:
		t.Fatal("should not receive after unsubscribe")
	case <-time.After(50 * time.Millisecond):
		// Expected
	}
}

func TestBroadcaster_UnsubscribeTwice(t *testing.T) {
	b := New()
	id, _ := b.Subscribe("global")

	b.Unsubscribe("global", id)
	// Should not panic
	b.Unsubscribe("global", id)
}

func TestBroadcaster_UnsubscribeRemovesEmptyTopic(t *testing.T) {
	b := New()
	id, _ := b.Subscribe("global")

	b.Unsubscribe("global", id)

	b.mu.RLock()
	_, exists := b.subscribers["global"]
	b.mu.RUnlock()
	assert.False(t, exists, "empty topic should be removed from map")
}

func TestBroadcaster_UnsubscribeNonexistentTopic(t *testing.T) {
	b := New()
	// Should not panic
	b.Unsubscribe("nonexistent", 999)
}

func TestBroadcaster_ConcurrentAccess(t *testing.T) {
	b := New()
	var wg sync.WaitGroup

	// Concurrent subscribes
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, ch := b.Subscribe("global")
			// Drain channel to avoid blocking publishers
			go func() {
				for range ch {
				}
			}()
			// Unsubscribe after a bit
			time.Sleep(10 * time.Millisecond)
			b.Unsubscribe("global", id)
		}()
	}

	// Concurrent publishes
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.Publish("global", []byte("{}"))
		}()
	}

	wg.Wait()
}

func TestBroadcaster_UniqueClientIDs(t *testing.T) {
	b := New()
	id1, _ := b.Subscribe("global")
	id2, _ := b.Subscribe("global")
	id3, _ := b.Subscribe("other")

	assert.NotEqual(t, id1, id2)
	assert.NotEqual(t, id2, id3)
}
```

**Step 3: Run tests to verify they fail**

Run: `go test -v -race -count=1 ./internal/broadcast/`
Expected: FAIL — Subscribe returns `0, nil`, Publish is a no-op.

**Step 4: Commit**

```bash
git add internal/broadcast/broadcast.go internal/broadcast/broadcast_test.go
git commit -m "test(broadcast): add failing tests for topic-based pub/sub broadcaster"
```

---

### Task 2: Broadcaster — Implementation

**Files:**
- Modify: `internal/broadcast/broadcast.go`

**Context:** Implement the three methods to make all broadcaster tests pass. Key behaviors: atomic client IDs, buffered channels (size 16), non-blocking send on publish, topic cleanup on last unsubscribe.

**Step 1: Implement Subscribe, Unsubscribe, Publish**

Replace the stub methods in `internal/broadcast/broadcast.go` with:

```go
package broadcast

import (
	"sync"
	"sync/atomic"
)

const channelBufferSize = 16

// Broadcaster is an in-memory topic-based pub/sub for SSE events.
type Broadcaster struct {
	subscribers map[string]map[uint64]chan []byte
	nextID      uint64 // accessed via atomic
	mu          sync.RWMutex
}

// New creates a new Broadcaster.
func New() *Broadcaster {
	return &Broadcaster{
		subscribers: make(map[string]map[uint64]chan []byte),
	}
}

// Subscribe registers a client for a topic. Returns a client ID and a receive-only channel.
func (b *Broadcaster) Subscribe(topic string) (uint64, <-chan []byte) {
	id := atomic.AddUint64(&b.nextID, 1)
	ch := make(chan []byte, channelBufferSize)

	b.mu.Lock()
	if b.subscribers[topic] == nil {
		b.subscribers[topic] = make(map[uint64]chan []byte)
	}
	b.subscribers[topic][id] = ch
	b.mu.Unlock()

	return id, ch
}

// Unsubscribe removes a client from a topic.
func (b *Broadcaster) Unsubscribe(topic string, clientID uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	clients, ok := b.subscribers[topic]
	if !ok {
		return
	}

	delete(clients, clientID)
	if len(clients) == 0 {
		delete(b.subscribers, topic)
	}
}

// Publish sends data to all subscribers of a topic. Non-blocking: if a client's
// buffer is full, the event is dropped for that client.
func (b *Broadcaster) Publish(topic string, data []byte) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	clients, ok := b.subscribers[topic]
	if !ok {
		return
	}

	for _, ch := range clients {
		select {
		case ch <- data:
		default:
			// Client buffer full, drop event
		}
	}
}
```

**Step 2: Run tests to verify they pass**

Run: `go test -v -race -count=1 ./internal/broadcast/`
Expected: ALL PASS

**Step 3: Commit**

```bash
git add internal/broadcast/broadcast.go
git commit -m "feat(broadcast): implement topic-based pub/sub broadcaster"
```

---

### Task 3: Queue Broadcasting — Failing Tests

**Files:**
- Modify: `internal/queue/queue_test.go`

**Context:** Add tests that verify each Queue state-change method publishes a `"global"` event via the Broadcaster after a successful DB write. Also test that nil broadcaster doesn't panic. These tests will fail because Queue doesn't have a Broadcaster field yet.

**Step 1: Add the new tests**

Append to `internal/queue/queue_test.go`:

```go
func TestQueue_Enqueue_PublishesBroadcast(t *testing.T) {
	q, _ := newTestQueue(t)
	b := broadcast.New()
	q.SetBroadcaster(b)

	_, ch := b.Subscribe("global")

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, q.Enqueue(job))

	select {
	case msg := <-ch:
		assert.Equal(t, []byte("{}"), msg)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for broadcast")
	}
}

func TestQueue_Dequeue_PublishesBroadcast(t *testing.T) {
	q, _ := newTestQueue(t)
	b := broadcast.New()
	q.SetBroadcaster(b)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, q.Enqueue(job))

	_, ch := b.Subscribe("global")
	_, err := q.Dequeue()
	require.NoError(t, err)

	select {
	case msg := <-ch:
		assert.Equal(t, []byte("{}"), msg)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for broadcast")
	}
}

func TestQueue_StateChanges_PublishBroadcast(t *testing.T) {
	tests := []struct {
		name   string
		action func(q *Queue, job *models.Job) error
		setup  func(q *Queue, job *models.Job) // run before subscribing
	}{
		{
			name:   "Pause",
			setup:  func(q *Queue, job *models.Job) { q.Dequeue() },
			action: func(q *Queue, job *models.Job) error { return q.Pause(job) },
		},
		{
			name: "Resume",
			setup: func(q *Queue, job *models.Job) {
				dequeued, _ := q.Dequeue()
				q.Pause(dequeued)
				*job = *dequeued
			},
			action: func(q *Queue, job *models.Job) error { return q.Resume(job) },
		},
		{
			name:   "Complete",
			setup:  func(q *Queue, job *models.Job) { dequeued, _ := q.Dequeue(); *job = *dequeued },
			action: func(q *Queue, job *models.Job) error { return q.Complete(job) },
		},
		{
			name:   "Fail",
			setup:  func(q *Queue, job *models.Job) { dequeued, _ := q.Dequeue(); *job = *dequeued },
			action: func(q *Queue, job *models.Job) error { return q.Fail(job, "error") },
		},
		{
			name:   "Cancel",
			action: func(q *Queue, job *models.Job) error { return q.Cancel(job) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, _ := newTestQueue(t)
			b := broadcast.New()
			q.SetBroadcaster(b)

			job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
			require.NoError(t, q.Enqueue(job))

			if tt.setup != nil {
				tt.setup(q, job)
			}

			_, ch := b.Subscribe("global")
			require.NoError(t, tt.action(q, job))

			select {
			case msg := <-ch:
				assert.Equal(t, []byte("{}"), msg)
			case <-time.After(time.Second):
				t.Fatal("timed out waiting for broadcast")
			}
		})
	}
}

func TestQueue_Reorder_PublishesBroadcast(t *testing.T) {
	q, _ := newTestQueue(t)
	b := broadcast.New()
	q.SetBroadcaster(b)

	job1 := models.NewJob("git@github.com:user/repo.git", "main", "test1", 10)
	job2 := models.NewJob("git@github.com:user/repo.git", "main", "test2", 10)
	require.NoError(t, q.Enqueue(job1))
	require.NoError(t, q.Enqueue(job2))

	_, ch := b.Subscribe("global")
	require.NoError(t, q.Reorder([]int64{job2.ID, job1.ID}))

	select {
	case msg := <-ch:
		assert.Equal(t, []byte("{}"), msg)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for broadcast")
	}
}

func TestQueue_NilBroadcaster(t *testing.T) {
	q, _ := newTestQueue(t)
	// No broadcaster set — should work without panic

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, q.Enqueue(job))

	dequeued, err := q.Dequeue()
	require.NoError(t, err)
	require.NoError(t, q.Complete(dequeued))
}
```

Also add the imports at the top of the file:

```go
import (
	"testing"
	"time"

	"github.com/ryan/ralph-o-matic/internal/broadcast"
	"github.com/ryan/ralph-o-matic/internal/db"
	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)
```

**Step 2: Run tests to verify they fail**

Run: `go test -v -race -count=1 ./internal/queue/ -run "Broadcast|NilBroadcaster"`
Expected: FAIL — `q.SetBroadcaster` undefined.

**Step 3: Commit**

```bash
git add internal/queue/queue_test.go
git commit -m "test(queue): add failing tests for broadcast on state changes"
```

---

### Task 4: Queue Broadcasting — Implementation

**Files:**
- Modify: `internal/queue/queue.go`

**Context:** Add a Broadcaster field to Queue with a setter method. Add a `publish` helper that checks for nil before calling `Publish`. Call it at the end of each state-change method after a successful DB write.

**Step 1: Add Broadcaster field and publish helper**

Add to the import block in `internal/queue/queue.go`:
```go
"github.com/ryan/ralph-o-matic/internal/broadcast"
```

Add `broadcaster` field to Queue struct:
```go
type Queue struct {
	db          *db.DB
	jobRepo     *db.JobRepo
	mu          sync.RWMutex
	broadcaster *broadcast.Broadcaster
}
```

Add the setter and helper after `New()`:
```go
// SetBroadcaster sets the broadcaster for publishing state change events.
func (q *Queue) SetBroadcaster(b *broadcast.Broadcaster) {
	q.broadcaster = b
}

func (q *Queue) publish() {
	if q.broadcaster != nil {
		q.broadcaster.Publish("global", []byte("{}"))
	}
}
```

**Step 2: Add publish calls to each state-change method**

In each method, add `q.publish()` **after the successful DB write, before the return**. The pattern is:

For `Enqueue`:
```go
func (q *Queue) Enqueue(job *models.Job) error {
	// ... existing code ...
	job.Status = models.StatusQueued
	if err := q.jobRepo.Create(job); err != nil {
		return err
	}
	q.publish()
	return nil
}
```

Change the last line of `Enqueue` from `return q.jobRepo.Create(job)` to the 3-line pattern above.

Apply the same pattern to `Dequeue` (publish before the final `return job, nil`), `Pause`, `Resume`, `Complete`, `Fail`, `Cancel` (publish before `return q.jobRepo.Update(job)` — capture error, check, publish, return), and `Reorder` (publish before `return q.jobRepo.UpdatePositions(jobIDs)` — same capture pattern).

Specifically, each method that currently ends with `return q.jobRepo.Update(job)` becomes:

```go
	if err := q.jobRepo.Update(job); err != nil {
		return err
	}
	q.publish()
	return nil
```

And `Reorder` changes from `return q.jobRepo.UpdatePositions(jobIDs)` to:

```go
	if err := q.jobRepo.UpdatePositions(jobIDs); err != nil {
		return err
	}
	q.publish()
	return nil
```

And `Dequeue` adds `q.publish()` before `return job, nil`.

**Step 3: Run tests to verify they pass**

Run: `go test -v -race -count=1 ./internal/queue/`
Expected: ALL PASS (both new broadcast tests and all existing tests)

**Step 4: Commit**

```bash
git add internal/queue/queue.go
git commit -m "feat(queue): publish broadcast events on state changes"
```

---

### Task 5: LogRepo Broadcasting — Failing Tests

**Files:**
- Modify: `internal/db/logs_test.go`

**Context:** Add tests that verify LogRepo.Append publishes a per-job event with correct JSON payload. Also test nil broadcaster safety and JSON escaping of special characters.

**Step 1: Add the new tests**

Append to `internal/db/logs_test.go`:

```go
func TestLogRepo_Append_PublishesBroadcast(t *testing.T) {
	db := newTestDB(t)
	jobRepo := NewJobRepo(db)
	b := broadcast.New()
	logRepo := NewLogRepo(db)
	logRepo.SetBroadcaster(b)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, jobRepo.Create(job))

	_, ch := b.Subscribe(fmt.Sprintf("job:%d", job.ID))

	err := logRepo.Append(job.ID, 1, "Starting iteration 1")
	require.NoError(t, err)

	select {
	case msg := <-ch:
		var payload map[string]interface{}
		require.NoError(t, json.Unmarshal(msg, &payload))
		assert.Equal(t, "log", payload["type"])
		assert.Equal(t, float64(1), payload["iteration"])
		assert.Equal(t, "Starting iteration 1", payload["message"])
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for broadcast")
	}
}

func TestLogRepo_Append_SpecialCharacters(t *testing.T) {
	db := newTestDB(t)
	jobRepo := NewJobRepo(db)
	b := broadcast.New()
	logRepo := NewLogRepo(db)
	logRepo.SetBroadcaster(b)

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, jobRepo.Create(job))

	_, ch := b.Subscribe(fmt.Sprintf("job:%d", job.ID))

	// Message with quotes, newlines, and unicode
	msg := "error: \"file not found\"\nnext line\ttab \u2603"
	err := logRepo.Append(job.ID, 2, msg)
	require.NoError(t, err)

	select {
	case raw := <-ch:
		var payload map[string]interface{}
		require.NoError(t, json.Unmarshal(raw, &payload), "payload must be valid JSON: %s", string(raw))
		assert.Equal(t, msg, payload["message"])
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for broadcast")
	}
}

func TestLogRepo_Append_NilBroadcaster(t *testing.T) {
	db := newTestDB(t)
	jobRepo := NewJobRepo(db)
	logRepo := NewLogRepo(db)
	// No broadcaster set

	job := models.NewJob("git@github.com:user/repo.git", "main", "test", 10)
	require.NoError(t, jobRepo.Create(job))

	// Should work without panic
	err := logRepo.Append(job.ID, 1, "Hello")
	require.NoError(t, err)
}
```

Also add imports to the test file:

```go
import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/ryan/ralph-o-matic/internal/broadcast"
	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)
```

**Step 2: Run tests to verify they fail**

Run: `go test -v -race -count=1 ./internal/db/ -run "Broadcast|NilBroadcaster"`
Expected: FAIL — `logRepo.SetBroadcaster` undefined.

**Step 3: Commit**

```bash
git add internal/db/logs_test.go
git commit -m "test(db): add failing tests for LogRepo broadcast on Append"
```

---

### Task 6: LogRepo Broadcasting — Implementation

**Files:**
- Modify: `internal/db/logs.go`

**Context:** Add a Broadcaster field to LogRepo with a setter. After successful DB insert in Append, publish a JSON event to the `"job:{id}"` topic. Use `encoding/json.Marshal` for the message field to handle escaping correctly.

**Step 1: Add Broadcaster field, setter, and publish call**

Add to imports in `internal/db/logs.go`:

```go
import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ryan/ralph-o-matic/internal/broadcast"
)
```

Add `broadcaster` field to LogRepo struct:

```go
type LogRepo struct {
	db          *DB
	broadcaster *broadcast.Broadcaster
}
```

Add the setter after `NewLogRepo`:

```go
// SetBroadcaster sets the broadcaster for publishing log events.
func (r *LogRepo) SetBroadcaster(b *broadcast.Broadcaster) {
	r.broadcaster = b
}
```

Modify `Append` to publish after successful insert:

```go
func (r *LogRepo) Append(jobID int64, iteration int, message string) error {
	_, err := r.db.conn.Exec(`
		INSERT INTO job_logs (job_id, iteration, message)
		VALUES (?, ?, ?)
	`, jobID, iteration, message)
	if err != nil {
		return fmt.Errorf("failed to append log: %w", err)
	}

	if r.broadcaster != nil {
		msgJSON, _ := json.Marshal(message)
		payload := fmt.Sprintf(`{"type":"log","iteration":%d,"message":%s}`, iteration, msgJSON)
		r.broadcaster.Publish(fmt.Sprintf("job:%d", jobID), []byte(payload))
	}

	return nil
}
```

Note: `json.Marshal(message)` on a string returns a JSON-quoted string (e.g., `"hello \"world\""`) which is exactly what we need to embed in the JSON object. The `json.Marshal` error for a string is always nil, so we ignore it.

**Step 2: Run tests to verify they pass**

Run: `go test -v -race -count=1 ./internal/db/`
Expected: ALL PASS

**Step 3: Commit**

```bash
git add internal/db/logs.go
git commit -m "feat(db): publish broadcast events on LogRepo.Append"
```

---

### Task 7: SSE Handlers — Failing Tests

**Files:**
- Create: `internal/api/sse_test.go`

**Context:** Test the two SSE endpoints. The test helper needs updating since Server will need a Broadcaster. We use `httptest.NewServer` (not `NewRecorder`) because SSE is long-lived — we need a real HTTP connection that we can read from and then close. Key gotcha: the `middleware.Timeout(60s)` in `setupRoutes` will kill SSE connections. The SSE routes must be registered in a separate route group without the timeout middleware.

**Step 1: Write the SSE handler tests**

```go
package api

import (
	"bufio"
	"fmt"
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
```

**Step 2: Run tests to verify they fail**

Run: `go test -v -race -count=1 ./internal/api/ -run "SSE"`
Expected: FAIL — `ServerOptions` has no `Broadcaster` field, no SSE routes exist.

**Step 3: Commit**

```bash
git add internal/api/sse_test.go
git commit -m "test(api): add failing tests for SSE endpoints"
```

---

### Task 8: SSE Handlers — Implementation

**Files:**
- Modify: `internal/api/server.go` — Add Broadcaster to ServerOptions and Server, register SSE routes, add handler methods

**Context:** Two SSE handler methods. Critical detail: SSE routes must be registered in a route group that does NOT have the `middleware.Timeout(60s)` — otherwise the 60-second timeout will kill SSE connections. We keep SSE routes inside the auth middleware group but create a sub-group without the timeout.

**Step 1: Add Broadcaster to ServerOptions and Server**

In `internal/api/server.go`, add to imports:

```go
"github.com/ryan/ralph-o-matic/internal/broadcast"
```

Add `Broadcaster` field to `ServerOptions`:

```go
type ServerOptions struct {
	AuthProvider *auth.EntraProvider
	Sessions     *auth.SessionStore
	Secure       bool
	Broadcaster  *broadcast.Broadcaster
}
```

Add `broadcaster` field to `Server`:

```go
type Server struct {
	db           *db.DB
	queue        *queue.Queue
	dashboard    *dashboard.Dashboard
	addr         string
	router       chi.Router
	server       *http.Server
	authProvider *auth.EntraProvider
	sessions     *auth.SessionStore
	secure       bool
	broadcaster  *broadcast.Broadcaster
}
```

In `NewServer`, add to the `if opts != nil` block:

```go
s.broadcaster = opts.Broadcaster
```

**Step 2: Register SSE routes outside timeout middleware**

Restructure `setupRoutes` to separate the timeout middleware from SSE routes. The key change: move `middleware.Timeout(60 * time.Second)` from the top-level middleware stack into a sub-group that wraps only the non-SSE routes. SSE routes go in a sibling group with auth but no timeout.

Replace the `setupRoutes` method:

```go
func (s *Server) setupRoutes() {
	r := chi.NewRouter()

	// Middleware (applied to all routes)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware)

	// Health check — always accessible, no auth required
	r.Get("/health", s.handleHealth)

	// Auth routes — accessible without auth middleware
	r.Mount("/auth", auth.NewAuthRoutes(s.authProvider, s.sessions, s.secure))

	// Protected routes — wrapped in auth middleware
	r.Group(func(r chi.Router) {
		r.Use(auth.Middleware(s.authProvider, s.sessions))

		// SSE routes — no timeout (long-lived connections)
		r.Get("/api/events", s.handleSSEGlobal)
		r.Get("/api/jobs/{jobID}/events", s.handleSSEJob)

		// All other routes — with timeout
		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(60 * time.Second))

			// Dashboard
			r.Get("/", s.dashboard.HandleIndex)
			r.Get("/config", s.dashboard.HandleConfig)
			r.Get("/jobs/{jobID}", func(w http.ResponseWriter, r *http.Request) {
				idStr := chi.URLParam(r, "jobID")
				id, err := strconv.ParseInt(idStr, 10, 64)
				if err != nil {
					http.Error(w, "Invalid job ID", http.StatusBadRequest)
					return
				}
				s.dashboard.HandleJob(w, r, id)
			})

			// API routes
			r.Route("/api", func(r chi.Router) {
				r.Route("/jobs", func(r chi.Router) {
					r.Post("/", s.handleCreateJob)
					r.Get("/", s.handleListJobs)
					r.Put("/order", auth.RequireRole("Admin", s.handleReorderJobs))

					r.Route("/{jobID}", func(r chi.Router) {
						r.Get("/", s.handleGetJob)
						r.Delete("/", s.handleCancelJob)
						r.Patch("/", s.handleUpdateJob)
						r.Get("/logs", s.handleGetJobLogs)
						r.Post("/pause", s.handlePauseJob)
						r.Post("/resume", s.handleResumeJob)
					})
				})

				r.Route("/config", func(r chi.Router) {
					r.Get("/", s.handleGetConfig)
					r.Patch("/", auth.RequireRole("Admin", s.handleUpdateConfig))
					r.Post("/test-notify", auth.RequireRole("Admin", s.handleTestNotify))
				})
			})
		})
	})

	s.router = r
}
```

**Step 3: Add SSE handler methods**

Add to `internal/api/server.go` (or create `internal/api/sse.go` — either works; adding to server.go keeps it simple since the handlers are short):

```go
func (s *Server) handleSSEGlobal(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	clientID, ch := s.broadcaster.Subscribe("global")
	defer s.broadcaster.Unsubscribe("global", clientID)

	for {
		select {
		case msg := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) handleSSEJob(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	jobID := chi.URLParam(r, "jobID")

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	topic := "job:" + jobID
	clientID, ch := s.broadcaster.Subscribe(topic)
	defer s.broadcaster.Unsubscribe(topic, clientID)

	for {
		select {
		case msg := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `go test -v -race -count=1 ./internal/api/`
Expected: ALL PASS (both SSE tests and all existing tests)

**Step 5: Commit**

```bash
git add internal/api/server.go
git commit -m "feat(api): add SSE endpoints for dashboard and job log streaming"
```

---

### Task 9: Wire Broadcaster in main.go

**Files:**
- Modify: `cmd/server/main.go`
- Modify: `internal/executor/ralph.go`

**Context:** Create the Broadcaster in `run()`, pass it to Queue (via setter), LogRepo (via setter on executor's logRepo), and Server (via ServerOptions). The tricky part: LogRepo is created in multiple places. The executor creates its own `NewLogRepo(database)` at construction time. We need to either (a) expose the executor's logRepo to set the broadcaster on it, or (b) pass the broadcaster into the executor constructor. Option (b) is simpler — add a setter on RalphHandler.

**Step 1: Update cmd/server/main.go**

Add `broadcast` to imports:

```go
"github.com/ryan/ralph-o-matic/internal/broadcast"
```

In `run()`, after creating `q := queue.New(database)`:

```go
	b := broadcast.New()
	q.SetBroadcaster(b)
```

Update the `serverOpts` construction. Currently there are two paths (auth enabled / auth disabled). For auth enabled, add `Broadcaster: b` to the `ServerOptions`. For auth disabled (nil opts), we now need to pass opts to carry the broadcaster:

Replace the serverOpts / NewServer section. After the auth block, before `srv := api.NewServer(...)`:

```go
	if serverOpts == nil {
		serverOpts = &api.ServerOptions{}
	}
	serverOpts.Broadcaster = b
```

After `handler := executor.NewRalphHandler(...)`, set the broadcaster on the handler's log repo:

```go
	handler.SetLogBroadcaster(b)
```

**Step 2: Add SetLogBroadcaster to RalphHandler**

In `internal/executor/ralph.go`, add to imports:

```go
"github.com/ryan/ralph-o-matic/internal/broadcast"
```

Add method:

```go
// SetLogBroadcaster sets the broadcaster on the handler's LogRepo for live log streaming.
func (h *RalphHandler) SetLogBroadcaster(b *broadcast.Broadcaster) {
	h.logRepo.SetBroadcaster(b)
}
```

**Step 3: Handle other LogRepo instances**

Check if the dashboard and API also create LogRepo instances that need the broadcaster. Looking at the grep results:

- `internal/api/jobs.go:285` — `logRepo := db.NewLogRepo(s.db)` in the job logs REST endpoint. This is a read-only endpoint (GetForJob), so it doesn't call Append. No broadcaster needed.
- `internal/dashboard/dashboard.go:141` — `logRepo := db.NewLogRepo(d.db)` in dashboard job handler. Also read-only. No broadcaster needed.

Only the executor's LogRepo calls `Append`, so only that one needs the broadcaster.

**Step 4: Run full test suite**

Run: `go test -v -race -count=1 ./...`
Expected: ALL PASS

**Step 5: Run linter**

Run: `make lint`
Expected: PASS

**Step 6: Commit**

```bash
git add cmd/server/main.go internal/executor/ralph.go
git commit -m "feat: wire broadcaster into server, queue, and executor"
```

---

### Task 10: Build and Smoke Test

**Files:** None (verification only)

**Context:** Verify the full build succeeds and there are no import cycles or compilation errors.

**Step 1: Build**

Run: `make build`
Expected: Binaries in `build/` with no errors.

**Step 2: Run full test suite with race detector**

Run: `make test`
Expected: ALL PASS with no race conditions detected.

**Step 3: Run linter**

Run: `make lint`
Expected: PASS

**Step 4: Commit (if any fixups needed)**

Only if previous steps required code changes.

---

## Summary of all file changes

### New files
| File | Purpose |
|------|---------|
| `internal/broadcast/broadcast.go` | Topic-based pub/sub Broadcaster |
| `internal/broadcast/broadcast_test.go` | Broadcaster unit tests |
| `internal/api/sse_test.go` | SSE endpoint tests |

### Modified files
| File | Change |
|------|--------|
| `internal/queue/queue.go` | Add `broadcaster` field, `SetBroadcaster()`, `publish()` helper, publish calls in 8 methods |
| `internal/queue/queue_test.go` | Add broadcast and nil-broadcaster tests |
| `internal/db/logs.go` | Add `broadcaster` field, `SetBroadcaster()`, publish in `Append()` |
| `internal/db/logs_test.go` | Add broadcast, special char, and nil-broadcaster tests |
| `internal/api/server.go` | Add `Broadcaster` to ServerOptions/Server, SSE route registration, two handler methods, move timeout middleware to sub-group |
| `internal/executor/ralph.go` | Add `SetLogBroadcaster()` method |
| `cmd/server/main.go` | Create Broadcaster, wire to Queue/Server/Executor |
