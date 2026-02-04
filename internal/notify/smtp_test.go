package notify

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSMTPServer is a minimal SMTP server for testing.
type fakeSMTPServer struct {
	addr     string
	listener net.Listener
	messages []fakeSMTPMessage
	rejectRcpt bool
	authFail   bool
}

type fakeSMTPMessage struct {
	From       string
	To         []string
	Data       string
}

func newFakeSMTPServer(t *testing.T) *fakeSMTPServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	s := &fakeSMTPServer{
		addr:     ln.Addr().String(),
		listener: ln,
	}

	go s.serve(t)

	t.Cleanup(func() { ln.Close() })
	return s
}

func (s *fakeSMTPServer) serve(t *testing.T) {
	t.Helper()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return // listener closed
		}
		go s.handleConn(t, conn)
	}
}

func (s *fakeSMTPServer) handleConn(_ *testing.T, conn net.Conn) {
	defer conn.Close()

	// Send greeting
	fmt.Fprintf(conn, "220 fake SMTP server ready\r\n")

	var msg fakeSMTPMessage
	inData := false
	var dataLines []string

	buf := make([]byte, 4096)
	leftover := ""

	for {
		n, err := conn.Read(buf)
		if err != nil {
			return
		}

		input := leftover + string(buf[:n])
		leftover = ""

		lines := strings.Split(input, "\r\n")
		// If the last element isn't empty, it's a partial line
		if len(lines) > 0 && lines[len(lines)-1] != "" {
			leftover = lines[len(lines)-1]
			lines = lines[:len(lines)-1]
		}

		for _, line := range lines {
			if line == "" && !inData {
				continue
			}

			if inData {
				if line == "." {
					inData = false
					msg.Data = strings.Join(dataLines, "\r\n")
					s.messages = append(s.messages, msg)
					fmt.Fprintf(conn, "250 OK\r\n")
					dataLines = nil
					continue
				}
				dataLines = append(dataLines, line)
				continue
			}

			cmd := strings.ToUpper(line)
			switch {
			case strings.HasPrefix(cmd, "EHLO") || strings.HasPrefix(cmd, "HELO"):
				fmt.Fprintf(conn, "250-Hello\r\n")
				fmt.Fprintf(conn, "250 OK\r\n")
			case strings.HasPrefix(cmd, "AUTH"):
				if s.authFail {
					fmt.Fprintf(conn, "535 Authentication failed\r\n")
				} else {
					fmt.Fprintf(conn, "235 Authentication successful\r\n")
				}
			case strings.HasPrefix(cmd, "MAIL FROM:"):
				from := strings.TrimPrefix(line, "MAIL FROM:")
				from = strings.TrimSpace(from)
				from = strings.Trim(from, "<>")
				msg.From = from
				fmt.Fprintf(conn, "250 OK\r\n")
			case strings.HasPrefix(cmd, "RCPT TO:"):
				if s.rejectRcpt {
					fmt.Fprintf(conn, "550 Recipient rejected\r\n")
					continue
				}
				rcpt := strings.TrimPrefix(line, "RCPT TO:")
				rcpt = strings.TrimSpace(rcpt)
				rcpt = strings.Trim(rcpt, "<>")
				msg.To = append(msg.To, rcpt)
				fmt.Fprintf(conn, "250 OK\r\n")
			case strings.HasPrefix(cmd, "DATA"):
				fmt.Fprintf(conn, "354 Start mail input\r\n")
				inData = true
			case strings.HasPrefix(cmd, "QUIT"):
				fmt.Fprintf(conn, "221 Bye\r\n")
				return
			case strings.HasPrefix(cmd, "STARTTLS"):
				// Don't actually support TLS in tests
				fmt.Fprintf(conn, "502 Not supported\r\n")
			default:
				fmt.Fprintf(conn, "500 Unrecognized command\r\n")
			}
		}
	}
}

func (s *fakeSMTPServer) host() string {
	host, _, _ := net.SplitHostPort(s.addr)
	return host
}

func (s *fakeSMTPServer) port() int {
	_, portStr, _ := net.SplitHostPort(s.addr)
	port := 0
	fmt.Sscanf(portStr, "%d", &port)
	return port
}

func smtpTestConfig(s *fakeSMTPServer) models.SMTPConfig {
	return models.SMTPConfig{
		Enabled:    true,
		Host:       s.host(),
		Port:       s.port(),
		From:       "ralph@company.com",
		Recipients: []string{"team@company.com"},
	}
}

func completedJob() *models.Job {
	job := models.NewJob("https://github.com/user/my-repo.git", "feature-x", "implement the thing", 10)
	job.ID = 42
	job.OwnerName = "Alice"
	job.Iteration = 7
	job.PRURL = "https://github.com/user/my-repo/pull/99"
	now := time.Now()
	started := now.Add(-15 * time.Minute)
	job.StartedAt = &started
	job.CompletedAt = &now
	return job
}

func failedJob() *models.Job {
	job := models.NewJob("https://github.com/user/my-repo.git", "fix-bug", "fix the bug", 10)
	job.ID = 43
	job.OwnerName = "Bob"
	job.Iteration = 3
	job.Error = "claude subprocess exited with code 1"
	now := time.Now()
	started := now.Add(-5 * time.Minute)
	job.StartedAt = &started
	job.CompletedAt = &now
	return job
}

func cancelledJob() *models.Job {
	job := models.NewJob("https://github.com/user/my-repo.git", "refactor", "refactor the module", 10)
	job.ID = 44
	job.Iteration = 1
	now := time.Now()
	started := now.Add(-2 * time.Minute)
	job.StartedAt = &started
	job.CompletedAt = &now
	return job
}

// --- Happy Path ---

func TestSMTP_Completed_SendsCorrectEmail(t *testing.T) {
	server := newFakeSMTPServer(t)
	cfg := smtpTestConfig(server)
	notifier := NewSMTPNotifier(cfg)

	job := completedJob()
	err := notifier.Notify(context.Background(), job, EventCompleted)
	require.NoError(t, err)

	require.Len(t, server.messages, 1)
	msg := server.messages[0]
	assert.Equal(t, "ralph@company.com", msg.From)
	assert.Contains(t, msg.To, "team@company.com")
	assert.Contains(t, msg.Data, "Job #42 completed")
	assert.Contains(t, msg.Data, "my-repo/feature-x")
}

func TestSMTP_Failed_SendsEmailWithError(t *testing.T) {
	server := newFakeSMTPServer(t)
	cfg := smtpTestConfig(server)
	notifier := NewSMTPNotifier(cfg)

	job := failedJob()
	err := notifier.Notify(context.Background(), job, EventFailed)
	require.NoError(t, err)

	require.Len(t, server.messages, 1)
	msg := server.messages[0]
	assert.Contains(t, msg.Data, "failed")
	assert.Contains(t, msg.Data, "claude subprocess exited with code 1")
}

func TestSMTP_Cancelled_SendsEmail(t *testing.T) {
	server := newFakeSMTPServer(t)
	cfg := smtpTestConfig(server)
	notifier := NewSMTPNotifier(cfg)

	job := cancelledJob()
	err := notifier.Notify(context.Background(), job, EventCancelled)
	require.NoError(t, err)

	require.Len(t, server.messages, 1)
	msg := server.messages[0]
	assert.Contains(t, msg.Data, "cancelled")
}

// --- Success Scenarios ---

func TestSMTP_IncludesPRURL(t *testing.T) {
	server := newFakeSMTPServer(t)
	cfg := smtpTestConfig(server)
	notifier := NewSMTPNotifier(cfg)

	job := completedJob()
	err := notifier.Notify(context.Background(), job, EventCompleted)
	require.NoError(t, err)

	require.Len(t, server.messages, 1)
	assert.Contains(t, server.messages[0].Data, "https://github.com/user/my-repo/pull/99")
}

func TestSMTP_IncludesOwnerName(t *testing.T) {
	server := newFakeSMTPServer(t)
	cfg := smtpTestConfig(server)
	notifier := NewSMTPNotifier(cfg)

	job := completedJob()
	err := notifier.Notify(context.Background(), job, EventCompleted)
	require.NoError(t, err)

	require.Len(t, server.messages, 1)
	assert.Contains(t, server.messages[0].Data, "Alice")
}

func TestSMTP_IncludesIterationCount(t *testing.T) {
	server := newFakeSMTPServer(t)
	cfg := smtpTestConfig(server)
	notifier := NewSMTPNotifier(cfg)

	job := completedJob()
	err := notifier.Notify(context.Background(), job, EventCompleted)
	require.NoError(t, err)

	require.Len(t, server.messages, 1)
	assert.Contains(t, server.messages[0].Data, "7/10")
}

func TestSMTP_MultipleRecipients(t *testing.T) {
	server := newFakeSMTPServer(t)
	cfg := smtpTestConfig(server)
	cfg.Recipients = []string{"alice@company.com", "bob@company.com", "team@company.com"}
	notifier := NewSMTPNotifier(cfg)

	job := completedJob()
	err := notifier.Notify(context.Background(), job, EventCompleted)
	require.NoError(t, err)

	require.Len(t, server.messages, 1)
	assert.ElementsMatch(t, []string{"alice@company.com", "bob@company.com", "team@company.com"}, server.messages[0].To)
}

// --- Failure Scenarios ---

func TestSMTP_ServerUnreachable_ReturnsError(t *testing.T) {
	cfg := models.SMTPConfig{
		Enabled:    true,
		Host:       "127.0.0.1",
		Port:       1, // nobody listening
		From:       "ralph@company.com",
		Recipients: []string{"team@company.com"},
	}
	notifier := NewSMTPNotifier(cfg)

	err := notifier.Notify(context.Background(), completedJob(), EventCompleted)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "smtp: connection failed")
}

func TestSMTP_RecipientRejected_ReturnsError(t *testing.T) {
	server := newFakeSMTPServer(t)
	server.rejectRcpt = true
	cfg := smtpTestConfig(server)
	notifier := NewSMTPNotifier(cfg)

	err := notifier.Notify(context.Background(), completedJob(), EventCompleted)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "rejected")
}

// --- Edge Cases ---

func TestSMTP_EmptyOwnerName_GracefullyOmits(t *testing.T) {
	server := newFakeSMTPServer(t)
	cfg := smtpTestConfig(server)
	notifier := NewSMTPNotifier(cfg)

	job := completedJob()
	job.OwnerName = ""
	err := notifier.Notify(context.Background(), job, EventCompleted)
	require.NoError(t, err)

	require.Len(t, server.messages, 1)
	assert.NotContains(t, server.messages[0].Data, "Owner:")
}

func TestSMTP_EmptyPRURL_OmitsPRSection(t *testing.T) {
	server := newFakeSMTPServer(t)
	cfg := smtpTestConfig(server)
	notifier := NewSMTPNotifier(cfg)

	job := failedJob()
	job.PRURL = ""
	err := notifier.Notify(context.Background(), job, EventFailed)
	require.NoError(t, err)

	require.Len(t, server.messages, 1)
	assert.NotContains(t, server.messages[0].Data, "Pull Request:")
}

func TestSMTP_EmptyRecipients_ReturnsError(t *testing.T) {
	cfg := models.SMTPConfig{
		Enabled:    true,
		Host:       "smtp.example.com",
		Port:       587,
		From:       "ralph@company.com",
		Recipients: nil,
	}
	notifier := NewSMTPNotifier(cfg)

	err := notifier.Notify(context.Background(), completedJob(), EventCompleted)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no recipients")
}

func TestSMTP_EmptyHost_ReturnsError(t *testing.T) {
	cfg := models.SMTPConfig{
		Enabled:    true,
		Host:       "",
		Port:       587,
		From:       "ralph@company.com",
		Recipients: []string{"team@company.com"},
	}
	notifier := NewSMTPNotifier(cfg)

	err := notifier.Notify(context.Background(), completedJob(), EventCompleted)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no host")
}

func TestSMTP_Name(t *testing.T) {
	notifier := NewSMTPNotifier(models.SMTPConfig{})
	assert.Equal(t, "smtp", notifier.Name())
}

func TestSMTP_BuildSubject(t *testing.T) {
	notifier := NewSMTPNotifier(models.SMTPConfig{})

	tests := []struct {
		name     string
		job      *models.Job
		event    Event
		contains string
	}{
		{
			name:     "completed",
			job:      completedJob(),
			event:    EventCompleted,
			contains: "[ralph-o-matic] Job #42 completed - my-repo/feature-x",
		},
		{
			name:     "failed",
			job:      failedJob(),
			event:    EventFailed,
			contains: "[ralph-o-matic] Job #43 failed - my-repo/fix-bug",
		},
		{
			name:     "cancelled",
			job:      cancelledJob(),
			event:    EventCancelled,
			contains: "[ralph-o-matic] Job #44 cancelled - my-repo/refactor",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			subject := notifier.buildSubject(tc.job, tc.event)
			assert.Equal(t, tc.contains, subject)
		})
	}
}

func TestSMTP_BuildBody_IncludesAllFields(t *testing.T) {
	notifier := NewSMTPNotifier(models.SMTPConfig{})
	job := completedJob()

	body := notifier.buildBody(job, EventCompleted)

	assert.Contains(t, body, "Job #42 completed")
	assert.Contains(t, body, "https://github.com/user/my-repo.git")
	assert.Contains(t, body, "feature-x")
	assert.Contains(t, body, "Alice")
	assert.Contains(t, body, "7/10")
	assert.Contains(t, body, "Pull Request: https://github.com/user/my-repo/pull/99")
}
