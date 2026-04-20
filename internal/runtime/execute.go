package runtime

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	executorapi "github.com/iw2rmb/amata/internal/executor"
	exprruntime "github.com/iw2rmb/amata/internal/expr"
	"github.com/iw2rmb/amata/internal/progress"
	"github.com/iw2rmb/amata/internal/spec"
	"github.com/iw2rmb/amata/internal/state"
)

const (
	defaultStallAfter          = 15 * time.Minute
	stallCancellationGraceWait = 1 * time.Second
	stallCallReturnType        = "stall.call"
	stallRerunAttemptsTotal    = "INF"
	defaultCodexTPMRetryAfter  = 1 * time.Minute
	defaultCodexResumePrompt   = "continue"
)

type stallPolicy struct {
	After  time.Duration
	Action string
	Flow   string
}

type codexTPMPolicy struct {
	Enabled    bool
	MaxRetries int
}

type codexTPMRetryContext struct {
	ContinuationSessionID string
	UsedFreshFallback     bool
}

func (r *Runner) executeStep(
	ctx context.Context,
	reporter *progress.Reporter,
	config Config,
	responses responseResolver,
	snapshot state.Snapshot,
	flowName string,
	frameID string,
	stepIndex int,
	step spec.Step,
	previous *state.StepResult,
	bindings map[string]any,
) (stepAction, state.StepResult, func() error) {
	runtime := newStepRuntime(config, previous, snapshot.StepByRef, bindings)
	action, result := r.prepareStepAction(config, runtime, previous, stepIndex, step)
	if result.Status != "" || action.pushFrame != nil {
		return action, finalizeStatus(result), nil
	}

	tpmPolicy, failure := resolveCodexTPMPolicy(runtime, config.Spec.Defaults, stepIndex, step)
	if failure != nil {
		result.Status = state.StepStatusFailed
		result.Error = failure
		return stepAction{}, finalizeStatus(result), nil
	}

	factory, ok := r.registry.Lookup(result.Type)
	if !ok {
		result.Status = state.StepStatusFailed
		result.Error = &state.Failure{
			Code:    "unknown_executor",
			Message: fmt.Sprintf("executor %q is not registered", result.Type),
		}
		return stepAction{}, result, nil
	}

	stepExecutor := factory()
	if stepExecutor == nil {
		result.Status = state.StepStatusFailed
		result.Error = &state.Failure{
			Code:    "invalid_executor",
			Message: fmt.Sprintf("executor %q returned nil", result.Type),
		}
		return stepAction{}, result, nil
	}
	checkpointCleanup := checkpointCleanup(stepExecutor, config.RunDir, frameID, stepIndex)

	policy, failure := resolveStallPolicy(runtime, config.Spec.Defaults, stepIndex, step)
	if failure != nil {
		result.Status = state.StepStatusFailed
		result.Error = failure
		return stepAction{}, finalizeStatus(result), nil
	}

	codexTPMRetryCount := 0
	retryContext := codexTPMRetryContext{}

attemptLoop:
	for attempt := 1; ; attempt++ {
		stepCtx := executorapi.StepContext{
			RunID:                 config.RunID,
			RunDir:                config.RunDir,
			FrameID:               frameID,
			SpecPath:              config.SpecPath,
			Spec:                  config.Spec,
			Workspace:             config.Workspace,
			FlowName:              flowName,
			StepIndex:             stepIndex,
			Step:                  step,
			Previous:              previous,
			Runtime:               runtime,
			ExecutionLabel:        stepExecutionLabel(snapshot.LastSequence+1, attempt),
			ContinuationSessionID: retryContext.ContinuationSessionID,
		}
		if retryContext.ContinuationSessionID != "" {
			stepCtx.ContinuationPrompt = defaultCodexResumePrompt
		}

		if policy == nil {
			result = executeStepAttempt(ctx, stepExecutor, stepCtx, step)
			finalized := r.finalizeStepResult(config, responses, snapshot.StepByRef, previous, bindings, step, result)
			if retry, nextRetryContext, aborted := r.maybeRetryCodexTPM(ctx, stepIndex, step, finalized, tpmPolicy, &codexTPMRetryCount, retryContext); aborted.Status != "" {
				return stepAction{}, aborted, nil
			} else if retry {
				retryContext = nextRetryContext
				continue attemptLoop
			}
			return stepAction{}, finalized, checkpointCleanup
		}

		attemptCtx, cancel := context.WithCancel(ctx)
		done := make(chan state.StepResult, 1)
		go func() {
			done <- executeStepAttempt(attemptCtx, stepExecutor, stepCtx, step)
		}()

		activityProbe := newStepActivityProbe(stepCtx)
		lastActivityAt := time.Now().UTC()
		activityProbe.ObserveChanged()

		timer := time.NewTimer(policy.After)
		probeTicker := time.NewTicker(stallProbeInterval(policy.After))
		for {
			select {
			case result = <-done:
				timer.Stop()
				probeTicker.Stop()
				cancel()
				finalized := r.finalizeStepResult(config, responses, snapshot.StepByRef, previous, bindings, step, result)
				if retry, nextRetryContext, aborted := r.maybeRetryCodexTPM(ctx, stepIndex, step, finalized, tpmPolicy, &codexTPMRetryCount, retryContext); aborted.Status != "" {
					return stepAction{}, aborted, nil
				} else if retry {
					retryContext = nextRetryContext
					continue attemptLoop
				}
				return stepAction{}, finalized, checkpointCleanup
			case <-probeTicker.C:
				if activityProbe.ObserveChanged() {
					lastActivityAt = time.Now().UTC()
					resetTimer(timer, policy.After)
				}
			case <-timer.C:
				if activityProbe.ObserveChanged() {
					lastActivityAt = time.Now().UTC()
					resetTimer(timer, policy.After)
					continue
				}
				inactiveFor := time.Since(lastActivityAt)
				if inactiveFor < policy.After {
					resetTimer(timer, policy.After-inactiveFor)
					continue
				}

				cancel()
				probeTicker.Stop()
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
					}), nil
				}
				switch policy.Action {
				case "rerun":
					r.reportStallRerunProgress(reporter, config, snapshot, flowName, frameID, stepIndex, step, previous, bindings, policy.After, attempt)
					continue attemptLoop
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
					}), nil
				case "call":
					action, result := r.stallCallAction(config, stepIndex, step, previous, policy.Flow)
					if result.Status != "" || action.pushFrame == nil {
						return stepAction{}, finalizeStatus(result), nil
					}
					return action, state.StepResult{}, nil
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
					}), nil
				}
			case <-ctx.Done():
				timer.Stop()
				probeTicker.Stop()
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
					}), nil
				}
				finalized := r.finalizeStepResult(config, responses, snapshot.StepByRef, previous, bindings, step, result)
				return stepAction{}, finalized, checkpointCleanup
			}
		}
	}
}

func checkpointCleanup(stepExecutor executorapi.Executor, runDir string, frameID string, stepIndex int) func() error {
	cleaner, ok := stepExecutor.(executorapi.CheckpointCleaner)
	if !ok {
		return nil
	}
	key := executorapi.CheckpointKey{
		RunDir:    runDir,
		FrameID:   frameID,
		StepIndex: stepIndex,
	}
	return func() error {
		return cleaner.CleanupCheckpoint(context.Background(), key)
	}
}

func resolveCodexTPMPolicy(runtime exprruntime.Runtime, defaults map[string]any, stepIndex int, step spec.Step) (codexTPMPolicy, *state.Failure) {
	if step.ExecutorType() != "codex" {
		return codexTPMPolicy{}, nil
	}

	raw, ok := defaults["tpm"]
	if !ok {
		return codexTPMPolicy{}, nil
	}

	resolved, err := runtime.Resolve(raw)
	if err != nil {
		return codexTPMPolicy{}, &state.Failure{
			Code:    "invalid_defaults",
			Message: fmt.Sprintf("step %d defaults.tpm is invalid: %v", stepIndex, err),
		}
	}

	retries := 1
	switch value := resolved.(type) {
	case map[string]any:
		rateSpecified := false
		if rateRaw, ok := value["rate"]; ok {
			rateSpecified = true
			if _, ok := parsePositiveNumber(rateRaw); !ok {
				return codexTPMPolicy{}, &state.Failure{
					Code:    "invalid_defaults",
					Message: fmt.Sprintf("step %d defaults.tpm is invalid: rate must resolve to a positive number", stepIndex),
				}
			}
		}
		retriesSpecified := false
		if retriesRaw, ok := value["retries"]; ok {
			retriesSpecified = true
			parsedRetries, ok := parseNonNegativeInt(retriesRaw)
			if !ok {
				return codexTPMPolicy{}, &state.Failure{
					Code:    "invalid_defaults",
					Message: fmt.Sprintf("step %d defaults.tpm is invalid: retries must resolve to a non-negative integer", stepIndex),
				}
			}
			retries = parsedRetries
		}
		if !rateSpecified && !retriesSpecified {
			return codexTPMPolicy{}, &state.Failure{
				Code:    "invalid_defaults",
				Message: fmt.Sprintf("step %d defaults.tpm is invalid: rate or retries is required", stepIndex),
			}
		}
	default:
		if _, ok := parsePositiveNumber(resolved); !ok {
			return codexTPMPolicy{}, &state.Failure{
				Code:    "invalid_defaults",
				Message: fmt.Sprintf("step %d defaults.tpm is invalid: must resolve to a positive number", stepIndex),
			}
		}
	}

	return codexTPMPolicy{
		Enabled:    true,
		MaxRetries: retries,
	}, nil
}

func (r *Runner) maybeRetryCodexTPM(
	ctx context.Context,
	stepIndex int,
	step spec.Step,
	result state.StepResult,
	tpmPolicy codexTPMPolicy,
	retryCount *int,
	retryContext codexTPMRetryContext,
) (bool, codexTPMRetryContext, state.StepResult) {
	if step.ExecutorType() != "codex" || !tpmPolicy.Enabled || retryCount == nil || *retryCount >= tpmPolicy.MaxRetries {
		return false, retryContext, state.StepResult{}
	}
	sessionID, isRateLimit := rateLimitContinuationSessionID(result)
	if !isRateLimit {
		return false, retryContext, state.StepResult{}
	}
	if sessionID == "" {
		if retryContext.UsedFreshFallback {
			return false, retryContext, state.StepResult{}
		}
		retryContext.UsedFreshFallback = true
		retryContext.ContinuationSessionID = ""
	} else {
		retryContext.ContinuationSessionID = sessionID
	}

	waitFn := r.retryWait
	if waitFn == nil {
		waitFn = waitWithContext
	}
	if err := waitFn(ctx, defaultCodexTPMRetryAfter); err != nil {
		code := "canceled"
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			code = "deadline_exceeded"
		}
		return false, retryContext, finalizeStatus(state.StepResult{
			Index:  stepIndex,
			ID:     step.ID,
			Type:   step.ExecutorType(),
			Status: state.StepStatusFailed,
			Error: &state.Failure{
				Code:    code,
				Message: fmt.Sprintf("step %d canceled while waiting to retry after rate limit", stepIndex),
			},
		})
	}

	*retryCount++
	return true, retryContext, state.StepResult{}
}

func parsePositiveNumber(value any) (float64, bool) {
	switch typed := value.(type) {
	case int:
		if typed > 0 {
			return float64(typed), true
		}
	case int64:
		if typed > 0 {
			return float64(typed), true
		}
	case uint:
		if typed > 0 {
			return float64(typed), true
		}
	case uint64:
		if typed > 0 {
			return float64(typed), true
		}
	case float64:
		if typed > 0 {
			return typed, true
		}
	case string:
		parsed, parseErr := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if parseErr == nil && parsed > 0 {
			return parsed, true
		}
	}
	return 0, false
}

func parseNonNegativeInt(value any) (int, bool) {
	maxInt := int64(^uint(0) >> 1)
	switch typed := value.(type) {
	case int:
		if typed >= 0 {
			return typed, true
		}
	case int64:
		if typed >= 0 && typed <= maxInt {
			return int(typed), true
		}
	case uint:
		if typed <= uint(maxInt) {
			return int(typed), true
		}
	case uint64:
		if typed <= uint64(maxInt) {
			return int(typed), true
		}
	case float64:
		if typed >= 0 && typed <= float64(maxInt) && math.Trunc(typed) == typed {
			return int(typed), true
		}
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return 0, false
		}
		if parsed, err := strconv.ParseInt(text, 10, 64); err == nil && parsed >= 0 && parsed <= maxInt {
			return int(parsed), true
		}
		if parsed, err := strconv.ParseFloat(text, 64); err == nil && parsed >= 0 && parsed <= float64(maxInt) && math.Trunc(parsed) == parsed {
			return int(parsed), true
		}
	}
	return 0, false
}

func isRateLimitStepFailure(result state.StepResult) bool {
	providerError, ok := providerErrorDetails(result)
	if !ok {
		return false
	}

	for _, key := range []string{"code", "type", "message"} {
		value, _ := providerError[key].(string)
		if isRateLimitText(value) {
			return true
		}
	}
	return false
}

func rateLimitContinuationSessionID(result state.StepResult) (string, bool) {
	providerError, ok := providerErrorDetails(result)
	if !ok || !isRateLimitStepFailure(result) {
		return "", false
	}
	for _, key := range []string{"session_id", "thread_id"} {
		value, _ := providerError[key].(string)
		value = strings.TrimSpace(value)
		if value != "" {
			return value, true
		}
	}
	return "", true
}

func providerErrorDetails(result state.StepResult) (map[string]any, bool) {
	if result.Status != state.StepStatusFailed || result.Error == nil || len(result.Error.Details) == 0 {
		return nil, false
	}
	providerError, ok := result.Error.Details["provider_error"].(map[string]any)
	if !ok {
		return nil, false
	}
	return providerError, true
}

func isRateLimitText(value string) bool {
	text := strings.ToLower(strings.TrimSpace(value))
	if text == "" {
		return false
	}
	return strings.Contains(text, "429") ||
		strings.Contains(text, "too many requests") ||
		strings.Contains(text, "rate_limit") ||
		strings.Contains(text, "ratelimit")
}

func waitWithContext(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return nil
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type stepActivityProbe struct {
	paths []string
	last  map[string]fileStamp
}

type fileStamp struct {
	Exists  bool
	Size    int64
	ModUnix int64
}

func newStepActivityProbe(stepCtx executorapi.StepContext) *stepActivityProbe {
	stepDir := executorapi.StepArtifactDir(stepCtx.RunDir, stepCtx.StepIndex, stepCtx.Step.ID, stepCtx.ExecutionLabel)
	return &stepActivityProbe{
		paths: []string{
			filepath.Join(stepDir, "stdout.txt"),
			filepath.Join(stepDir, "stderr.txt"),
		},
		last: map[string]fileStamp{},
	}
}

func (p *stepActivityProbe) ObserveChanged() bool {
	changed := false
	for _, path := range p.paths {
		current := readFileStamp(path)
		previous, ok := p.last[path]
		if !ok || previous != current {
			changed = true
			p.last[path] = current
		}
	}
	return changed
}

func readFileStamp(path string) fileStamp {
	info, err := os.Stat(path)
	if err != nil {
		return fileStamp{}
	}
	return fileStamp{
		Exists:  true,
		Size:    info.Size(),
		ModUnix: info.ModTime().UTC().UnixNano(),
	}
}

func stallProbeInterval(after time.Duration) time.Duration {
	switch {
	case after <= 0:
		return 250 * time.Millisecond
	case after <= 2*time.Second:
		interval := after / 10
		if interval < 10*time.Millisecond {
			return 10 * time.Millisecond
		}
		return interval
	default:
		interval := after / 6
		if interval > 5*time.Second {
			return 5 * time.Second
		}
		return interval
	}
}

func resetTimer(timer *time.Timer, duration time.Duration) {
	if duration <= 0 {
		duration = time.Millisecond
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(duration)
}

func (r *Runner) reportStallRerunProgress(
	reporter *progress.Reporter,
	config Config,
	snapshot state.Snapshot,
	flowName string,
	frameID string,
	stepIndex int,
	step spec.Step,
	previous *state.StepResult,
	bindings map[string]any,
	after time.Duration,
	attempt int,
) {
	if reporter == nil {
		return
	}

	lookup := snapshot.StepByRef
	nextAttempt := attempt + 1
	attemptStatus := fmt.Sprintf("RERUN %d/%s", nextAttempt, stallRerunAttemptsTotal)
	afterLabel := after.String()

	failedResult := state.StepResult{
		Index:  stepIndex,
		ID:     step.ID,
		Type:   step.ExecutorType(),
		Status: state.StepStatusFailed,
		Error: &state.Failure{
			Code:    "step_stalled",
			Message: fmt.Sprintf("step %d stalled after %s (attempt %d/%s)", stepIndex, afterLabel, attempt, stallRerunAttemptsTotal),
		},
	}
	failedStep := progressResultStep(config, flowName, frameID, step, previous, bindings, failedResult, lookup)
	failedStep.Descriptor = &progress.DescriptorData{
		PrimaryText:         fmt.Sprintf("stalled after %s", afterLabel),
		DetailText:          []string{attemptStatus + " scheduled"},
		FinalSummaryDetails: []string{fmt.Sprintf("stalled after %s", afterLabel)},
	}
	reporter.StepFinished(failedStep)

	rerunExecutionLabel := stepExecutionLabel(snapshot.LastSequence+1, nextAttempt)
	rerunStep := progressStep(config, flowName, frameID, stepIndex, step, rerunExecutionLabel, previous, bindings, lookup)
	if rerunStep.Descriptor == nil {
		rerunStep.Descriptor = &progress.DescriptorData{}
	}
	rerunStep.Descriptor.PrimaryText = strings.TrimSpace(strings.Join([]string{afterLabel, attemptStatus, rerunStep.Descriptor.PrimaryText}, " "))
	reporter.StepStarted(rerunStep)
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
