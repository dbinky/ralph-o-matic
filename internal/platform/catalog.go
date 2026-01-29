package platform

import (
	_ "embed"
	"fmt"

	"gopkg.in/yaml.v3"
)

//go:embed catalog_embed.yaml
var embeddedCatalog []byte

// CatalogModel describes a model available in Ollama
type CatalogModel struct {
	Name        string  `yaml:"name"`
	MemoryGB    float64 `yaml:"memory_gb"`
	Role        string  `yaml:"role"` // "large", "small", or "both"
	Quality     int     `yaml:"quality"`
	Description string  `yaml:"description"`
}

// Catalog holds the list of recommended models
type Catalog struct {
	Models []CatalogModel `yaml:"models"`
}

// LoadEmbeddedCatalog loads the built-in model catalog
func LoadEmbeddedCatalog() (*Catalog, error) {
	return LoadCatalogFromBytes(embeddedCatalog)
}

// LoadCatalogFromBytes parses a YAML byte slice into a Catalog
func LoadCatalogFromBytes(data []byte) (*Catalog, error) {
	var catalog Catalog
	if err := yaml.Unmarshal(data, &catalog); err != nil {
		return nil, fmt.Errorf("failed to parse model catalog: %w", err)
	}

	if err := catalog.validate(); err != nil {
		return nil, err
	}

	return &catalog, nil
}

func (c *Catalog) validate() error {
	if len(c.Models) == 0 {
		return fmt.Errorf("model catalog is empty")
	}

	seen := make(map[string]bool)
	for i, m := range c.Models {
		if m.Name == "" {
			return fmt.Errorf("model at index %d: name is required", i)
		}
		if m.MemoryGB <= 0 {
			return fmt.Errorf("model %q: memory_gb must be positive", m.Name)
		}
		if m.Role == "" {
			return fmt.Errorf("model %q: role is required", m.Name)
		}
		if m.Role != "large" && m.Role != "small" && m.Role != "both" {
			return fmt.Errorf("model %q: role must be large, small, or both", m.Name)
		}
		if seen[m.Name] {
			return fmt.Errorf("duplicate model name: %q", m.Name)
		}
		seen[m.Name] = true
	}

	return nil
}

// LargeModels returns models that can serve as the large (primary) model
func (c *Catalog) LargeModels() []CatalogModel {
	var result []CatalogModel
	for _, m := range c.Models {
		if m.Role == "large" || m.Role == "both" {
			result = append(result, m)
		}
	}
	return result
}

// SmallModels returns models that can serve as the small (helper) model
func (c *Catalog) SmallModels() []CatalogModel {
	var result []CatalogModel
	for _, m := range c.Models {
		if m.Role == "small" || m.Role == "both" {
			result = append(result, m)
		}
	}
	return result
}
