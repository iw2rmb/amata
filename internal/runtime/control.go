package runtime

import (
	"fmt"

	executorapi "github.com/iw2rmb/amata/internal/executor"
	exprruntime "github.com/iw2rmb/amata/internal/expr"
	"github.com/iw2rmb/amata/internal/jsonutil"
	"github.com/iw2rmb/amata/internal/runtime/response"
	"github.com/iw2rmb/amata/internal/spec"
	"github.com/iw2rmb/amata/internal/state"
)

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
