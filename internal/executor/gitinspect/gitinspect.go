package gitinspect

import (
	"context"
	"fmt"

	"github.com/iw2rmb/amata/internal/executor"
	"github.com/iw2rmb/amata/internal/gitadapter"
	"github.com/iw2rmb/amata/internal/state"
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

	cwd, err := executor.ResolveCWD(stepCtx)
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
