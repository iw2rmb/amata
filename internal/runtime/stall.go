package runtime

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	executorapi "github.com/iw2rmb/amata/internal/executor"
	exprruntime "github.com/iw2rmb/amata/internal/expr"
	"github.com/iw2rmb/amata/internal/runtime/response"
	"github.com/iw2rmb/amata/internal/spec"
	"github.com/iw2rmb/amata/internal/state"
)

const (
	defaultStallAfter          = 15 * time.Minute
	stallCancellationGraceWait = 1 * time.Second
	stallCallReturnType        = "stall.call"
)

type stallPolicy struct {
	After  time.Duration
	Action string
	Flow   string
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
	runtime := newStepRuntime(config, previous, snapshot.StepByRef, bindings)
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
			if _, ok := waitForAttemptResult(done, stallCancellationGraceWait); !ok {
				return stepAction{}, finalizeStatus(state.StepResult{
					Index:  stepIndex,
					ID:     step.ID,
					Type:   step.ExecutorType(),
					Status: state.StepStatusFailed,
					Error: &state.Failure{
						Code:    "step_stalled",
						Message: fmt.Sprintf("step %d stalled after %s and did not stop within %s after cancellation", stepIndex, policy.After, stallCancellationGraceWait),
					},
				})
			}
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
			result, ok := waitForAttemptResult(done, stallCancellationGraceWait)
			if !ok {
				errCode := "canceled"
				errMessage := fmt.Sprintf("step %d canceled but did not stop within %s", stepIndex, stallCancellationGraceWait)
				if errors.Is(ctx.Err(), context.DeadlineExceeded) {
					errCode = "deadline_exceeded"
					errMessage = fmt.Sprintf("step %d deadline exceeded and executor did not stop within %s", stepIndex, stallCancellationGraceWait)
				}
				return stepAction{}, finalizeStatus(state.StepResult{
					Index:  stepIndex,
					ID:     step.ID,
					Type:   step.ExecutorType(),
					Status: state.StepStatusFailed,
					Error: &state.Failure{
						Code:    errCode,
						Message: errMessage,
					},
				})
			}
			return stepAction{}, r.finalizeStepResult(config, responses, snapshot.StepByRef, previous, bindings, step, result)
		}
	}
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

func waitForAttemptResult(done <-chan state.StepResult, timeout time.Duration) (state.StepResult, bool) {
	if timeout <= 0 {
		select {
		case result := <-done:
			return result, true
		default:
			return state.StepResult{}, false
		}
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case result := <-done:
		return result, true
	case <-timer.C:
		return state.StepResult{}, false
	}
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
