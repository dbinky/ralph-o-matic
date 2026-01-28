package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/ryan/ralph-o-matic/internal/models"
)

// Client communicates with the ralph-o-matic server
type Client struct {
	baseURL    string
	httpClient *http.Client
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
	Env           map[string]string `json:"env,omitempty"`
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

// ReorderJobs reorders the queue
func (c *Client) ReorderJobs(jobIDs []int64) error {
	req := map[string][]int64{"job_ids": jobIDs}
	return c.put("/api/jobs/order", req, nil)
}

// GetConfig retrieves server config
func (c *Client) GetConfig() (*models.ServerConfig, error) {
	var cfg models.ServerConfig
	if err := c.get("/api/config", &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// UpdateConfig updates server config
func (c *Client) UpdateConfig(updates map[string]interface{}) (*models.ServerConfig, error) {
	var cfg models.ServerConfig
	if err := c.patch("/api/config", updates, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
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
