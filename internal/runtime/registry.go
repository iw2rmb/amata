package runtime

import "fmt"

type ExecutorFactory func() any

type Registry struct {
	factories map[string]ExecutorFactory
}

func NewRegistry() *Registry {
	return &Registry{
		factories: make(map[string]ExecutorFactory),
	}
}

func (r *Registry) Register(name string, factory ExecutorFactory) error {
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

func (r *Registry) Lookup(name string) (ExecutorFactory, bool) {
	factory, ok := r.factories[name]
	return factory, ok
}
