package runtime

import (
	"context"
	"fmt"

	"github.com/iw2rmb/amata/internal/progress"
	"github.com/iw2rmb/amata/internal/spec"
	"github.com/iw2rmb/amata/internal/state"
)

func (r *Runner) executeStep(ctx context.Context, reporter *progress.Reporter, config Config, responses responseResolver, snapshot state.Snapshot, flowName, frameID string, stepIndex int, step spec.Step, previous *state.StepResult, bindings map[string]any) (stepAction, state.StepResult, func() error) {
	attempts := 0
	continuation := retryContext{}
	roles, err := spec.ParseAgentRoles(config.Spec.Defaults)
	if err != nil {
		return stepAction{}, roleFailure(stepIndex, step, "invalid_defaults", err), nil
	}
	candidates, err := roles.Candidates(step)
	if err != nil {
		return stepAction{}, roleFailure(stepIndex, step, "invalid_defaults", err), nil
	}
	if len(candidates) == 0 {
		return r.executeCandidate(ctx, reporter, config, responses, snapshot, flowName, frameID, stepIndex, step, previous, bindings, &continuation, &attempts)
	}
	runtime := newStepRuntime(config, previous, snapshot.StepByRef, bindings)
	action, result := r.prepareStepAction(config, runtime, previous, stepIndex, step)
	if result.Status != "" {
		return action, result, nil
	}
	var cleanup func() error
	for i, candidate := range candidates {
		if i > 0 {
			previousSession := continuation.ContinuationSessionID
			continuation = retryContext{}
			if candidate.Type == candidates[i-1].Type {
				sessionID := roleContinuationSessionID(candidate.Type, result)
				if sessionID == "" {
					sessionID = previousSession
				}
				if sessionID != "" {
					continuation = retryContext{ContinuationSessionID: sessionID, ContinuationPrompt: defaultAgentResumePrompt}
				}
			}
			reporter.StepFinished(progressResultStep(config, flowName, frameID, candidates[i-1], previous, bindings, result, snapshot.StepByRef))
			reporter.StepStarted(progressStep(config, flowName, frameID, stepIndex, candidate, stepExecutionLabel(snapshot.LastSequence+1, attempts+1), previous, bindings, snapshot.StepByRef))
		}
		action, result, cleanup = r.executeCandidate(ctx, reporter, config, responses, snapshot, flowName, frameID, stepIndex, candidate, previous, bindings, &continuation, &attempts)
		if result.Status != state.StepStatusFailed || action.pushFrame != nil || ctx.Err() != nil {
			return action, result, cleanup
		}
		if result.Error != nil && result.Error.Details["attempt_running"] == true {
			return action, result, cleanup
		}
	}
	return action, result, cleanup
}

func roleContinuationSessionID(agentName string, result state.StepResult) string {
	if agentName == "nessy" {
		return continuationSessionIDFromMetadata(result)
	}
	return structuredRetrySessionID(agentName, result)
}

func roleFailure(index int, step spec.Step, code string, err error) state.StepResult {
	return finalizeStatus(state.StepResult{Index: index, ID: step.ID, Type: step.ExecutorType(), Status: state.StepStatusFailed, Error: &state.Failure{Code: code, Message: fmt.Sprintf("step %d: %v", index, err)}})
}
