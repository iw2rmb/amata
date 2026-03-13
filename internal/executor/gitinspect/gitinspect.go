package gitinspect

import (
	"context"
	"fmt"
	"path/filepath"

	"auto/internal/executor"
	"auto/internal/gitadapter"
	"auto/internal/state"
)

type inspectService interface {
	Inspect(context.Context, string) (gitadapter.Snapshot, error)
}

type Executor struct {
	service inspectService
}

func New() executor.Executor {
	return NewWithService(gitadapter.New())
}

func NewWithService(service inspectService) executor.Executor {
	return &Executor{service: service}
}

func (e *Executor) Execute(ctx context.Context, stepCtx executor.StepContext) state.StepResult {
	if e.service == nil {
		return executor.Failed("invalid_executor", fmt.Sprintf("step %d: git inspect service is required", stepCtx.StepIndex))
	}

	cwd, err := resolveCWD(stepCtx)
	if err != nil {
		return executor.Failed("invalid_cwd", fmt.Sprintf("step %d: %v", stepCtx.StepIndex, err))
	}

	snapshot, err := e.service.Inspect(ctx, cwd)
	if err != nil {
		return executor.Failed("git_inspect_failed", fmt.Sprintf("step %d: %v", stepCtx.StepIndex, err))
	}

	return executor.Succeeded(map[string]any{
		"isRepo":  snapshot.IsRepo,
		"hasDiff": snapshot.HasDiff,
		"files":   snapshot.Files,
	})
}

func resolveCWD(stepCtx executor.StepContext) (string, error) {
	value, ok := stepCtx.Step.Fields["cwd"]
	if !ok {
		return stepCtx.Workspace.Root, nil
	}

	text, err := stepCtx.Runtime.ResolveString(value)
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(text) {
		return filepath.Clean(text), nil
	}

	return filepath.Clean(filepath.Join(stepCtx.Workspace.Root, text)), nil
}
