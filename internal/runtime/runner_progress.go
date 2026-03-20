package runtime

import (
	executorapi "github.com/iw2rmb/amata/internal/executor"
	"github.com/iw2rmb/amata/internal/progress"
	"github.com/iw2rmb/amata/internal/spec"
	"github.com/iw2rmb/amata/internal/state"
)

func (r *Runner) recordStepStartedEvent(
	store *state.Store,
	reporter *progress.Reporter,
	config Config,
	flowName string,
	frameID string,
	stepIndex int,
	step spec.Step,
	previous *state.StepResult,
	bindings map[string]any,
	lookup func(*state.StepRef) *state.StepResult,
) (state.Snapshot, error) {
	started := state.StepResult{
		Index:    stepIndex,
		ID:       step.ID,
		Type:     step.ExecutorType(),
		Status:   state.StepStatusRunning,
		FrameID:  frameID,
		Previous: stepRef(previous),
	}
	snapshot, err := store.Append(state.RunEvent{
		Kind: state.EventStepStarted,
		Step: &started,
	})
	if err != nil {
		return state.Snapshot{}, err
	}
	reporter.StepStarted(progressStep(config, flowName, stepIndex, step, previous, bindings, lookup))
	return snapshot, nil
}

func (r *Runner) recordResultEvent(
	store *state.Store,
	reporter *progress.Reporter,
	config Config,
	runID string,
	flowName string,
	frameID string,
	step spec.Step,
	previous *state.StepResult,
	bindings map[string]any,
	kind state.EventKind,
	result state.StepResult,
	lookup func(*state.StepRef) *state.StepResult,
) (state.Snapshot, error) {
	recorded := result
	recorded.FrameID = frameID
	recorded.Previous = stepRef(previous)
	snapshot, err := store.Append(state.RunEvent{
		Kind: kind,
		Step: &recorded,
	})
	if err != nil {
		return state.Snapshot{}, err
	}
	reporter.StepFinished(progressResultStep(config, flowName, step, previous, bindings, result, lookup))

	if result.Status != state.StepStatusFailed {
		return snapshot, nil
	}

	failure := failureForStep(result)
	snapshot, err = store.Append(state.RunEvent{
		Kind:    state.EventRunFinished,
		Status:  state.RunStatusFailed,
		Failure: failure,
	})
	if err != nil {
		return state.Snapshot{}, err
	}
	reporter.RunFinished(progress.RunStatusFailed, state.CloneFailure(failure))

	return snapshot, RunFailedError{
		RunID:   runID,
		Failure: *failure,
	}
}

func progressStep(config Config, flowName string, stepIndex int, step spec.Step, previous *state.StepResult, bindings map[string]any, lookup func(*state.StepRef) *state.StepResult) progress.Step {
	stepCtx := progressStepContext(config, flowName, stepIndex, step, previous, bindings, lookup)
	progressStep, err := progress.StepFromContext(stepCtx)
	if err == nil {
		return progressStep
	}

	return progress.Step{
		Flow:   flowName,
		Index:  stepIndex,
		ID:     step.ID,
		Type:   step.ExecutorType(),
		Status: progress.StepStatusRunning,
	}
}

func progressResultStep(config Config, flowName string, step spec.Step, previous *state.StepResult, bindings map[string]any, result state.StepResult, lookup func(*state.StepRef) *state.StepResult) progress.Step {
	stepCtx := progressStepContext(config, flowName, result.Index, step, previous, bindings, lookup)
	progressStep, err := progress.StepFromResultWithContext(flowName, stepCtx, result)
	if err == nil {
		return progressStep
	}
	return progress.StepFromResult(flowName, result)
}

func progressStepContext(config Config, flowName string, stepIndex int, step spec.Step, previous *state.StepResult, bindings map[string]any, lookup func(*state.StepRef) *state.StepResult) executorapi.StepContext {
	return executorapi.StepContext{
		RunID:     config.RunID,
		RunDir:    config.RunDir,
		SpecPath:  config.SpecPath,
		Spec:      config.Spec,
		Workspace: config.Workspace,
		FlowName:  flowName,
		StepIndex: stepIndex,
		Step:      step,
		Previous:  previous,
		Runtime:   newStepRuntime(config, previous, lookup, bindings),
	}
}

func resumeActiveProgressSteps(config Config, plan *flowPlan, snapshot state.Snapshot) []progress.Step {
	if len(snapshot.Frames) < 2 {
		return []progress.Step{}
	}

	lookup := snapshot.StepByRef
	steps := make([]progress.Step, 0, len(snapshot.Frames)-1)
	for index := 1; index < len(snapshot.Frames); index++ {
		frame := snapshot.Frames[index]
		if frame.Return == nil {
			continue
		}

		parentFlowName := snapshot.Frames[index-1].Flow
		parentFlow, ok := plan.Lookup(parentFlowName)
		if !ok || frame.Return.StepIndex < 0 || frame.Return.StepIndex >= len(parentFlow.Steps) {
			steps = append(steps, progress.Step{
				Flow:   parentFlowName,
				Index:  frame.Return.StepIndex,
				ID:     frame.Return.StepID,
				Type:   frame.Return.StepType,
				Status: progress.StepStatusRunning,
			})
			continue
		}

		steps = append(steps, progressStep(config, parentFlowName, frame.Return.StepIndex, parentFlow.Steps[frame.Return.StepIndex], lookup(frame.Previous), snapshot.Frames[index-1].Bindings, lookup))
	}

	return steps
}
