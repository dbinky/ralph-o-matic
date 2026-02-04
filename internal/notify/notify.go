package notify

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ryan/ralph-o-matic/internal/models"
)

// Event describes why a notification is being sent.
type Event string

const (
	EventCompleted Event = "completed"
	EventFailed    Event = "failed"
	EventCancelled Event = "cancelled"
)

// Notifier sends a notification about a job event.
type Notifier interface {
	Notify(ctx context.Context, job *models.Job, event Event) error
	Name() string
}

// ConfigProvider reads the current server config.
// This is satisfied by *db.ConfigRepo.
type ConfigProvider interface {
	Get() (*models.ServerConfig, error)
}

// Dispatcher fans out notifications to all enabled notifiers.
// It reads config from the DB on each call so config changes take
// effect immediately without restart.
type Dispatcher struct {
	configProvider ConfigProvider
	logger         *slog.Logger
}

// NewDispatcher creates a dispatcher that reads config per-call.
func NewDispatcher(cp ConfigProvider, logger *slog.Logger) *Dispatcher {
	if logger == nil {
		logger = slog.Default()
	}
	return &Dispatcher{
		configProvider: cp,
		logger:         logger,
	}
}

// Notify sends the event to all enabled notifiers. Errors are logged but
// never returned — notification failure must not affect job processing.
func (d *Dispatcher) Notify(ctx context.Context, job *models.Job, event Event) {
	if job == nil {
		d.logger.Warn("notify: nil job, skipping")
		return
	}

	cfg, err := d.configProvider.Get()
	if err != nil {
		d.logger.Error("notify: failed to load config", "error", err)
		return
	}

	notifiers := d.buildNotifiers(cfg)
	if len(notifiers) == 0 {
		return
	}

	for _, n := range notifiers {
		d.callNotifier(ctx, n, job, event)
	}
}

// buildNotifiers returns enabled notifiers based on current config.
func (d *Dispatcher) buildNotifiers(cfg *models.ServerConfig) []Notifier {
	var notifiers []Notifier

	if cfg.Notify.SMTP.Enabled {
		notifiers = append(notifiers, NewSMTPNotifier(cfg.Notify.SMTP))
	}
	if cfg.Notify.Teams.Enabled {
		notifiers = append(notifiers, NewTeamsNotifier(cfg.Notify.Teams))
	}

	return notifiers
}

// callNotifier calls a single notifier, recovering from panics.
func (d *Dispatcher) callNotifier(ctx context.Context, n Notifier, job *models.Job, event Event) {
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
