package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// OllamaModel represents a model available on the Ollama server
type OllamaModel struct {
	Name   string  // model name/tag
	SizeGB float64 // size on disk in GB
}

// OllamaClient communicates with the Ollama REST API
type OllamaClient struct {
	host       string
	httpClient *http.Client
}

// NewOllamaClient creates a new client for the given Ollama host
func NewOllamaClient(host string) *OllamaClient {
	// Normalize URL
	host = strings.TrimSuffix(host, "/")
	if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
		host = "http://" + host
	}

	return &OllamaClient{
		host:       host,
		httpClient: &http.Client{},
	}
}

// Ping checks if Ollama is reachable
func (c *OllamaClient) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.host+"/api/tags", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to Ollama at %s: %w", c.host, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Ollama returned status %d", resp.StatusCode)
	}

	return nil
}

// ListModels returns all models available on the Ollama server
func (c *OllamaClient) ListModels(ctx context.Context) ([]OllamaModel, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.host+"/api/tags", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to list models from %s: %w", c.host, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to list models: Ollama returned status %d", resp.StatusCode)
	}

	var result struct {
		Models []struct {
			Name string `json:"name"`
			Size int64  `json:"size"`
		} `json:"models"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to parse model list: %w", err)
	}

	var models []OllamaModel
	for _, m := range result.Models {
		models = append(models, OllamaModel{
			Name:   m.Name,
			SizeGB: float64(m.Size) / (1024 * 1024 * 1024),
		})
	}

	return models, nil
}

// HasModel checks if a specific model is available on the server
func (c *OllamaClient) HasModel(ctx context.Context, name string) (bool, error) {
	body := fmt.Sprintf(`{"name":%q}`, name)
	req, err := http.NewRequestWithContext(ctx, "POST", c.host+"/api/show", strings.NewReader(body))
	if err != nil {
		return false, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("failed to check model %s: %w", name, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body) //nolint:errcheck

	if resp.StatusCode == http.StatusOK {
		return true, nil
	}
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	return false, fmt.Errorf("failed to check model %s: HTTP %d", name, resp.StatusCode)
}

// PullModel downloads a model from the Ollama registry
func (c *OllamaClient) PullModel(ctx context.Context, name string) error {
	body := fmt.Sprintf(`{"name":%q}`, name)
	req, err := http.NewRequestWithContext(ctx, "POST", c.host+"/api/pull", strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create pull request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to pull model %s: %w", name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&errResp)
		if errResp.Error != "" {
			return fmt.Errorf("failed to pull model %s: %s", name, errResp.Error)
		}
		return fmt.Errorf("failed to pull model %s: HTTP %d", name, resp.StatusCode)
	}

	// Consume the full streaming NDJSON response so Ollama completes the pull.
	// Each line is a JSON status object; we read until EOF.
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		return fmt.Errorf("failed to pull model %s: error reading response: %w", name, err)
	}

	return nil
}
