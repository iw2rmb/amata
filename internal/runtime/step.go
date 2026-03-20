package runtime

import (
	"fmt"

	exprruntime "github.com/iw2rmb/amata/internal/expr"
	"github.com/iw2rmb/amata/internal/spec"
	"github.com/iw2rmb/amata/internal/state"
)

type stepAction struct {
	pushFrame *state.FlowFrame
}

func stepRef(step *state.StepResult) *state.StepRef {
	if step == nil {
		return nil
	}
	return state.StepRefFor(*step)
}

func shouldSkipStep(stepIndex int, step spec.Step, runtime exprruntime.Runtime) (bool, *state.Failure) {
	value, ok := step.Fields["when"]
	if !ok {
		return false, nil
	}

	resolved, err := runtime.Resolve(value)
	if err != nil {
		return false, &state.Failure{
			Code:    "invalid_when",
			Message: fmt.Sprintf("step %d when is invalid: %v", stepIndex, err),
		}
	}

	enabled, ok := resolved.(bool)
	if !ok {
		return false, &state.Failure{
			Code:    "invalid_when",
			Message: fmt.Sprintf("step %d when must be a boolean", stepIndex),
		}
	}

	return !enabled, nil
}

func expectStep(stepIndex int, step spec.Step, runtime exprruntime.Runtime, result state.StepResult) *state.Failure {
	value, ok := step.Fields["expect"]
	if !ok {
		return nil
	}

	boundRuntime := runtime.WithBindings(stepResultContext(result, nil))
	resolved, err := boundRuntime.Resolve(value)
	if err != nil {
		return &state.Failure{
			Code:    "invalid_expect",
			Message: fmt.Sprintf("step %d expect is invalid: %v", stepIndex, err),
		}
	}

	passed, ok := resolved.(bool)
	if !ok {
		return &state.Failure{
			Code:    "invalid_expect",
			Message: fmt.Sprintf("step %d expect must be a boolean", stepIndex),
		}
	}
	if passed {
		return nil
	}

	return &state.Failure{
		Code:    "expectation_failed",
		Message: fmt.Sprintf("step %d expectation failed", stepIndex),
	}
}

func durableFailedStep(snapshot state.Snapshot) *state.StepResult {
	if len(snapshot.Steps) == 0 {
		return nil
	}

	last := snapshot.Steps[len(snapshot.Steps)-1]
	if last.Status != state.StepStatusFailed {
		return nil
	}
	return &last
}

func failureForSnapshot(runID string, snapshot state.Snapshot) *state.Failure {
	if snapshot.Failure != nil {
		failure := *snapshot.Failure
		return &failure
	}
	if failed := durableFailedStep(snapshot); failed != nil {
		return failureForStep(*failed)
	}
	return &state.Failure{
		Code:    "run_failed",
		Message: fmt.Sprintf("run %q failed", runID),
	}
}

func failureForStep(step state.StepResult) *state.Failure {
	if step.Error != nil {
		failure := *step.Error
		return &failure
	}
	return &state.Failure{
		Code:    "step_failed",
		Message: fmt.Sprintf("step %d failed", step.Index),
	}
}
