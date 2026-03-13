package runtime

import (
	"fmt"

	"auto/internal/executor"
)

type Registry struct {
	factories map[string]executor.Factory
}

func NewRegistry() *Registry {
	return &Registry{
		factories: make(map[string]executor.Factory),
	}
}

func (r *Registry) Register(name string, factory executor.Factory) error {
	if name == "" {
		return fmt.Errorf("executor name is required")
	}
	if factory == nil {
		return fmt.Errorf("executor factory is required")
	}
	if _, exists := r.factories[name]; exists {
		return fmt.Errorf("executor %q is already registered", name)
	}

	r.factories[name] = factory
	return nil
}

func (r *Registry) Lookup(name string) (executor.Factory, bool) {
	factory, ok := r.factories[name]
	return factory, ok
}
