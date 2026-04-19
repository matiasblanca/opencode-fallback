package provider

import "fmt"

// Registry is a map of provider ID to Provider implementation.
// It is the central lookup used by the fallback chain to find providers.
type Registry struct {
	providers map[string]Provider
}

// NewRegistry creates an empty provider registry.
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]Provider),
	}
}

// Register adds a provider to the registry. If a provider with the same ID
// already exists, it is overwritten.
func (r *Registry) Register(p Provider) {
	r.providers[p.ID()] = p
}

// Get returns the provider with the given ID. Returns an error if the
// provider is not registered.
func (r *Registry) Get(id string) (Provider, error) {
	p, ok := r.providers[id]
	if !ok {
		return nil, fmt.Errorf("provider not registered: %s", id)
	}
	return p, nil
}

// List returns all registered provider IDs.
func (r *Registry) List() []string {
	ids := make([]string, 0, len(r.providers))
	for id := range r.providers {
		ids = append(ids, id)
	}
	return ids
}

// Len returns the number of registered providers.
func (r *Registry) Len() int {
	return len(r.providers)
}
