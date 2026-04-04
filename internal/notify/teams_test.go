package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ryan/ralph-o-matic/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type capturedPayload struct {
	body       []byte
	parsed     map[string]interface{}
	statusCode int
}

func newTeamsTestServer(t *testing.T, statusCode int) (*httptest.Server, *capturedPayload) {
	t.Helper()
	captured := &capturedPayload{statusCode: statusCode}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		captured.body = body

		var parsed map[string]interface{}
		if err := json.Unmarshal(body, &parsed); err == nil {
			captured.parsed = parsed
		}

		w.WriteHeader(captured.statusCode)
		w.Write([]byte("OK"))
	}))

	t.Cleanup(server.Close)
	return server, captured
}

// --- Happy Path ---

func TestTeams_Completed_SendsGreenCard(t *testing.T) {
	server, captured := newTeamsTestServer(t, http.StatusOK)

	cfg := models.TeamsConfig{Enabled: true, WebhookURL: server.URL}
	notifier := NewTeamsNotifier(cfg)

	job := completedJob()
	err := notifier.Notify(context.Background(), job, EventCompleted)
	require.NoError(t, err)

	require.NotNil(t, captured.parsed)
	assert.Equal(t, "00FF00", captured.parsed["themeColor"])
	assert.Contains(t, captured.parsed["summary"], "Job #42")
	assert.Contains(t, captured.parsed["summary"], "Completed")
}

func TestTeams_Failed_SendsRedCard(t *testing.T) {
	server, captured := newTeamsTestServer(t, http.StatusOK)

	cfg := models.TeamsConfig{Enabled: true, WebhookURL: server.URL}
	notifier := NewTeamsNotifier(cfg)

	job := failedJob()
	err := notifier.Notify(context.Background(), job, EventFailed)
	require.NoError(t, err)

	require.NotNil(t, captured.parsed)
	assert.Equal(t, "FF0000", captured.parsed["themeColor"])
}

func TestTeams_Cancelled_SendsYellowCard(t *testing.T) {
	server, captured := newTeamsTestServer(t, http.StatusOK)

	cfg := models.TeamsConfig{Enabled: true, WebhookURL: server.URL}
	notifier := NewTeamsNotifier(cfg)

	job := cancelledJob()
	err := notifier.Notify(context.Background(), job, EventCancelled)
	require.NoError(t, err)

	require.NotNil(t, captured.parsed)
	assert.Equal(t, "FFFF00", captured.parsed["themeColor"])
}

// --- Success Scenarios ---

func TestTeams_IncludesOwnerAndRepoAndBranch(t *testing.T) {
	server, captured := newTeamsTestServer(t, http.StatusOK)

	cfg := models.TeamsConfig{Enabled: true, WebhookURL: server.URL}
	notifier := NewTeamsNotifier(cfg)

	job := completedJob()
	err := notifier.Notify(context.Background(), job, EventCompleted)
	require.NoError(t, err)

	bodyStr := string(captured.body)
	assert.Contains(t, bodyStr, "Alice")
	assert.Contains(t, bodyStr, "https://github.com/user/my-repo.git")
	assert.Contains(t, bodyStr, "feature-x")
	assert.Contains(t, bodyStr, "7/10")
}

func TestTeams_CompletedWithPR_HasActionButton(t *testing.T) {
	server, captured := newTeamsTestServer(t, http.StatusOK)

	cfg := models.TeamsConfig{Enabled: true, WebhookURL: server.URL}
	notifier := NewTeamsNotifier(cfg)

	job := completedJob()
	err := notifier.Notify(context.Background(), job, EventCompleted)
	require.NoError(t, err)

	require.NotNil(t, captured.parsed)
	actions, ok := captured.parsed["potentialAction"].([]interface{})
	require.True(t, ok, "should have potentialAction")
	require.Len(t, actions, 1)

	action := actions[0].(map[string]interface{})
	assert.Equal(t, "View PR", action["name"])
}

func TestTeams_HTTP200_Success(t *testing.T) {
	server, _ := newTeamsTestServer(t, http.StatusOK)

	cfg := models.TeamsConfig{Enabled: true, WebhookURL: server.URL}
	notifier := NewTeamsNotifier(cfg)

	err := notifier.Notify(context.Background(), completedJob(), EventCompleted)
	assert.NoError(t, err)
}

// --- Failure Scenarios ---

func TestTeams_WebhookUnreachable_ReturnsError(t *testing.T) {
	cfg := models.TeamsConfig{Enabled: true, WebhookURL: "http://127.0.0.1:1/webhook"}
	notifier := NewTeamsNotifier(cfg)

	err := notifier.Notify(context.Background(), completedJob(), EventCompleted)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "teams: request failed")
}

func TestTeams_Non2xxStatus_ReturnsError(t *testing.T) {
	statusCodes := []int{400, 403, 429, 500}

	for _, code := range statusCodes {
		t.Run(http.StatusText(code), func(t *testing.T) {
			server, _ := newTeamsTestServer(t, code)

			cfg := models.TeamsConfig{Enabled: true, WebhookURL: server.URL}
			notifier := NewTeamsNotifier(cfg)

			err := notifier.Notify(context.Background(), completedJob(), EventCompleted)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "teams: webhook returned HTTP")
		})
	}
}

func TestTeams_EmptyWebhookURL_ReturnsError(t *testing.T) {
	cfg := models.TeamsConfig{Enabled: true, WebhookURL: ""}
	notifier := NewTeamsNotifier(cfg)

	err := notifier.Notify(context.Background(), completedJob(), EventCompleted)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no webhook URL")
}

// --- Edge Cases ---

func TestTeams_NoOwnerName_OmitsOwner(t *testing.T) {
	server, captured := newTeamsTestServer(t, http.StatusOK)

	cfg := models.TeamsConfig{Enabled: true, WebhookURL: server.URL}
	notifier := NewTeamsNotifier(cfg)

	job := completedJob()
	job.OwnerName = ""
	err := notifier.Notify(context.Background(), job, EventCompleted)
	require.NoError(t, err)

	bodyStr := string(captured.body)
	assert.NotContains(t, bodyStr, "\"title\":\"Owner\"")
}

func TestTeams_NoPRURL_OmitsActionButton(t *testing.T) {
	server, captured := newTeamsTestServer(t, http.StatusOK)

	cfg := models.TeamsConfig{Enabled: true, WebhookURL: server.URL}
	notifier := NewTeamsNotifier(cfg)

	job := failedJob()
	job.PRURL = ""
	err := notifier.Notify(context.Background(), job, EventFailed)
	require.NoError(t, err)

	require.NotNil(t, captured.parsed)
	_, hasAction := captured.parsed["potentialAction"]
	assert.False(t, hasAction, "should not have action button without PR URL")
}

func TestTeams_LongErrorMessage_Truncated(t *testing.T) {
	server, captured := newTeamsTestServer(t, http.StatusOK)

	cfg := models.TeamsConfig{Enabled: true, WebhookURL: server.URL}
	notifier := NewTeamsNotifier(cfg)

	job := failedJob()
	job.Error = strings.Repeat("x", 1000)
	err := notifier.Notify(context.Background(), job, EventFailed)
	require.NoError(t, err)

	bodyStr := string(captured.body)
	// Should be truncated to 500 chars + "..."
	assert.Contains(t, bodyStr, strings.Repeat("x", 500)+"...")
	assert.NotContains(t, bodyStr, strings.Repeat("x", 501))
}

func TestTeams_WebhookURLWithTrailingSlash(t *testing.T) {
	server, _ := newTeamsTestServer(t, http.StatusOK)

	cfg := models.TeamsConfig{Enabled: true, WebhookURL: server.URL + "/"}
	notifier := NewTeamsNotifier(cfg)

	err := notifier.Notify(context.Background(), completedJob(), EventCompleted)
	assert.NoError(t, err)
}

func TestTeams_Name(t *testing.T) {
	notifier := NewTeamsNotifier(models.TeamsConfig{})
	assert.Equal(t, "teams", notifier.Name())
}

func TestTeams_ErrorResponseBody_IncludedInError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Invalid webhook payload"))
	}))
	t.Cleanup(server.Close)

	cfg := models.TeamsConfig{Enabled: true, WebhookURL: server.URL}
	notifier := NewTeamsNotifier(cfg)

	err := notifier.Notify(context.Background(), completedJob(), EventCompleted)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Invalid webhook payload")
}

func TestTeams_CardStructure(t *testing.T) {
	server, captured := newTeamsTestServer(t, http.StatusOK)

	cfg := models.TeamsConfig{Enabled: true, WebhookURL: server.URL}
	notifier := NewTeamsNotifier(cfg)

	err := notifier.Notify(context.Background(), completedJob(), EventCompleted)
	require.NoError(t, err)

	require.NotNil(t, captured.parsed)
	assert.Equal(t, "MessageCard", captured.parsed["@type"])
	assert.Equal(t, "http://schema.org/extensions", captured.parsed["@context"])

	sections, ok := captured.parsed["sections"].([]interface{})
	require.True(t, ok)
	require.Len(t, sections, 1)

	section := sections[0].(map[string]interface{})
	assert.Contains(t, section["activityTitle"], "Job #42")
}

func TestTeams_SendMessage_Success(t *testing.T) {
	var received map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "POST", r.Method)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		err := json.NewDecoder(r.Body).Decode(&received)
		require.NoError(t, err)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := NewTeamsNotifier(models.TeamsConfig{
		Enabled:    true,
		WebhookURL: server.URL,
	})

	err := notifier.SendMessage(context.Background(), "Pipeline started for user-auth")
	require.NoError(t, err)

	assert.Equal(t, "MessageCard", received["@type"])
	assert.Equal(t, "Ralph-o-matic", received["summary"])
	sections := received["sections"].([]interface{})
	require.Len(t, sections, 1)
	section := sections[0].(map[string]interface{})
	assert.Equal(t, "Pipeline started for user-auth", section["text"])
}

func TestTeams_SendMessage_EmptyWebhookURL_ReturnsError(t *testing.T) {
	notifier := NewTeamsNotifier(models.TeamsConfig{
		Enabled:    true,
		WebhookURL: "",
	})

	err := notifier.SendMessage(context.Background(), "test message")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no webhook URL")
}

func TestTeams_SendMessage_Non2xx_ReturnsError(t *testing.T) {
	server, _ := newTeamsTestServer(t, http.StatusInternalServerError)

	notifier := NewTeamsNotifier(models.TeamsConfig{
		Enabled:    true,
		WebhookURL: server.URL,
	})

	err := notifier.SendMessage(context.Background(), "test message")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "teams: webhook returned HTTP")
}

func TestTeams_SendMessage_Unreachable_ReturnsError(t *testing.T) {
	notifier := NewTeamsNotifier(models.TeamsConfig{
		Enabled:    true,
		WebhookURL: "http://127.0.0.1:1/webhook",
	})

	err := notifier.SendMessage(context.Background(), "test message")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "teams: request failed")
}

func TestTeams_DurationIncluded(t *testing.T) {
	server, captured := newTeamsTestServer(t, http.StatusOK)

	cfg := models.TeamsConfig{Enabled: true, WebhookURL: server.URL}
	notifier := NewTeamsNotifier(cfg)

	job := completedJob()
	// Set known duration
	now := time.Now()
	started := now.Add(-10 * time.Minute)
	job.StartedAt = &started
	job.CompletedAt = &now

	err := notifier.Notify(context.Background(), job, EventCompleted)
	require.NoError(t, err)

	bodyStr := string(captured.body)
	assert.Contains(t, bodyStr, "Duration")
	assert.Contains(t, bodyStr, "10m0s")
}
