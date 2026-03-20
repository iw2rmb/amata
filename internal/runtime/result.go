package runtime

import (
	"fmt"

	executorapi "github.com/iw2rmb/amata/internal/executor"
	"github.com/iw2rmb/amata/internal/jsonutil"
	"github.com/iw2rmb/amata/internal/runtime/response"
	"github.com/iw2rmb/amata/internal/spec"
	"github.com/iw2rmb/amata/internal/state"
)

func (r *Runner) finalizeStepResult(
	config Config,
	responses response.Resolver,
	lookup func(*state.StepRef) *state.StepResult,
	previous *state.StepResult,
	bindings map[string]any,
	step spec.Step,
	result state.StepResult,
) state.StepResult {
	runtime := newStepRuntime(config, previous, lookup, bindings)

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
