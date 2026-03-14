package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"

	executorapi "auto/internal/executor"
	exprruntime "auto/internal/expr"
	"auto/internal/jsonutil"
	"auto/internal/progress"
	"auto/internal/runtime/response"
	"auto/internal/schema"
	"auto/internal/spec"
	"auto/internal/state"
)

type Runner struct {
	registry     *Registry
	progressSink progress.Sink
}

type RunnerOption func(*Runner)

type RunFailedError struct {
	RunID   string
	Failure state.Failure
}

func (e RunFailedError) Error() string {
	return fmt.Sprintf("run %q failed: %s", e.RunID, e.Failure.Message)
}

func WithRunnerProgressSink(sink progress.Sink) RunnerOption {
	return func(runner *Runner) {
		runner.progressSink = sink
	}
}

func NewRunner(registry *Registry, options ...RunnerOption) *Runner {
	if registry == nil {
		registry = builtinRegistry()
	}

	runner := &Runner{registry: registry}
	for _, option := range options {
		if option != nil {
			option(runner)
		}
	}

	return runner
}

func (r *Runner) Run(ctx context.Context, config Config) (state.Snapshot, error) {
	return r.execute(ctx, config, false)
}

func (r *Runner) Resume(ctx context.Context, config Config) (state.Snapshot, error) {
	return r.execute(ctx, config, true)
}

func (r *Runner) execute(ctx context.Context, config Config, resume bool) (state.Snapshot, error) {
	plan, err := buildFlowPlan(config.Spec)
	if err != nil {
		return state.Snapshot{}, err
	}
	entryFlow, ok := plan.Lookup(config.Spec.Entry)
	if !ok {
		return state.Snapshot{}, fmt.Errorf("entry flow %q is not defined", config.Spec.Entry)
	}
	responses := response.NewResolver(schema.NewRegistry(config.Spec.Schemas))
	reporter := progress.NewReporter(config.RunID, r.progressSink)

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

		switch snapshot.Status {
		case state.RunStatusSucceeded:
			return snapshot, nil
		case state.RunStatusFailed:
			failure := failureForSnapshot(config.RunID, snapshot)
			return snapshot, RunFailedError{
				RunID:   config.RunID,
				Failure: *failure,
			}
		}

		if failed := durableFailedStep(snapshot); failed != nil {
			failure := failureForStep(*failed)
			snapshot, err = store.Append(state.RunEvent{
				Kind:    state.EventRunFinished,
				Status:  state.RunStatusFailed,
				Failure: failure,
			})
			if err != nil {
				return state.Snapshot{}, err
			}
			reporter.RunFinished(progress.RunStatusFailed, progressFailure(failure))
			return snapshot, RunFailedError{
				RunID:   config.RunID,
				Failure: *failure,
			}
		}

		if len(snapshot.Frames) > 0 {
			snapshot, err = store.Append(state.RunEvent{
				Kind:    state.EventRunResumed,
				Command: "resume",
			})
			if err != nil {
				return state.Snapshot{}, err
			}
			reporter.RunResumed(resumeActiveProgressSteps(config, plan, snapshot))
		}
	} else {
		if !errors.Is(err, os.ErrNotExist) {
			return state.Snapshot{}, fmt.Errorf("run %q already has stored state", config.RunID)
		}
		snapshot, err = store.Append(state.RunEvent{
			Kind: state.EventRunInitialized,
			Frame: &state.FlowFrame{
				Flow:      config.Spec.Entry,
				StepCount: len(entryFlow.Steps),
			},
			Command: "run",
		})
		if err != nil {
			return state.Snapshot{}, err
		}
		reporter.RunStarted("run")
	}

	for {
		if len(snapshot.Frames) == 0 {
			return state.Snapshot{}, fmt.Errorf("run %q has no flow frame state", config.RunID)
		}

		frame := snapshot.Frames[len(snapshot.Frames)-1]
		flow, ok := plan.Lookup(frame.Flow)
		if !ok {
			return state.Snapshot{}, fmt.Errorf("flow %q is not defined", frame.Flow)
		}

		if frame.NextStep >= frame.StepCount {
			if frame.Return == nil {
				snapshot, err = store.Append(state.RunEvent{
					Kind:   state.EventRunFinished,
					Status: state.RunStatusSucceeded,
				})
				if err != nil {
					return state.Snapshot{}, err
				}
				reporter.RunFinished(progress.RunStatusSucceeded, nil)
				return snapshot, nil
			}

			parentFrame := snapshot.Frames[len(snapshot.Frames)-2]
			parentFlow, ok := plan.Lookup(parentFrame.Flow)
			if !ok {
				return state.Snapshot{}, fmt.Errorf("flow %q is not defined", parentFrame.Flow)
			}
			parentStep := parentFlow.Steps[frame.Return.StepIndex]
			returned := returnedControlResult(frame.Return, frame.Produced)
			finalized := r.finalizeStepResult(config, responses, parentFrame.Previous, parentStep, returned)

			if snapshot, err = r.recordResultEvent(store, reporter, config, config.RunID, parentFrame.Flow, parentStep, parentFrame.Previous, state.EventControlReturned, finalized); err != nil {
				return snapshot, err
			}
			continue
		}

		stepIndex := frame.NextStep
		step := flow.Steps[stepIndex]
		runtime := exprruntime.NewRuntime(buildRuntimeContext(config, frame.Previous))

		switch step.ExecutorType() {
		case "call":
			reporter.StepStarted(progressStep(config, frame.Flow, stepIndex, step, frame.Previous))
			action, result := r.prepareStepAction(config, runtime, frame.Previous, stepIndex, step)
			if action.pushFrame != nil {
				snapshot, err = store.Append(state.RunEvent{
					Kind:  state.EventFramePushed,
					Frame: action.pushFrame,
				})
				if err != nil {
					return state.Snapshot{}, err
				}
				continue
			}
			if snapshot, err = r.recordResultEvent(store, reporter, config, config.RunID, frame.Flow, step, frame.Previous, state.EventStepRecorded, result); err != nil {
				return snapshot, err
			}
		case "switch":
			reporter.StepStarted(progressStep(config, frame.Flow, stepIndex, step, frame.Previous))
			action, result := r.prepareSwitch(config, runtime, plan, responses, frame.Flow, frame.Previous, stepIndex, step)
			if action.pushFrame != nil {
				snapshot, err = store.Append(state.RunEvent{
					Kind:  state.EventFramePushed,
					Frame: action.pushFrame,
				})
				if err != nil {
					return state.Snapshot{}, err
				}
				continue
			}
			if snapshot, err = r.recordResultEvent(store, reporter, config, config.RunID, frame.Flow, step, frame.Previous, state.EventStepRecorded, result); err != nil {
				return snapshot, err
			}
		default:
			reporter.StepStarted(progressStep(config, frame.Flow, stepIndex, step, frame.Previous))
			result := r.executeStep(ctx, config, responses, frame.Flow, stepIndex, step, frame.Previous)
			if snapshot, err = r.recordResultEvent(store, reporter, config, config.RunID, frame.Flow, step, frame.Previous, state.EventStepRecorded, result); err != nil {
				return snapshot, err
			}
		}
	}
}

func (r *Runner) executeStep(
	ctx context.Context,
	config Config,
	responses response.Resolver,
	flowName string,
	stepIndex int,
	step spec.Step,
	previous *state.StepResult,
) state.StepResult {
	runtime := exprruntime.NewRuntime(buildRuntimeContext(config, previous))
	action, result := r.prepareStepAction(config, runtime, previous, stepIndex, step)
	if result.Status != "" || action.pushFrame != nil {
		return finalizeStatus(result)
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

	stepExecutor := factory()
	if stepExecutor == nil {
		result.Status = state.StepStatusFailed
		result.Error = &state.Failure{
			Code:    "invalid_executor",
			Message: fmt.Sprintf("executor %q returned nil", result.Type),
		}
		return result
	}

	result = stepExecutor.Execute(ctx, executorapi.StepContext{
		RunID:     config.RunID,
		RunDir:    config.RunDir,
		SpecPath:  config.SpecPath,
		Spec:      config.Spec,
		Workspace: config.Workspace,
		FlowName:  flowName,
		StepIndex: stepIndex,
		Step:      step,
		Previous:  previous,
		Runtime:   runtime,
	})
	result = executorapi.NormalizeResult(result)
	result.Index = stepIndex
	if result.ID == "" {
		result.ID = step.ID
	}
	if result.Type == "" {
		result.Type = step.ExecutorType()
	}

	return r.finalizeStepResult(config, responses, previous, step, result)
}

type stepAction struct {
	pushFrame *state.FlowFrame
}

func (r *Runner) prepareStepAction(
	config Config,
	runtime exprruntime.Runtime,
	previous *state.StepResult,
	stepIndex int,
	step spec.Step,
) (stepAction, state.StepResult) {
	result := executorapi.NormalizeResult(state.StepResult{
		Index: stepIndex,
		ID:    step.ID,
		Type:  step.ExecutorType(),
	})

	if skip, failure := shouldSkipStep(stepIndex, step, runtime); failure != nil {
		result.Status = state.StepStatusFailed
		result.Error = failure
		return stepAction{}, finalizeStatus(result)
	} else if skip {
		result.Status = state.StepStatusSkipped
		return stepAction{}, finalizeStatus(result)
	}

	if result.Type == "" {
		result.Status = state.StepStatusFailed
		result.Error = &state.Failure{
			Code:    "missing_executor",
			Message: fmt.Sprintf("step %d does not declare an executor", stepIndex),
		}
		return stepAction{}, finalizeStatus(result)
	}

	if result.Type != "call" {
		return stepAction{}, result
	}

	flowValue, ok := step.Fields["flow"]
	if !ok {
		result.Status = state.StepStatusFailed
		result.Error = &state.Failure{
			Code:    "invalid_call",
			Message: fmt.Sprintf("step %d call is missing flow", stepIndex),
		}
		return stepAction{}, finalizeStatus(result)
	}

	targetFlow, err := runtime.ResolveString(flowValue)
	if err != nil {
		result.Status = state.StepStatusFailed
		result.Error = &state.Failure{
			Code:    "invalid_call",
			Message: fmt.Sprintf("step %d call flow is invalid: %v", stepIndex, err),
		}
		return stepAction{}, finalizeStatus(result)
	}

	target, ok := config.Spec.Flows[targetFlow]
	if !ok {
		result.Status = state.StepStatusFailed
		result.Error = &state.Failure{
			Code:    "unknown_flow",
			Message: fmt.Sprintf("step %d references undefined flow %q", stepIndex, targetFlow),
		}
		return stepAction{}, finalizeStatus(result)
	}

	return stepAction{
		pushFrame: &state.FlowFrame{
			Flow:      targetFlow,
			StepCount: len(target.Steps),
			Previous:  previous,
			Return: &state.FrameReturn{
				StepType:  result.Type,
				StepIndex: stepIndex,
				StepID:    step.ID,
				Flow:      targetFlow,
			},
		},
	}, result
}

func (r *Runner) prepareSwitch(
	config Config,
	runtime exprruntime.Runtime,
	plan *flowPlan,
	responses response.Resolver,
	flowName string,
	previous *state.StepResult,
	stepIndex int,
	step spec.Step,
) (stepAction, state.StepResult) {
	action, result := r.prepareStepAction(config, runtime, previous, stepIndex, step)
	if result.Type != "switch" || result.Status != "" || action.pushFrame != nil {
		return action, result
	}

	cases, err := decodeSwitchCases(step)
	if err != nil {
		result.Status = state.StepStatusFailed
		result.Error = &state.Failure{
			Code:    "invalid_switch",
			Message: fmt.Sprintf("step %d switch is invalid: %v", stepIndex, err),
		}
		return stepAction{}, finalizeStatus(result)
	}

	for caseIndex, branch := range cases {
		matched, failure := switchCaseMatches(stepIndex, caseIndex, branch, runtime)
		if failure != nil {
			result.Status = state.StepStatusFailed
			result.Error = failure
			return stepAction{}, finalizeStatus(result)
		}
		if !matched {
			continue
		}

		branchFlow, ok := plan.SwitchBranchFlow(flowName, stepIndex, caseIndex)
		if !ok {
			result.Status = state.StepStatusFailed
			result.Error = &state.Failure{
				Code:    "invalid_switch",
				Message: fmt.Sprintf("step %d switch branch %d is not planned", stepIndex, caseIndex),
			}
			return stepAction{}, finalizeStatus(result)
		}

		planned, ok := plan.Lookup(branchFlow)
		if !ok {
			result.Status = state.StepStatusFailed
			result.Error = &state.Failure{
				Code:    "invalid_switch",
				Message: fmt.Sprintf("step %d switch branch %d flow %q is missing", stepIndex, caseIndex, branchFlow),
			}
			return stepAction{}, finalizeStatus(result)
		}

		idx := caseIndex
		return stepAction{
			pushFrame: &state.FlowFrame{
				Flow:      branchFlow,
				StepCount: len(planned.Steps),
				Previous:  previous,
				Return: &state.FrameReturn{
					StepType:  result.Type,
					StepIndex: stepIndex,
					StepID:    step.ID,
					CaseIndex: &idx,
				},
			},
		}, result
	}

	result.Status = state.StepStatusSucceeded
	result.Value = switchResultValue(nil, nil)
	return stepAction{}, r.finalizeStepResult(config, responses, previous, step, result)
}

func switchCaseMatches(stepIndex int, caseIndex int, branch switchCase, runtime exprruntime.Runtime) (bool, *state.Failure) {
	if branch.When == nil {
		return true, nil
	}

	resolved, err := runtime.Resolve(branch.When)
	if err != nil {
		return false, &state.Failure{
			Code:    "invalid_switch",
			Message: fmt.Sprintf("step %d switch case %d when is invalid: %v", stepIndex, caseIndex, err),
		}
	}

	matched, ok := resolved.(bool)
	if !ok {
		return false, &state.Failure{
			Code:    "invalid_switch",
			Message: fmt.Sprintf("step %d switch case %d when must be a boolean", stepIndex, caseIndex),
		}
	}
	return matched, nil
}

func (r *Runner) finalizeStepResult(
	config Config,
	responses response.Resolver,
	previous *state.StepResult,
	step spec.Step,
	result state.StepResult,
) state.StepResult {
	runtime := exprruntime.NewRuntime(buildRuntimeContext(config, previous))

	if result.Status == state.StepStatusSucceeded {
		resolved, failure := responses.Apply(result.Index, config.SpecPath, step, result)
		result = resolved
		if failure != nil {
			result.Status = state.StepStatusFailed
			result.Error = failure
		}
	}

	if result.Status == state.StepStatusSucceeded {
		if failure := expectStep(result.Index, step, runtime, result); failure != nil {
			result.Status = state.StepStatusFailed
			result.Error = failure
		}
	}

	return finalizeStatus(result)
}

func finalizeStatus(result state.StepResult) state.StepResult {
	switch result.Status {
	case state.StepStatusSucceeded, state.StepStatusSkipped:
		return result
	case state.StepStatusFailed:
		if result.Error == nil {
			result.Error = &state.Failure{
				Code:    "step_failed",
				Message: fmt.Sprintf("step %d failed", result.Index),
			}
		}
		return result
	default:
		result.Status = state.StepStatusFailed
		result.Error = &state.Failure{
			Code:    "invalid_status",
			Message: fmt.Sprintf("step %d returned unsupported status %q", result.Index, result.Status),
		}
		return result
	}
}

func returnedControlResult(meta *state.FrameReturn, produced *state.StepResult) state.StepResult {
	result := executorapi.NormalizeResult(state.StepResult{
		Index:  meta.StepIndex,
		ID:     meta.StepID,
		Type:   meta.StepType,
		Status: state.StepStatusSucceeded,
	})

	switch meta.StepType {
	case "call":
		result.Value = callResultValue(meta.Flow, produced)
	case "switch":
		result.Value = switchResultValue(meta.CaseIndex, produced)
	default:
		result.Status = state.StepStatusFailed
		result.Error = &state.Failure{
			Code:    "invalid_return",
			Message: fmt.Sprintf("step %d returned unsupported control type %q", meta.StepIndex, meta.StepType),
		}
	}

	return result
}

func callResultValue(flow string, previous *state.StepResult) map[string]any {
	value := nestedResultValue(previous)
	value["flow"] = flow
	return value
}

func switchResultValue(caseIndex *int, previous *state.StepResult) map[string]any {
	value := nestedResultValue(previous)
	value["matched"] = caseIndex != nil
	if caseIndex == nil {
		value["case"] = nil
		return value
	}

	value["case"] = *caseIndex
	return value
}

func nestedResultValue(previous *state.StepResult) map[string]any {
	value := map[string]any{
		"status":    string(state.StepStatusSucceeded),
		"value":     nil,
		"error":     nil,
		"artifacts": artifactsContext(state.Artifacts{Files: map[string]string{}}),
	}
	if previous == nil {
		return value
	}

	value["index"] = previous.Index
	value["type"] = previous.Type
	value["status"] = string(previous.Status)
	value["value"] = jsonutil.CloneValue(previous.Value)
	value["error"] = failureContext(previous.Error)
	value["artifacts"] = artifactsContext(previous.Artifacts)
	return value
}

func (r *Runner) recordResultEvent(
	store *state.Store,
	reporter *progress.Reporter,
	config Config,
	runID string,
	flowName string,
	step spec.Step,
	previous *state.StepResult,
	kind state.EventKind,
	result state.StepResult,
) (state.Snapshot, error) {
	snapshot, err := store.Append(state.RunEvent{
		Kind: kind,
		Step: &result,
	})
	if err != nil {
		return state.Snapshot{}, err
	}
	reporter.StepFinished(progressResultStep(config, flowName, step, previous, result))

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
	reporter.RunFinished(progress.RunStatusFailed, progressFailure(failure))

	return snapshot, RunFailedError{
		RunID:   runID,
		Failure: *failure,
	}
}

func progressStep(config Config, flowName string, stepIndex int, step spec.Step, previous *state.StepResult) progress.Step {
	stepCtx := progressStepContext(config, flowName, stepIndex, step, previous)
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

func progressResultStep(config Config, flowName string, step spec.Step, previous *state.StepResult, result state.StepResult) progress.Step {
	stepCtx := progressStepContext(config, flowName, result.Index, step, previous)
	progressStep, err := progress.StepFromResultWithContext(flowName, stepCtx, result)
	if err == nil {
		return progressStep
	}
	return progress.StepFromResult(flowName, result)
}

func progressStepContext(config Config, flowName string, stepIndex int, step spec.Step, previous *state.StepResult) executorapi.StepContext {
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
		Runtime:   exprruntime.NewRuntime(buildRuntimeContext(config, previous)),
	}
}

func progressFailure(failure *state.Failure) *progress.Failure {
	if failure == nil {
		return nil
	}
	return &progress.Failure{
		Code:    failure.Code,
		Message: failure.Message,
	}
}

func resumeActiveProgressSteps(config Config, plan *flowPlan, snapshot state.Snapshot) []progress.Step {
	if len(snapshot.Frames) < 2 {
		return []progress.Step{}
	}

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

		steps = append(steps, progressStep(config, parentFlowName, frame.Return.StepIndex, parentFlow.Steps[frame.Return.StepIndex], frame.Previous))
	}

	return steps
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

	boundRuntime := runtime.WithBindings(stepResultContext(result))
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
