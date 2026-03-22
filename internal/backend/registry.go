package backend

import (
	"fmt"
	"strings"
)

// Registry holds all registered backends
type Registry struct {
	backends []Backend
}

// NewRegistry creates a new backend registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// Register adds a new backend to the registry
func (r *Registry) Register(b Backend) {
	r.backends = append(r.backends, b)
}

// AllModels returns all supported models across all active backends
func (r *Registry) AllModels() []string {
	var models []string
	for _, b := range r.backends {
		models = append(models, b.Models()...)
	}
	return models
}

// Resolve finds a backend that supports the given model name.
// If not explicitly matched, it can fallback to a heuristic (e.g., prefix match).
func (r *Registry) Resolve(modelName string) (Backend, error) {
	if len(r.backends) == 0 {
		return nil, fmt.Errorf("no backend registered")
	}

	for _, b := range r.backends {
		for _, m := range b.Models() {
			if m == modelName {
				return b, nil
			}
		}
	}

	// Fallback heuristic: match prefix or suffix
	for _, b := range r.backends {
		if strings.Contains(strings.ToLower(modelName), strings.ToLower(b.Name())) {
			return b, nil
		}
	}

	return nil, fmt.Errorf("no capability to handle model: %s", modelName)
}
