package platform

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadEmbeddedCatalog(t *testing.T) {
	catalog, err := LoadEmbeddedCatalog()
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(catalog.Models), 5)

	// Verify first model
	assert.Equal(t, "devstral", catalog.Models[0].Name)
	assert.Equal(t, 15.0, catalog.Models[0].MemoryGB)
	assert.Equal(t, "large", catalog.Models[0].Role)
	assert.Equal(t, 9, catalog.Models[0].Quality)
}

func TestLoadCatalogFromBytes(t *testing.T) {
	yaml := []byte(`
models:
  - name: "test-model:7b"
    memory_gb: 5
    role: both
    quality: 4
    description: "Test model"
`)
	catalog, err := LoadCatalogFromBytes(yaml)
	require.NoError(t, err)
	assert.Len(t, catalog.Models, 1)
	assert.Equal(t, "test-model:7b", catalog.Models[0].Name)
}

func TestLoadCatalogFromBytes_Invalid(t *testing.T) {
	t.Run("malformed yaml", func(t *testing.T) {
		_, err := LoadCatalogFromBytes([]byte("not: [valid: yaml"))
		assert.Error(t, err)
	})

	t.Run("empty models list", func(t *testing.T) {
		_, err := LoadCatalogFromBytes([]byte("models: []"))
		assert.Error(t, err)
	})

	t.Run("missing name", func(t *testing.T) {
		yaml := []byte(`
models:
  - memory_gb: 5
    role: both
    quality: 4
`)
		_, err := LoadCatalogFromBytes(yaml)
		assert.Error(t, err)
	})

	t.Run("missing memory_gb", func(t *testing.T) {
		yaml := []byte(`
models:
  - name: "test:7b"
    role: both
    quality: 4
`)
		_, err := LoadCatalogFromBytes(yaml)
		assert.Error(t, err)
	})

	t.Run("missing role", func(t *testing.T) {
		yaml := []byte(`
models:
  - name: "test:7b"
    memory_gb: 5
    quality: 4
`)
		_, err := LoadCatalogFromBytes(yaml)
		assert.Error(t, err)
	})

	t.Run("duplicate model names", func(t *testing.T) {
		yaml := []byte(`
models:
  - name: "test:7b"
    memory_gb: 5
    role: both
    quality: 4
  - name: "test:7b"
    memory_gb: 5
    role: both
    quality: 4
`)
		_, err := LoadCatalogFromBytes(yaml)
		assert.Error(t, err)
	})
}

func TestCatalog_LargeModels(t *testing.T) {
	catalog := &Catalog{
		Models: []CatalogModel{
			{Name: "a", Role: "large", Quality: 10, MemoryGB: 42},
			{Name: "b", Role: "small", Quality: 2, MemoryGB: 1.5},
			{Name: "c", Role: "both", Quality: 4, MemoryGB: 5},
		},
	}

	large := catalog.LargeModels()
	assert.Len(t, large, 2) // "a" (large) and "c" (both)
	assert.Equal(t, "a", large[0].Name)
	assert.Equal(t, "c", large[1].Name)
}

func TestCatalog_SmallModels(t *testing.T) {
	catalog := &Catalog{
		Models: []CatalogModel{
			{Name: "a", Role: "large", Quality: 10, MemoryGB: 42},
			{Name: "b", Role: "small", Quality: 2, MemoryGB: 1.5},
			{Name: "c", Role: "both", Quality: 4, MemoryGB: 5},
		},
	}

	small := catalog.SmallModels()
	assert.Len(t, small, 2) // "b" (small) and "c" (both)
	assert.Equal(t, "b", small[0].Name)
	assert.Equal(t, "c", small[1].Name)
}
