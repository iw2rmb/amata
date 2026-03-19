package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	executorapi "github.com/iw2rmb/amata/internal/executor"
	exprruntime "github.com/iw2rmb/amata/internal/expr"
	"github.com/iw2rmb/amata/internal/jsonutil"
	"github.com/iw2rmb/amata/internal/progress"
	"github.com/iw2rmb/amata/internal/runtime/response"
	"github.com/iw2rmb/amata/internal/schema"
	"github.com/iw2rmb/amata/internal/spec"
	"github.com/iw2rmb/amata/internal/state"
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

const (
	defaultStallAfter   = 15 * time.Minute
	stallCallReturnType = "stall.call"
)

type stallPolicy struct {
	After  time.Duration
	Action string
	Flow   string
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
			reporter.RunFinished(progress.RunStatusFailed, state.CloneFailure(failure))
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
				ID:        state.FrameID(1),
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
		previous := snapshot.StepByRef(frame.Previous)
		produced := snapshot.StepByRef(frame.Produced)
		lookup := snapshot.StepByRef
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
			parentPrevious := snapshot.StepByRef(parentFrame.Previous)
			parentFlow, ok := plan.Lookup(parentFrame.Flow)
			if !ok {
				return state.Snapshot{}, fmt.Errorf("flow %q is not defined", parentFrame.Flow)
			}
			parentStep := parentFlow.Steps[frame.Return.StepIndex]
			if frame.Return.StepType == "for_each" {
				nextFrame, finalized := r.prepareForEachContinuation(config, plan, responses, lookup, parentFrame, parentPrevious, parentStep, frame.Return, produced)
				if nextFrame != nil {
					nextFrame.ID = state.FrameID(snapshot.LastSequence + 1)
					snapshot, err = store.Append(state.RunEvent{
						Kind:  state.EventControlContinued,
						Frame: nextFrame,
					})
					if err != nil {
						return state.Snapshot{}, err
					}
					continue
				}

				if snapshot, err = r.recordResultEvent(store, reporter, config, config.RunID, parentFrame.Flow, parentFrame.ID, parentStep, parentPrevious, parentFrame.Bindings, state.EventControlReturned, finalized, lookup); err != nil {
					return snapshot, err
				}
				continue
			}

			returned := returnedControlResult(frame.Return, produced)
			finalized := r.finalizeStepResult(config, responses, lookup, parentPrevious, parentFrame.Bindings, parentStep, returned)

			if snapshot, err = r.recordResultEvent(store, reporter, config, config.RunID, parentFrame.Flow, parentFrame.ID, parentStep, parentPrevious, parentFrame.Bindings, state.EventControlReturned, finalized, lookup); err != nil {
				return snapshot, err
			}
			continue
		}

		stepIndex := frame.NextStep
		step := flow.Steps[stepIndex]
		if snapshot, err = r.recordStepStartedEvent(store, reporter, config, frame.Flow, frame.ID, stepIndex, step, previous, frame.Bindings, lookup); err != nil {
			return state.Snapshot{}, err
		}
		runtime := exprruntime.NewRuntime(buildRuntimeContext(config, previous, lookup, frame.Bindings))

		switch step.ExecutorType() {
		case "call":
			action, result := r.prepareStepAction(config, runtime, previous, stepIndex, step)
			if action.pushFrame != nil {
				action.pushFrame.ID = state.FrameID(snapshot.LastSequence + 1)
				snapshot, err = store.Append(state.RunEvent{
					Kind:  state.EventFramePushed,
					Frame: action.pushFrame,
				})
				if err != nil {
					return state.Snapshot{}, err
				}
				continue
			}
			if snapshot, err = r.recordResultEvent(store, reporter, config, config.RunID, frame.Flow, frame.ID, step, previous, frame.Bindings, state.EventStepRecorded, result, lookup); err != nil {
				return snapshot, err
			}
		case "switch":
			action, result := r.prepareSwitch(config, runtime, plan, responses, lookup, frame.Flow, previous, frame.Bindings, stepIndex, step)
			if action.pushFrame != nil {
				action.pushFrame.ID = state.FrameID(snapshot.LastSequence + 1)
				snapshot, err = store.Append(state.RunEvent{
					Kind:  state.EventFramePushed,
					Frame: action.pushFrame,
				})
				if err != nil {
					return state.Snapshot{}, err
				}
				continue
			}
			if snapshot, err = r.recordResultEvent(store, reporter, config, config.RunID, frame.Flow, frame.ID, step, previous, frame.Bindings, state.EventStepRecorded, result, lookup); err != nil {
				return snapshot, err
			}
		case "for_each":
			action, result := r.prepareForEach(config, runtime, plan, responses, lookup, frame.Flow, previous, frame.Bindings, stepIndex, step)
			if action.pushFrame != nil {
				action.pushFrame.ID = state.FrameID(snapshot.LastSequence + 1)
				snapshot, err = store.Append(state.RunEvent{
					Kind:  state.EventFramePushed,
					Frame: action.pushFrame,
				})
				if err != nil {
					return state.Snapshot{}, err
				}
				continue
			}
			if snapshot, err = r.recordResultEvent(store, reporter, config, config.RunID, frame.Flow, frame.ID, step, previous, frame.Bindings, state.EventStepRecorded, result, lookup); err != nil {
				return snapshot, err
			}
		default:
			action, result := r.executeStep(ctx, config, responses, snapshot, frame.Flow, stepIndex, step, previous, frame.Bindings)
			if action.pushFrame != nil {
				action.pushFrame.ID = state.FrameID(snapshot.LastSequence + 1)
				snapshot, err = store.Append(state.RunEvent{
					Kind:  state.EventFramePushed,
					Frame: action.pushFrame,
				})
				if err != nil {
					return state.Snapshot{}, err
				}
				continue
			}
			if snapshot, err = r.recordResultEvent(store, reporter, config, config.RunID, frame.Flow, frame.ID, step, previous, frame.Bindings, state.EventStepRecorded, result, lookup); err != nil {
				return snapshot, err
			}
		}
	}
}

func (r *Runner) executeStep(
	ctx context.Context,
	config Config,
	responses response.Resolver,
	snapshot state.Snapshot,
	flowName string,
	stepIndex int,
	step spec.Step,
	previous *state.StepResult,
	bindings map[string]any,
) (stepAction, state.StepResult) {
	runtime := exprruntime.NewRuntime(buildRuntimeContext(config, previous, snapshot.StepByRef, bindings))
	action, result := r.prepareStepAction(config, runtime, previous, stepIndex, step)
	if result.Status != "" || action.pushFrame != nil {
		return action, finalizeStatus(result)
	}

	factory, ok := r.registry.Lookup(result.Type)
	if !ok {
		result.Status = state.StepStatusFailed
		result.Error = &state.Failure{
			Code:    "unknown_executor",
			Message: fmt.Sprintf("executor %q is not registered", result.Type),
		}
		return stepAction{}, result
	}

	stepExecutor := factory()
	if stepExecutor == nil {
		result.Status = state.StepStatusFailed
		result.Error = &state.Failure{
			Code:    "invalid_executor",
			Message: fmt.Sprintf("executor %q returned nil", result.Type),
		}
		return stepAction{}, result
	}

	policy, failure := resolveStallPolicy(runtime, config.Spec.Defaults, stepIndex, step)
	if failure != nil {
		result.Status = state.StepStatusFailed
		result.Error = failure
		return stepAction{}, finalizeStatus(result)
	}

	for attempt := 1; ; attempt++ {
		stepCtx := executorapi.StepContext{
			RunID:          config.RunID,
			RunDir:         config.RunDir,
			SpecPath:       config.SpecPath,
			Spec:           config.Spec,
			Workspace:      config.Workspace,
			FlowName:       flowName,
			StepIndex:      stepIndex,
			Step:           step,
			Previous:       previous,
			Runtime:        runtime,
			ExecutionLabel: stepExecutionLabel(snapshot.LastSequence+1, attempt),
		}

		if policy == nil {
			result = executeStepAttempt(ctx, stepExecutor, stepCtx, step)
			return stepAction{}, r.finalizeStepResult(config, responses, snapshot.StepByRef, previous, bindings, step, result)
		}

		attemptCtx, cancel := context.WithCancel(ctx)
		done := make(chan state.StepResult, 1)
		go func() {
			done <- executeStepAttempt(attemptCtx, stepExecutor, stepCtx, step)
		}()

		timer := time.NewTimer(policy.After)
		select {
		case result = <-done:
			timer.Stop()
			cancel()
			return stepAction{}, r.finalizeStepResult(config, responses, snapshot.StepByRef, previous, bindings, step, result)
		case <-timer.C:
			cancel()
			<-done
			switch policy.Action {
			case "rerun":
				continue
			case "error":
				return stepAction{}, finalizeStatus(state.StepResult{
					Index:  stepIndex,
					ID:     step.ID,
					Type:   step.ExecutorType(),
					Status: state.StepStatusFailed,
					Error: &state.Failure{
						Code:    "step_stalled",
						Message: fmt.Sprintf("step %d stalled after %s", stepIndex, policy.After),
					},
				})
			case "call":
				action, result := r.stallCallAction(config, stepIndex, step, previous, policy.Flow)
				if result.Status != "" || action.pushFrame == nil {
					return stepAction{}, finalizeStatus(result)
				}
				return action, state.StepResult{}
			default:
				return stepAction{}, finalizeStatus(state.StepResult{
					Index:  stepIndex,
					ID:     step.ID,
					Type:   step.ExecutorType(),
					Status: state.StepStatusFailed,
					Error: &state.Failure{
						Code:    "invalid_stall",
						Message: fmt.Sprintf("step %d stall action %q is unsupported", stepIndex, policy.Action),
					},
				})
			}
		case <-ctx.Done():
			timer.Stop()
			cancel()
			result = <-done
			return stepAction{}, r.finalizeStepResult(config, responses, snapshot.StepByRef, previous, bindings, step, result)
		}
	}
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
			Previous:  stepRef(previous),
			Return: &state.FrameReturn{
				StepType:  result.Type,
				StepIndex: stepIndex,
				StepID:    step.ID,
				Flow:      targetFlow,
			},
		},
	}, result
}

func executeStepAttempt(ctx context.Context, stepExecutor executorapi.Executor, stepCtx executorapi.StepContext, step spec.Step) state.StepResult {
	result := stepExecutor.Execute(ctx, stepCtx)
	result = executorapi.NormalizeResult(result)
	result.Index = stepCtx.StepIndex
	if result.ID == "" {
		result.ID = step.ID
	}
	if result.Type == "" {
		result.Type = step.ExecutorType()
	}
	return result
}

func resolveStallPolicy(runtime exprruntime.Runtime, defaults map[string]any, stepIndex int, step spec.Step) (*stallPolicy, *state.Failure) {
	raw, ok, err := resolveRawStallPolicy(defaults, step)
	if err != nil {
		return nil, invalidStallFailure(stepIndex, err.Error())
	}
	if !ok {
		return nil, nil
	}

	policy := &stallPolicy{
		After:  defaultStallAfter,
		Action: "rerun",
	}

	switch typed := raw.(type) {
	case string:
		action, err := runtime.ResolveString(typed)
		if err != nil {
			return nil, invalidStallFailure(stepIndex, err.Error())
		}
		policy.Action = action
	case map[string]any:
		if actionRaw, ok := typed["type"]; ok {
			action, err := runtime.ResolveString(actionRaw)
			if err != nil {
				return nil, invalidStallFailure(stepIndex, fmt.Sprintf("resolve type: %v", err))
			}
			policy.Action = action
		}
		if afterRaw, ok := typed["after"]; ok {
			after, err := resolveStallAfter(runtime, afterRaw)
			if err != nil {
				return nil, invalidStallFailure(stepIndex, fmt.Sprintf("resolve after: %v", err))
			}
			policy.After = after
		}
		if flowRaw, ok := typed["flow"]; ok {
			flow, err := runtime.ResolveString(flowRaw)
			if err != nil {
				return nil, invalidStallFailure(stepIndex, fmt.Sprintf("resolve flow: %v", err))
			}
			policy.Flow = flow
		}
	default:
		return nil, invalidStallFailure(stepIndex, "must be a string or object")
	}

	switch policy.Action {
	case "rerun", "error":
	case "call":
		if policy.Flow == "" {
			return nil, invalidStallFailure(stepIndex, "call action requires flow")
		}
	default:
		return nil, invalidStallFailure(stepIndex, fmt.Sprintf("unsupported action %q", policy.Action))
	}
	if policy.After <= 0 {
		return nil, invalidStallFailure(stepIndex, "after must be greater than zero")
	}

	return policy, nil
}

func resolveRawStallPolicy(defaults map[string]any, step spec.Step) (any, bool, error) {
	if raw, ok := step.Fields["stall"]; ok {
		return raw, true, nil
	}

	rawExecutors, ok := defaults["executors"]
	if !ok {
		return nil, false, nil
	}
	executors, ok := rawExecutors.(map[string]any)
	if !ok {
		return nil, false, fmt.Errorf("defaults.executors must be a map")
	}

	executorType := step.ExecutorType()
	if executorType == "" {
		return nil, false, nil
	}
	rawExecutorDefaults, ok := executors[executorType]
	if !ok {
		return nil, false, nil
	}
	executorDefaults, ok := rawExecutorDefaults.(map[string]any)
	if !ok {
		return nil, false, fmt.Errorf("defaults.executors.%s must be a map", executorType)
	}

	rawStall, ok := executorDefaults["stall"]
	if !ok {
		return nil, false, nil
	}
	return rawStall, true, nil
}

func resolveStallAfter(runtime exprruntime.Runtime, raw any) (time.Duration, error) {
	resolved, err := runtime.Resolve(raw)
	if err != nil {
		return 0, err
	}

	switch typed := resolved.(type) {
	case int:
		return time.Duration(typed * int(time.Minute)), nil
	case int64:
		return time.Duration(typed) * time.Minute, nil
	case float64:
		return time.Duration(typed * float64(time.Minute)), nil
	case string:
		if duration, err := time.ParseDuration(typed); err == nil {
			return duration, nil
		}
		minutes, err := strconv.ParseFloat(typed, 64)
		if err != nil {
			return 0, fmt.Errorf("expected minutes number or duration string")
		}
		return time.Duration(minutes * float64(time.Minute)), nil
	default:
		return 0, fmt.Errorf("expected minutes number or duration string")
	}
}

func invalidStallFailure(stepIndex int, reason string) *state.Failure {
	return &state.Failure{
		Code:    "invalid_stall",
		Message: fmt.Sprintf("step %d stall is invalid: %s", stepIndex, reason),
	}
}

func stepExecutionLabel(sequence int, attempt int) string {
	return fmt.Sprintf("seq-%06d-a%02d", sequence, attempt)
}

func (r *Runner) stallCallAction(config Config, stepIndex int, step spec.Step, previous *state.StepResult, flow string) (stepAction, state.StepResult) {
	target, ok := config.Spec.Flows[flow]
	if !ok {
		return stepAction{}, finalizeStatus(state.StepResult{
			Index:  stepIndex,
			ID:     step.ID,
			Type:   step.ExecutorType(),
			Status: state.StepStatusFailed,
			Error: &state.Failure{
				Code:    "unknown_flow",
				Message: fmt.Sprintf("step %d stall flow %q is not defined", stepIndex, flow),
			},
		})
	}

	return stepAction{
		pushFrame: &state.FlowFrame{
			Flow:      flow,
			StepCount: len(target.Steps),
			Previous:  stepRef(previous),
			Return: &state.FrameReturn{
				StepType:   stallCallReturnType,
				ResultType: step.ExecutorType(),
				StepIndex:  stepIndex,
				StepID:     step.ID,
				Flow:       flow,
			},
		},
	}, state.StepResult{}
}

func (r *Runner) prepareSwitch(
	config Config,
	runtime exprruntime.Runtime,
	plan *flowPlan,
	responses response.Resolver,
	lookup func(*state.StepRef) *state.StepResult,
	flowName string,
	previous *state.StepResult,
	bindings map[string]any,
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
				Previous:  stepRef(previous),
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
	return stepAction{}, r.finalizeStepResult(config, responses, lookup, previous, bindings, step, result)
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

func (r *Runner) prepareForEach(
	config Config,
	runtime exprruntime.Runtime,
	plan *flowPlan,
	responses response.Resolver,
	lookup func(*state.StepRef) *state.StepResult,
	flowName string,
	previous *state.StepResult,
	bindings map[string]any,
	stepIndex int,
	step spec.Step,
) (stepAction, state.StepResult) {
	action, result := r.prepareStepAction(config, runtime, previous, stepIndex, step)
	if result.Type != "for_each" || result.Status != "" || action.pushFrame != nil {
		return action, result
	}

	decoded, err := decodeForEach(step)
	if err != nil {
		result.Status = state.StepStatusFailed
		result.Error = &state.Failure{
			Code:    "invalid_for_each",
			Message: fmt.Sprintf("step %d for_each is invalid: %v", stepIndex, err),
		}
		return stepAction{}, finalizeStatus(result)
	}

	items, err := resolveForEachItems(runtime, decoded.Items)
	if err != nil {
		result.Status = state.StepStatusFailed
		result.Error = &state.Failure{
			Code:    "invalid_for_each",
			Message: fmt.Sprintf("step %d for_each items are invalid: %v", stepIndex, err),
		}
		return stepAction{}, finalizeStatus(result)
	}

	if decoded.As == "" {
		decoded.As = "item"
	}

	if len(items) == 0 {
		result.Status = state.StepStatusSucceeded
		result.Value = forEachResultValue(&state.ForEachState{Items: items, Index: -1, As: decoded.As}, nil)
		return stepAction{}, r.finalizeStepResult(config, responses, lookup, previous, bindings, step, result)
	}

	bodyFlow, ok := plan.ForEachBodyFlow(flowName, stepIndex)
	if !ok {
		result.Status = state.StepStatusFailed
		result.Error = &state.Failure{
			Code:    "invalid_for_each",
			Message: fmt.Sprintf("step %d for_each body is not planned", stepIndex),
		}
		return stepAction{}, finalizeStatus(result)
	}
	planned, ok := plan.Lookup(bodyFlow)
	if !ok {
		result.Status = state.StepStatusFailed
		result.Error = &state.Failure{
			Code:    "invalid_for_each",
			Message: fmt.Sprintf("step %d for_each body flow %q is missing", stepIndex, bodyFlow),
		}
		return stepAction{}, finalizeStatus(result)
	}

	return stepAction{
		pushFrame: &state.FlowFrame{
			Flow:      bodyFlow,
			StepCount: len(planned.Steps),
			Previous:  stepRef(previous),
			Bindings:  forEachBindings(items[0], 0, decoded.As),
			Return: &state.FrameReturn{
				StepType:  result.Type,
				StepIndex: stepIndex,
				StepID:    step.ID,
				ForEach: &state.ForEachState{
					Items: items,
					Index: 0,
					As:    decoded.As,
				},
			},
		},
	}, result
}

func (r *Runner) prepareForEachContinuation(
	config Config,
	plan *flowPlan,
	responses response.Resolver,
	lookup func(*state.StepRef) *state.StepResult,
	parentFrame state.FlowFrame,
	parentPrevious *state.StepResult,
	parentStep spec.Step,
	meta *state.FrameReturn,
	produced *state.StepResult,
) (*state.FlowFrame, state.StepResult) {
	if meta == nil || meta.ForEach == nil {
		index := 0
		stepID := ""
		stepType := "for_each"
		if meta != nil {
			index = meta.StepIndex
			stepID = meta.StepID
			if meta.StepType != "" {
				stepType = meta.StepType
			}
		}
		return nil, finalizeStatus(state.StepResult{
			Index:  index,
			ID:     stepID,
			Type:   stepType,
			Status: state.StepStatusFailed,
			Error: &state.Failure{
				Code:    "invalid_return",
				Message: fmt.Sprintf("step %d for_each return is missing iteration state", index),
			},
		})
	}

	nextIndex := meta.ForEach.Index + 1
	if nextIndex < len(meta.ForEach.Items) {
		bodyFlow, ok := plan.ForEachBodyFlow(parentFrame.Flow, meta.StepIndex)
		if !ok {
			return nil, finalizeStatus(state.StepResult{
				Index:  meta.StepIndex,
				ID:     meta.StepID,
				Type:   meta.StepType,
				Status: state.StepStatusFailed,
				Error: &state.Failure{
					Code:    "invalid_for_each",
					Message: fmt.Sprintf("step %d for_each body is not planned", meta.StepIndex),
				},
			})
		}
		planned, ok := plan.Lookup(bodyFlow)
		if !ok {
			return nil, finalizeStatus(state.StepResult{
				Index:  meta.StepIndex,
				ID:     meta.StepID,
				Type:   meta.StepType,
				Status: state.StepStatusFailed,
				Error: &state.Failure{
					Code:    "invalid_for_each",
					Message: fmt.Sprintf("step %d for_each body flow %q is missing", meta.StepIndex, bodyFlow),
				},
			})
		}

		return &state.FlowFrame{
			Flow:      bodyFlow,
			StepCount: len(planned.Steps),
			Previous:  parentFrame.Previous,
			Bindings:  forEachBindings(meta.ForEach.Items[nextIndex], nextIndex, meta.ForEach.As),
			Return: &state.FrameReturn{
				StepType:  meta.StepType,
				StepIndex: meta.StepIndex,
				StepID:    meta.StepID,
				ForEach: &state.ForEachState{
					Items: meta.ForEach.Items,
					Index: nextIndex,
					As:    meta.ForEach.As,
				},
			},
		}, state.StepResult{}
	}

	returned := returnedControlResult(meta, produced)
	return nil, r.finalizeStepResult(config, responses, lookup, parentPrevious, parentFrame.Bindings, parentStep, returned)
}

func resolveForEachItems(runtime exprruntime.Runtime, raw any) ([]any, error) {
	resolved, err := runtime.Resolve(raw)
	if err != nil {
		return nil, err
	}

	switch typed := resolved.(type) {
	case []any:
		return jsonutil.CloneValue(typed).([]any), nil
	case []string:
		items := make([]any, 0, len(typed))
		for _, item := range typed {
			items = append(items, item)
		}
		return items, nil
	default:
		return nil, fmt.Errorf("must resolve to an array")
	}
}

func forEachBindings(item any, index int, as string) map[string]any {
	bindings := map[string]any{
		"item":  jsonutil.CloneValue(item),
		"index": index,
	}
	if as != "" {
		bindings[as] = jsonutil.CloneValue(item)
	}
	return bindings
}

func (r *Runner) finalizeStepResult(
	config Config,
	responses response.Resolver,
	lookup func(*state.StepRef) *state.StepResult,
	previous *state.StepResult,
	bindings map[string]any,
	step spec.Step,
	result state.StepResult,
) state.StepResult {
	runtime := exprruntime.NewRuntime(buildRuntimeContext(config, previous, lookup, bindings))

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
	if meta.ResultType != "" {
		result.Type = meta.ResultType
	}

	switch meta.StepType {
	case "call":
		result.Value = callResultValue(meta.Flow, produced)
	case "switch":
		result.Value = switchResultValue(meta.CaseIndex, produced)
	case "for_each":
		result.Value = forEachResultValue(meta.ForEach, produced)
	case stallCallReturnType:
		if produced == nil {
			result.Status = state.StepStatusFailed
			result.Error = &state.Failure{
				Code:    "invalid_return",
				Message: fmt.Sprintf("step %d stall call returned no result", meta.StepIndex),
			}
			return result
		}
		result = executorapi.NormalizeResult(*produced)
		result.Index = meta.StepIndex
		result.ID = meta.StepID
		if meta.ResultType != "" {
			result.Type = meta.ResultType
		}
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

func forEachResultValue(loop *state.ForEachState, previous *state.StepResult) map[string]any {
	value := nestedResultValue(previous)
	value["count"] = 0
	value["index"] = nil
	value["item"] = nil
	value["as"] = "item"
	if loop == nil {
		return value
	}

	value["count"] = len(loop.Items)
	if loop.As != "" {
		value["as"] = loop.As
	}
	if loop.Index >= 0 && loop.Index < len(loop.Items) {
		value["index"] = loop.Index
		value["item"] = jsonutil.CloneValue(loop.Items[loop.Index])
	}
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
		Runtime:   exprruntime.NewRuntime(buildRuntimeContext(config, previous, lookup, bindings)),
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
