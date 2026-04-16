package runtime

import (
	"github.com/iw2rmb/amata/internal/executor"
	assertexec "github.com/iw2rmb/amata/internal/executor/assert"
	claudeexec "github.com/iw2rmb/amata/internal/executor/claude"
	codexexec "github.com/iw2rmb/amata/internal/executor/codex"
	crushexec "github.com/iw2rmb/amata/internal/executor/crush"
	datagetexec "github.com/iw2rmb/amata/internal/executor/dataget"
	exprexec "github.com/iw2rmb/amata/internal/executor/expr"
	gitcommitexec "github.com/iw2rmb/amata/internal/executor/gitcommit"
	gitinspectexec "github.com/iw2rmb/amata/internal/executor/gitinspect"
	pollingshortexec "github.com/iw2rmb/amata/internal/executor/pollingshort"
	shellexec "github.com/iw2rmb/amata/internal/executor/shell"
)

func builtinRegistry() *Registry {
	registry := NewRegistry()
	mustRegister(registry, "shell", shellexec.New)
	mustRegister(registry, "expr", exprexec.New)
	mustRegister(registry, "assert", assertexec.New)
	mustRegister(registry, "data.get", datagetexec.New)
	mustRegister(registry, "codex", codexexec.New)
	mustRegister(registry, "claude", claudeexec.New)
	mustRegister(registry, "crush", crushexec.New)
	mustRegister(registry, "git.inspect", gitinspectexec.New)
	mustRegister(registry, "git.commit", gitcommitexec.New)
	mustRegister(registry, "polling.short", pollingshortexec.New)
	return registry
}

func mustRegister(registry *Registry, name string, factory executor.Factory) {
	if err := registry.Register(name, factory); err != nil {
		panic(err)
	}
}
