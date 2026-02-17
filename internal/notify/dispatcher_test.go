package notify

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockNotifier records calls for testing.
type mockNotifier struct {
	mu      sync.Mutex
	name    string
	calls   []mockCall
	err     error
	panicOn bool
}

type mockCall struct {
	Job   *models.Job
	Event Event
}

func (m *mockNotifier) Notify(_ context.Context, job *models.Job, event Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, mockCall{Job: job, Event: event})
	if m.panicOn {
		panic("test panic")
	}
	return m.err
}

func (m *mockNotifier) Name() string { return m.name }

func (m *mockNotifier) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func (m *mockNotifier) lastCall() mockCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls[len(m.calls)-1]
}

// mockConfigProvider returns a fixed config.
type mockConfigProvider struct {
	cfg *models.ServerConfig
	err error
}

func (m *mockConfigProvider) Get() (*models.ServerConfig, error) {
	return m.cfg, m.err
}

func newTestJob() *models.Job {
	job := models.NewJob("https://github.com/user/repo.git", "feature-branch", "test prompt", 10)
	job.ID = 42
	job.OwnerName = "Test User"
	job.Iteration = 5
	now := time.Now()
	job.StartedAt = &now
	return job
}

func smtpEnabledConfig() *models.ServerConfig {
	cfg := models.DefaultServerConfig()
	cfg.Notify.SMTP.Enabled = true
	cfg.Notify.SMTP.Host = "smtp.example.com"
	cfg.Notify.SMTP.Port = 587
	cfg.Notify.SMTP.From = "test@example.com"
	cfg.Notify.SMTP.Recipients = []string{"team@example.com"}
	return cfg
}

func teamsEnabledConfig() *models.ServerConfig {
	cfg := models.DefaultServerConfig()
	cfg.Notify.Teams.Enabled = true
	cfg.Notify.Teams.WebhookURL = "https://outlook.office.com/webhook/test"
	return cfg
}

func bothEnabledConfig() *models.ServerConfig {
	cfg := models.DefaultServerConfig()
	cfg.Notify.SMTP.Enabled = true
	cfg.Notify.SMTP.Host = "smtp.example.com"
	cfg.Notify.SMTP.Port = 587
	cfg.Notify.SMTP.From = "test@example.com"
	cfg.Notify.SMTP.Recipients = []string{"team@example.com"}
	cfg.Notify.Teams.Enabled = true
	cfg.Notify.Teams.WebhookURL = "https://outlook.office.com/webhook/test"
	return cfg
}

// --- Happy Path ---

func TestDispatcher_SingleNotifier_CalledWithCorrectArgs(t *testing.T) {
	mock := &mockNotifier{name: "test"}
	job := newTestJob()

	// Use a custom dispatcher that injects our mock
	d := &testableDispatcher{
		notifiers: []Notifier{mock},
		logger:    slog.Default(),
	}

	d.Notify(context.Background(), job, EventCompleted)

	require.Equal(t, 1, mock.callCount())
	call := mock.lastCall()
	assert.Equal(t, job, call.Job)
	assert.Equal(t, EventCompleted, call.Event)
}

func TestDispatcher_MultipleNotifiers_AllCalled(t *testing.T) {
	mock1 := &mockNotifier{name: "notifier1"}
	mock2 := &mockNotifier{name: "notifier2"}
	job := newTestJob()

	d := &testableDispatcher{
		notifiers: []Notifier{mock1, mock2},
		logger:    slog.Default(),
	}

	d.Notify(context.Background(), job, EventFailed)

	assert.Equal(t, 1, mock1.callCount())
	assert.Equal(t, 1, mock2.callCount())
}

// --- Failure Scenarios ---

func TestDispatcher_FirstFails_SecondStillCalled(t *testing.T) {
	failing := &mockNotifier{name: "failing", err: fmt.Errorf("send failed")}
	working := &mockNotifier{name: "working"}
	job := newTestJob()

	d := &testableDispatcher{
		notifiers: []Notifier{failing, working},
		logger:    slog.Default(),
	}

	d.Notify(context.Background(), job, EventCompleted)

	assert.Equal(t, 1, failing.callCount())
	assert.Equal(t, 1, working.callCount())
}

func TestDispatcher_AllFail_NoPanic(t *testing.T) {
	fail1 := &mockNotifier{name: "fail1", err: fmt.Errorf("error 1")}
	fail2 := &mockNotifier{name: "fail2", err: fmt.Errorf("error 2")}
	job := newTestJob()

	d := &testableDispatcher{
		notifiers: []Notifier{fail1, fail2},
		logger:    slog.Default(),
	}

	// Should not panic
	assert.NotPanics(t, func() {
		d.Notify(context.Background(), job, EventFailed)
	})

	assert.Equal(t, 1, fail1.callCount())
	assert.Equal(t, 1, fail2.callCount())
}

func TestDispatcher_NotifierPanics_OtherStillFires(t *testing.T) {
	panicker := &mockNotifier{name: "panicker", panicOn: true}
	working := &mockNotifier{name: "working"}
	job := newTestJob()

	d := &testableDispatcher{
		notifiers: []Notifier{panicker, working},
		logger:    slog.Default(),
	}

	assert.NotPanics(t, func() {
		d.Notify(context.Background(), job, EventCompleted)
	})

	assert.Equal(t, 1, panicker.callCount())
	assert.Equal(t, 1, working.callCount())
}

// --- Edge Cases ---

func TestDispatcher_ZeroNotifiers_NoOp(t *testing.T) {
	d := &testableDispatcher{
		notifiers: nil,
		logger:    slog.Default(),
	}

	assert.NotPanics(t, func() {
		d.Notify(context.Background(), newTestJob(), EventCompleted)
	})
}

func TestDispatcher_NilJob_Handled(t *testing.T) {
	cp := &mockConfigProvider{cfg: smtpEnabledConfig()}
	d := NewDispatcher(cp, slog.Default())

	// Should not panic
	assert.NotPanics(t, func() {
		d.Notify(context.Background(), nil, EventCompleted)
	})
}

func TestDispatcher_ContextCancelled_PassedToNotifiers(t *testing.T) {
	var receivedCtx context.Context
	mock := &mockNotifier{name: "ctx-checker"}
	// Override Notify to capture context
	contextChecker := &contextCapture{name: "ctx-checker"}
	job := newTestJob()

	_ = mock // not used directly

	d := &testableDispatcher{
		notifiers: []Notifier{contextChecker},
		logger:    slog.Default(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	d.Notify(ctx, job, EventCompleted)

	receivedCtx = contextChecker.lastCtx
	require.NotNil(t, receivedCtx)
	assert.Error(t, receivedCtx.Err())
}

// --- Config-based Dispatcher Tests ---

func TestDispatcher_ConfigProvider_SMTPEnabled(t *testing.T) {
	cp := &mockConfigProvider{cfg: smtpEnabledConfig()}
	d := NewDispatcher(cp, slog.Default())

	notifiers := d.buildNotifiers(cp.cfg)
	require.Len(t, notifiers, 1)
	assert.Equal(t, "smtp", notifiers[0].Name())
}

func TestDispatcher_ConfigProvider_TeamsEnabled(t *testing.T) {
	cp := &mockConfigProvider{cfg: teamsEnabledConfig()}
	d := NewDispatcher(cp, slog.Default())

	notifiers := d.buildNotifiers(cp.cfg)
	require.Len(t, notifiers, 1)
	assert.Equal(t, "teams", notifiers[0].Name())
}

func TestDispatcher_ConfigProvider_BothEnabled(t *testing.T) {
	cp := &mockConfigProvider{cfg: bothEnabledConfig()}
	d := NewDispatcher(cp, slog.Default())

	notifiers := d.buildNotifiers(cp.cfg)
	require.Len(t, notifiers, 2)
}

func TestDispatcher_ConfigProvider_NoneEnabled(t *testing.T) {
	cp := &mockConfigProvider{cfg: models.DefaultServerConfig()}
	d := NewDispatcher(cp, slog.Default())

	notifiers := d.buildNotifiers(cp.cfg)
	assert.Len(t, notifiers, 0)
}

func TestDispatcher_ConfigProvider_Error(t *testing.T) {
	cp := &mockConfigProvider{err: fmt.Errorf("db error")}
	d := NewDispatcher(cp, slog.Default())

	// Should not panic when config provider fails
	assert.NotPanics(t, func() {
		d.Notify(context.Background(), newTestJob(), EventCompleted)
	})
}

// --- Test Helpers ---

// testableDispatcher allows injecting mock notifiers directly.
type testableDispatcher struct {
	notifiers []Notifier
	logger    *slog.Logger
}

func (d *testableDispatcher) Notify(ctx context.Context, job *models.Job, event Event) {
	if job == nil {
		return
	}
	for _, n := range d.notifiers {
		d.callNotifier(ctx, n, job, event)
	}
}

func (d *testableDispatcher) callNotifier(ctx context.Context, n Notifier, job *models.Job, event Event) {
	defer func() {
		if r := recover(); r != nil {
			d.logger.Error("notify: notifier panicked",
				"notifier", n.Name(),
				"job_id", job.ID,
				"panic", fmt.Sprintf("%v", r),
			)
		}
	}()

	if err := n.Notify(ctx, job, event); err != nil {
		d.logger.Error("notify: notifier failed",
			"notifier", n.Name(),
			"job_id", job.ID,
			"event", string(event),
			"error", err,
		)
	}
}

// contextCapture captures the context passed to Notify.
type contextCapture struct {
	mu      sync.Mutex
	name    string
	lastCtx context.Context
}

func (c *contextCapture) Notify(ctx context.Context, _ *models.Job, _ Event) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastCtx = ctx
	return nil
}

func (c *contextCapture) Name() string { return c.name }
