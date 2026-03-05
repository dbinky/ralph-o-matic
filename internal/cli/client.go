package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/ryan/ralph-o-matic/internal/models"
)

// Client communicates with the ralph-o-matic server
type Client struct {
	baseURL     string
	httpClient  *http.Client
	tokenPath   string
	cachedToken *CachedToken // loaded once per CLI invocation
	tokenLoaded bool         // true after first load attempt
	apiKey      string       // static API key for AuthModeAPIKey servers
}

// SetTokenPath sets the path to the cached auth token file.
// When set, the client will attempt to load and attach a Bearer token
// to each request if the token is valid and matches the server.
func (c *Client) SetTokenPath(path string) {
	c.tokenPath = path
}

// SetAPIKey sets a static API key to be sent as a Bearer token on every
// request. Use this for servers configured with AuthModeAPIKey.
// The key is typically read from the RALPH_API_KEY environment variable.
func (c *Client) SetAPIKey(key string) {
	c.apiKey = key
}

// loadCachedToken returns the cached token, loading it from disk on first call.
func (c *Client) loadCachedToken() *CachedToken {
	if !c.tokenLoaded {
		c.tokenLoaded = true
		if token, err := loadToken(c.tokenPath); err == nil {
			c.cachedToken = token
		}
	}
	return c.cachedToken
}

// attachAuth sets the Authorization header on the request.
// Static API key takes precedence over cached Entra token.
func (c *Client) attachAuth(req *http.Request) {
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		return
	}
	if c.tokenPath == "" {
		return
	}
	token := c.loadCachedToken()
	if token == nil {
		return
	}
	if token.IsExpired() {
		fmt.Fprintf(os.Stderr, "Warning: auth token expired. Run 'ralph auth login' to re-authenticate.\n")
		return
	}
	if token.Server == c.baseURL {
		req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	}
}

// NewClient creates a new API client
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		httpClient: &http.Client{},
	}
}

// CreateJobRequest is the request for creating a job
type CreateJobRequest struct {
	RepoURL       string            `json:"repo_url"`
	Branch        string            `json:"branch"`
	Prompt        string            `json:"prompt"`
	MaxIterations int               `json:"max_iterations"`
	Priority      string            `json:"priority,omitempty"`
	WorkingDir    string            `json:"working_dir,omitempty"`
	ExitPromise   string            `json:"exit_promise,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
	Backend       string            `json:"backend,omitempty"`
}

// GetJobs retrieves jobs from the server
func (c *Client) GetJobs(statuses []string) ([]*models.Job, int, error) {
	path := "/api/jobs"
	if len(statuses) > 0 {
		path += "?status=" + url.QueryEscape(strings.Join(statuses, ","))
	}

	var resp struct {
		Jobs  []*models.Job `json:"jobs"`
		Total int           `json:"total"`
	}

	if err := c.get(path, &resp); err != nil {
		return nil, 0, err
	}

	return resp.Jobs, resp.Total, nil
}

// GetJob retrieves a single job
func (c *Client) GetJob(id int64) (*models.Job, error) {
	var job models.Job
	if err := c.get(fmt.Sprintf("/api/jobs/%d", id), &job); err != nil {
		return nil, err
	}
	return &job, nil
}

// CreateJob creates a new job
func (c *Client) CreateJob(req *CreateJobRequest) (*models.Job, error) {
	var job models.Job
	if err := c.post("/api/jobs", req, &job); err != nil {
		return nil, err
	}
	return &job, nil
}

// CancelJob cancels a job
func (c *Client) CancelJob(id int64) (*models.Job, error) {
	var job models.Job
	if err := c.delete(fmt.Sprintf("/api/jobs/%d", id), &job); err != nil {
		return nil, err
	}
	return &job, nil
}

// PauseJob pauses a running job
func (c *Client) PauseJob(id int64) (*models.Job, error) {
	var job models.Job
	if err := c.post(fmt.Sprintf("/api/jobs/%d/pause", id), nil, &job); err != nil {
		return nil, err
	}
	return &job, nil
}

// ResumeJob resumes a paused job
func (c *Client) ResumeJob(id int64) (*models.Job, error) {
	var job models.Job
	if err := c.post(fmt.Sprintf("/api/jobs/%d/resume", id), nil, &job); err != nil {
		return nil, err
	}
	return &job, nil
}

// UpdateJob updates properties of an existing job.
func (c *Client) UpdateJob(id int64, updates map[string]interface{}) (*models.Job, error) {
	var job models.Job
	if err := c.patch(fmt.Sprintf("/api/jobs/%d", id), updates, &job); err != nil {
		return nil, err
	}
	return &job, nil
}

// ReorderJobs reorders the queue
func (c *Client) ReorderJobs(jobIDs []int64) error {
	req := map[string][]int64{"job_ids": jobIDs}
	return c.put("/api/jobs/order", req, nil)
}

// GetConfig retrieves server config
func (c *Client) GetConfig() (*models.ServerConfigResponse, error) {
	var cfg models.ServerConfigResponse
	if err := c.get("/api/config", &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// UpdateConfig updates server config
func (c *Client) UpdateConfig(updates map[string]interface{}) (*models.ServerConfigResponse, error) {
	var cfg models.ServerConfigResponse
	if err := c.patch("/api/config", updates, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// TestNotifyResponse is the response from the test-notify endpoint.
type TestNotifyResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Error   string `json:"error,omitempty"`
}

// TestNotify sends a test notification via the specified channel.
func (c *Client) TestNotify(channel string) (*TestNotifyResponse, error) {
	req := map[string]string{"channel": channel}
	var resp TestNotifyResponse
	if err := c.post("/api/config/test-notify", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetLogs retrieves logs for a job
func (c *Client) GetLogs(jobID int64) ([]map[string]interface{}, error) {
	var resp struct {
		Logs []map[string]interface{} `json:"logs"`
	}
	if err := c.get(fmt.Sprintf("/api/jobs/%d/logs", jobID), &resp); err != nil {
		return nil, err
	}
	return resp.Logs, nil
}

// StreamJobEvents connects to the SSE endpoint for a job and sends a
// notification on the returned channel each time an event arrives.
// The channel is closed when the connection ends or the context is cancelled.
func (c *Client) StreamJobEvents(ctx context.Context, jobID int64) (<-chan struct{}, error) {
	sseURL := fmt.Sprintf("%s/api/jobs/%d/events", c.baseURL, jobID)
	req, err := http.NewRequestWithContext(ctx, "GET", sseURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	c.attachAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("SSE connection failed: %w", err)
	}

	if resp.StatusCode >= 400 {
		resp.Body.Close()
		return nil, fmt.Errorf("SSE connection failed (status %d)", resp.StatusCode)
	}

	ch := make(chan struct{})
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1<<20) // cap at 1 MB per line
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data:") {
				select {
				case ch <- struct{}{}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return ch, nil
}

// Ping checks if server is reachable
func (c *Client) Ping() error {
	return c.get("/health", nil)
}

func (c *Client) get(path string, result interface{}) error {
	return c.request("GET", path, nil, result)
}

func (c *Client) post(path string, body, result interface{}) error {
	return c.request("POST", path, body, result)
}

func (c *Client) put(path string, body, result interface{}) error {
	return c.request("PUT", path, body, result)
}

func (c *Client) patch(path string, body, result interface{}) error {
	return c.request("PATCH", path, body, result)
}

func (c *Client) delete(path string, result interface{}) error {
	return c.request("DELETE", path, nil, result)
}

func (c *Client) request(method, path string, body, result interface{}) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	c.attachAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var errResp struct {
			Error string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&errResp)
		return fmt.Errorf("server error: %s", errResp.Error)
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return nil
}
