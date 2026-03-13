package runtime

import (
	"auto/internal/executor"
	assertexec "auto/internal/executor/assert"
	claudeexec "auto/internal/executor/claude"
	codexexec "auto/internal/executor/codex"
	exprexec "auto/internal/executor/expr"
	shellexec "auto/internal/executor/shell"
)

func builtinRegistry() *Registry {
	registry := NewRegistry()
	mustRegister(registry, "shell", shellexec.New)
	mustRegister(registry, "expr", exprexec.New)
	mustRegister(registry, "assert", assertexec.New)
	mustRegister(registry, "codex", codexexec.New)
	mustRegister(registry, "claude", claudeexec.New)
	return registry
}

func mustRegister(registry *Registry, name string, factory executor.Factory) {
	if err := registry.Register(name, factory); err != nil {
		panic(err)
	}
}
