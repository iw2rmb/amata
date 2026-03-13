package executor

import (
	"context"

	"auto/internal/spec"
	"auto/internal/state"
	"auto/internal/workspace"
)

type Executor interface {
	Execute(context.Context, StepContext) state.StepResult
}

type Factory func() Executor

type StepContext struct {
	RunID     string
	RunDir    string
	SpecPath  string
	Workspace workspace.Config
	FlowName  string
	StepIndex int
	Step      spec.Step
	Previous  *state.StepResult
}
