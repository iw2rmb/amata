package expr

import (
	"context"
	"fmt"

	"auto/internal/executor"
	"auto/internal/state"
)

type Executor struct{}

func New() executor.Executor {
	return &Executor{}
}

func (e *Executor) Execute(_ context.Context, ctx executor.StepContext) state.StepResult {
	value, ok := ctx.Step.Fields["expr"]
	if !ok {
		return executor.Failed("invalid_expr", fmt.Sprintf("step %d is missing expr", ctx.StepIndex))
	}

	return executor.Succeeded(value)
}
