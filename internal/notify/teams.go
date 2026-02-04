package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ryan/ralph-o-matic/internal/models"
)

const teamsMaxErrorLength = 500

// TeamsNotifier sends notifications to Microsoft Teams via Incoming Webhook.
type TeamsNotifier struct {
	config     models.TeamsConfig
	httpClient *http.Client
}

// NewTeamsNotifier creates a Teams notifier with the given config.
func NewTeamsNotifier(cfg models.TeamsConfig) *TeamsNotifier {
	return &TeamsNotifier{
		config: cfg,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Name returns the notifier name.
func (t *TeamsNotifier) Name() string { return "teams" }

// Notify sends a Teams webhook notification about a job event.
func (t *TeamsNotifier) Notify(ctx context.Context, job *models.Job, event Event) error {
	if t.config.WebhookURL == "" {
		return fmt.Errorf("teams: no webhook URL configured")
	}

	card := t.buildCard(job, event)

	payload, err := json.Marshal(card)
	if err != nil {
		return fmt.Errorf("teams: failed to marshal card: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.config.WebhookURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("teams: failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("teams: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("teams: webhook returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func (t *TeamsNotifier) buildCard(job *models.Job, event Event) map[string]interface{} {
	color := t.eventColor(event)
	title := fmt.Sprintf("Job #%d %s", job.ID, strings.Title(string(event)))

	// Build facts
	facts := []map[string]string{
		{"title": "Repository", "value": job.RepoURL},
		{"title": "Branch", "value": job.Branch},
	}

	if job.OwnerName != "" {
		facts = append(facts, map[string]string{"title": "Owner", "value": job.OwnerName})
	}

	facts = append(facts,
		map[string]string{"title": "Iterations", "value": fmt.Sprintf("%d/%d", job.Iteration, job.MaxIterations)},
		map[string]string{"title": "Duration", "value": job.Duration().Truncate(time.Second).String()},
	)

	if job.Error != "" {
		errMsg := job.Error
		if len(errMsg) > teamsMaxErrorLength {
			errMsg = errMsg[:teamsMaxErrorLength] + "..."
		}
		facts = append(facts, map[string]string{"title": "Error", "value": errMsg})
	}

	section := map[string]interface{}{
		"activityTitle": title,
		"facts":         facts,
		"markdown":      true,
	}

	card := map[string]interface{}{
		"@type":      "MessageCard",
		"@context":   "http://schema.org/extensions",
		"themeColor": color,
		"summary":    title,
		"sections":   []interface{}{section},
	}

	// Add PR action button for completed jobs with a PR URL
	if job.PRURL != "" {
		card["potentialAction"] = []map[string]interface{}{
			{
				"@type":   "OpenUri",
				"name":    "View PR",
				"targets": []map[string]string{{"os": "default", "uri": job.PRURL}},
			},
		}
	}

	return card
}

func (t *TeamsNotifier) eventColor(event Event) string {
	switch event {
	case EventCompleted:
		return "00FF00" // green
	case EventFailed:
		return "FF0000" // red
	case EventCancelled:
		return "FFFF00" // yellow
	default:
		return "808080" // gray
	}
}
