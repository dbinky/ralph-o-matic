package platform

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOllamaClient_Ping(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/tags", r.URL.Path)
		json.NewEncoder(w).Encode(map[string]interface{}{"models": []interface{}{}})
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)
	err := client.Ping(context.Background())
	assert.NoError(t, err)
}

func TestOllamaClient_Ping_Unreachable(t *testing.T) {
	client := NewOllamaClient("http://127.0.0.1:1") // unreachable port
	err := client.Ping(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "127.0.0.1:1")
}

func TestOllamaClient_ListModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/tags", r.URL.Path)
		resp := map[string]interface{}{
			"models": []map[string]interface{}{
				{"name": "qwen3-coder:70b", "size": 42000000000},
				{"name": "qwen2.5-coder:7b", "size": 5000000000},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)
	models, err := client.ListModels(context.Background())
	require.NoError(t, err)
	assert.Len(t, models, 2)
	assert.Equal(t, "qwen3-coder:70b", models[0].Name)
	assert.InDelta(t, 39.1, models[0].SizeGB, 1.0)
}

func TestOllamaClient_ListModels_Empty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"models": []interface{}{}})
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)
	models, err := client.ListModels(context.Background())
	require.NoError(t, err)
	assert.Empty(t, models)
}

func TestOllamaClient_PullModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/pull", r.URL.Path)
		assert.Equal(t, "POST", r.Method)

		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		assert.Equal(t, "test-model:7b", body["name"])

		// Simulate completion
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "success"})
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)
	err := client.PullModel(context.Background(), "test-model:7b")
	assert.NoError(t, err)
}

func TestOllamaClient_PullModel_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"model not found"}`))
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)
	err := client.PullModel(context.Background(), "nonexistent:latest")
	assert.Error(t, err)
}

func TestOllamaClient_HasModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"models": []map[string]interface{}{
				{"name": "qwen3-coder:70b", "size": 42000000000},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)

	has, err := client.HasModel(context.Background(), "qwen3-coder:70b")
	require.NoError(t, err)
	assert.True(t, has)

	has, err = client.HasModel(context.Background(), "missing:latest")
	require.NoError(t, err)
	assert.False(t, has)
}

func TestOllamaClient_NormalizesURL(t *testing.T) {
	t.Run("trailing slash removed", func(t *testing.T) {
		client := NewOllamaClient("http://localhost:11434/")
		assert.Equal(t, "http://localhost:11434", client.host)
	})

	t.Run("scheme auto-prepended", func(t *testing.T) {
		client := NewOllamaClient("localhost:11434")
		assert.Equal(t, "http://localhost:11434", client.host)
	})
}

func TestOllamaClient_MalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)
	_, err := client.ListModels(context.Background())
	assert.Error(t, err)
}

func TestOllamaClient_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Slow response - will be cancelled
		<-r.Context().Done()
	}))
	defer server.Close()

	client := NewOllamaClient(server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := client.Ping(ctx)
	assert.Error(t, err)
}
