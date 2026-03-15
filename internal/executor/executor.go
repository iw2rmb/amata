package executor

import (
	"context"
	"path/filepath"

	exprruntime "auto/internal/expr"
	"auto/internal/spec"
	"auto/internal/state"
	"auto/internal/workspace"
)

type Executor interface {
	Execute(context.Context, StepContext) state.StepResult
}

type Factory func() Executor

type StepContext struct {
	RunID          string
	RunDir         string
	SpecPath       string
	Spec           spec.Document
	Workspace      workspace.Config
	FlowName       string
	StepIndex      int
	Step           spec.Step
	Previous       *state.StepResult
	Runtime        exprruntime.Runtime
	ExecutionLabel string
}

func ResolveCWD(stepCtx StepContext) (string, error) {
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

func CommandWithBinary(binary string, args []string) []string {
	command := make([]string, 0, len(args)+1)
	command = append(command, binary)
	command = append(command, args...)
	return command
}
