package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"

	"auto/internal/spec"
	"auto/internal/state"
)

type Runner struct {
	registry *Registry
}

type RunFailedError struct {
	RunID   string
	Failure state.Failure
}

func (e RunFailedError) Error() string {
	return fmt.Sprintf("run %q failed: %s", e.RunID, e.Failure.Message)
}

func NewRunner(registry *Registry) *Runner {
	if registry == nil {
		registry = NewRegistry()
	}

	return &Runner{registry: registry}
}

func (r *Runner) Run(ctx context.Context, config Config) (state.Snapshot, error) {
	return r.execute(ctx, config, false)
}

func (r *Runner) Resume(ctx context.Context, config Config) (state.Snapshot, error) {
	return r.execute(ctx, config, true)
}

func (r *Runner) execute(ctx context.Context, config Config, resume bool) (state.Snapshot, error) {
	flow, ok := config.Spec.Flows[config.Spec.Entry]
	if !ok {
		return state.Snapshot{}, fmt.Errorf("entry flow %q is not defined", config.Spec.Entry)
	}

	store := state.NewStore(config.RunDir)
	snapshot, err := store.LoadSnapshot()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return state.Snapshot{}, err
	}

	if resume {
		if errors.Is(err, os.ErrNotExist) {
			return state.Snapshot{}, fmt.Errorf("run %q has no stored state", config.RunID)
		}
		if len(snapshot.Frames) == 0 {
			return state.Snapshot{}, fmt.Errorf("run %q has no flow frame state", config.RunID)
		}
		if snapshot.Status != state.RunStatusSucceeded && snapshot.Frames[0].NextStep < snapshot.Frames[0].StepCount {
			snapshot, err = store.Append(state.RunEvent{
				Kind:    state.EventRunResumed,
				Command: "resume",
			})
			if err != nil {
				return state.Snapshot{}, err
			}
		}
	} else {
		if !errors.Is(err, os.ErrNotExist) {
			return state.Snapshot{}, fmt.Errorf("run %q already has stored state", config.RunID)
		}
		snapshot, err = store.Append(state.RunEvent{
			Kind: state.EventRunInitialized,
			Frame: &state.FlowFrame{
				Flow:      config.Spec.Entry,
				StepCount: len(flow.Steps),
			},
			Command: "run",
		})
		if err != nil {
			return state.Snapshot{}, err
		}
	}

	nextStep := 0
	if len(snapshot.Frames) > 0 {
		nextStep = snapshot.Frames[0].NextStep
	}
	if nextStep >= len(flow.Steps) {
		switch snapshot.Status {
		case state.RunStatusSucceeded:
			return snapshot, nil
		case state.RunStatusFailed:
			failure := snapshot.Failure
			if failure == nil {
				failure = &state.Failure{
					Code:    "run_failed",
					Message: fmt.Sprintf("run %q failed", config.RunID),
				}
			}
			return snapshot, RunFailedError{
				RunID:   config.RunID,
				Failure: *failure,
			}
		default:
			return store.Append(state.RunEvent{
				Kind:   state.EventRunFinished,
				Status: state.RunStatusSucceeded,
			})
		}
	}

	var previous *state.StepResult
	if len(snapshot.Steps) > 0 {
		last := snapshot.Steps[len(snapshot.Steps)-1]
		previous = &last
	}

	for index := nextStep; index < len(flow.Steps); index++ {
		step := flow.Steps[index]
		result := r.executeStep(ctx, config, index, step, previous)

		snapshot, err = store.Append(state.RunEvent{
			Kind: state.EventStepRecorded,
			Step: &result,
		})
		if err != nil {
			return state.Snapshot{}, err
		}

		if result.Status == state.StepStatusFailed {
			failure := result.Error
			if failure == nil {
				failure = &state.Failure{
					Code:    "step_failed",
					Message: fmt.Sprintf("step %d failed", index),
				}
			}

			snapshot, err = store.Append(state.RunEvent{
				Kind:    state.EventRunFinished,
				Status:  state.RunStatusFailed,
				Failure: failure,
			})
			if err != nil {
				return state.Snapshot{}, err
			}

			return snapshot, RunFailedError{
				RunID:   config.RunID,
				Failure: *failure,
			}
		}

		previous = &result
	}

	return store.Append(state.RunEvent{
		Kind:   state.EventRunFinished,
		Status: state.RunStatusSucceeded,
	})
}

func (r *Runner) executeStep(
	ctx context.Context,
	config Config,
	stepIndex int,
	step spec.Step,
	previous *state.StepResult,
) state.StepResult {
	result := state.StepResult{
		Index: stepIndex,
		ID:    step.ID,
		Type:  step.ExecutorType(),
	}

	if result.Type == "" {
		result.Status = state.StepStatusFailed
		result.Error = &state.Failure{
			Code:    "missing_executor",
			Message: fmt.Sprintf("step %d does not declare an executor", stepIndex),
		}
		return result
	}

	factory, ok := r.registry.Lookup(result.Type)
	if !ok {
		result.Status = state.StepStatusFailed
		result.Error = &state.Failure{
			Code:    "unknown_executor",
			Message: fmt.Sprintf("executor %q is not registered", result.Type),
		}
		return result
	}

	executor := factory()
	if executor == nil {
		result.Status = state.StepStatusFailed
		result.Error = &state.Failure{
			Code:    "invalid_executor",
			Message: fmt.Sprintf("executor %q returned nil", result.Type),
		}
		return result
	}

	result = executor.Execute(ctx, StepContext{
		Config:    config,
		FlowName:  config.Spec.Entry,
		StepIndex: stepIndex,
		Step:      step,
		Previous:  previous,
	})
	result.Index = stepIndex
	if result.ID == "" {
		result.ID = step.ID
	}
	if result.Type == "" {
		result.Type = step.ExecutorType()
	}

	switch result.Status {
	case state.StepStatusSucceeded, state.StepStatusSkipped:
		return result
	case state.StepStatusFailed:
		if result.Error == nil {
			result.Error = &state.Failure{
				Code:    "step_failed",
				Message: fmt.Sprintf("step %d failed", stepIndex),
			}
		}
		return result
	default:
		result.Status = state.StepStatusFailed
		result.Error = &state.Failure{
			Code:    "invalid_status",
			Message: fmt.Sprintf("step %d returned unsupported status %q", stepIndex, result.Status),
		}
		return result
	}
}
