package notify

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/ryan/ralph-o-matic/internal/models"
)

// SMTPNotifier sends email notifications via SMTP.
type SMTPNotifier struct {
	config models.SMTPConfig
}

// NewSMTPNotifier creates an SMTP notifier with the given config.
func NewSMTPNotifier(cfg models.SMTPConfig) *SMTPNotifier {
	return &SMTPNotifier{config: cfg}
}

// Name returns the notifier name.
func (s *SMTPNotifier) Name() string { return "smtp" }

// Notify sends an email notification about a job event.
func (s *SMTPNotifier) Notify(ctx context.Context, job *models.Job, event Event) error {
	if len(s.config.Recipients) == 0 {
		return fmt.Errorf("smtp: no recipients configured")
	}
	if s.config.Host == "" {
		return fmt.Errorf("smtp: no host configured")
	}

	subject := s.buildSubject(job, event)
	body := s.buildBody(job, event)
	msg := s.buildMessage(subject, body)

	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)

	// Use a dialer with timeout
	dialer := &net.Dialer{Timeout: 10 * time.Second}

	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("smtp: connection failed: %w", err)
	}

	client, err := smtp.NewClient(conn, s.config.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp: client creation failed: %w", err)
	}
	defer client.Close()

	// Try STARTTLS
	if ok, _ := client.Extension("STARTTLS"); ok {
		tlsCfg := &tls.Config{ServerName: s.config.Host, MinVersion: tls.VersionTLS12}
		if err := client.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("smtp: STARTTLS failed: %w", err)
		}
	}

	// Authenticate if credentials are provided
	if s.config.Username != "" {
		auth := smtp.PlainAuth("", s.config.Username, s.config.Password, s.config.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp: auth failed: %w", err)
		}
	}

	// Set sender
	if err := client.Mail(s.config.From); err != nil {
		return fmt.Errorf("smtp: sender rejected: %w", err)
	}

	// Set recipients
	for _, rcpt := range s.config.Recipients {
		rcpt = strings.TrimSpace(rcpt)
		if rcpt == "" {
			continue
		}
		if err := client.Rcpt(rcpt); err != nil {
			return fmt.Errorf("smtp: recipient %q rejected: %w", rcpt, err)
		}
	}

	// Send message
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp: data command failed: %w", err)
	}

	if _, err := w.Write([]byte(msg)); err != nil {
		return fmt.Errorf("smtp: write failed: %w", err)
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp: close data failed: %w", err)
	}

	return client.Quit()
}

func (s *SMTPNotifier) buildSubject(job *models.Job, event Event) string {
	repo := job.RepoURL
	// Extract short repo name if possible
	if idx := strings.LastIndex(repo, "/"); idx >= 0 {
		repo = repo[idx+1:]
	}
	repo = strings.TrimSuffix(repo, ".git")

	return fmt.Sprintf("[ralph-o-matic] Job #%d %s - %s/%s", job.ID, event, repo, job.Branch)
}

func (s *SMTPNotifier) buildBody(job *models.Job, event Event) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Job #%d %s\n\n", job.ID, event)
	fmt.Fprintf(&b, "Repository: %s\n", job.RepoURL)
	fmt.Fprintf(&b, "Branch:     %s\n", job.Branch)

	if job.OwnerName != "" {
		fmt.Fprintf(&b, "Owner:      %s\n", job.OwnerName)
	}

	fmt.Fprintf(&b, "Iterations: %d/%d\n", job.Iteration, job.MaxIterations)
	fmt.Fprintf(&b, "Duration:   %s\n", job.Duration().Truncate(time.Second))

	if job.PRURL != "" {
		fmt.Fprintf(&b, "\nPull Request: %s\n", job.PRURL)
	}

	if job.Error != "" {
		fmt.Fprintf(&b, "\nError: %s\n", job.Error)
	}

	return b.String()
}

func (s *SMTPNotifier) buildMessage(subject, body string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "From: %s\r\n", s.config.From)
	fmt.Fprintf(&b, "To: %s\r\n", strings.Join(s.config.Recipients, ", "))
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	fmt.Fprintf(&b, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&b, "Content-Type: text/plain; charset=UTF-8\r\n")
	fmt.Fprintf(&b, "\r\n")
	fmt.Fprintf(&b, "%s", body)

	return b.String()
}
