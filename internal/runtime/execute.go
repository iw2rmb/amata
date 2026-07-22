package runtime

import (
	"context"
	"encoding/json"
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
	"github.com/iw2rmb/amata/internal/schema"
	"github.com/iw2rmb/amata/internal/spec"
	"github.com/iw2rmb/amata/internal/state"
)

const (
	defaultStallAfter          = 15 * time.Minute
	stallCancellationGraceWait = 1 * time.Second
	stallCallReturnType        = "stall.call"
	stallRerunAttemptsTotal    = "INF"
	defaultProviderRetryAfter  = 1 * time.Minute
	defaultCodexResumePrompt   = "continue"
	defaultStructuredAttempts  = 3
	defaultStructuredPrompt    = "Your previous response did not satisfy the required response schema.\nRespond only with a JSON value that matches the required schema.\nDo not include prose, markdown, or commentary."
	continuationMetadataKey    = "continuation_session_id"
	highDemandProviderMessage  = "we're currently experiencing high demand, which may cause temporary errors."
)

type stallPolicy struct {
	After  time.Duration
	Action string
	Flow   string
}

type codexProviderRetryPolicy struct {
	Enabled    bool
	MaxRetries int
}

type retryContext struct {
	ContinuationSessionID string
	ContinuationPrompt    string
	UsedFreshFallback     bool
}

type structuredRetryPolicy struct {
	Enabled        bool
	MaxAttempts    int
	Prompt         string
	PromptOverride bool
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

	providerRetryPolicy, failure := resolveCodexProviderRetryPolicy(runtime, config.Spec.Defaults, stepIndex, step)
	if failure != nil {
		result.Status = state.StepStatusFailed
		result.Error = failure
		return stepAction{}, finalizeStatus(result), nil
	}
	structuredPolicy, failure := resolveStructuredRetryPolicy(runtime, config.Spec.Defaults, stepIndex, step)
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

	providerRetryCount := 0
	retryContext := retryContext{}

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
			ContinuationPrompt:    retryContext.ContinuationPrompt,
		}

		if policy == nil {
			result = executeStepAttempt(ctx, stepExecutor, stepCtx, step)
			finalized := r.finalizeStepResult(config, responses, snapshot.StepByRef, previous, bindings, step, result)
			if retry, nextRetryContext, aborted := r.maybeRetryCodexProviderFailure(ctx, stepIndex, step, finalized, providerRetryPolicy, &providerRetryCount, retryContext); aborted.Status != "" {
				return stepAction{}, aborted, nil
			} else if retry {
				retryContext = nextRetryContext
				continue attemptLoop
			}
			if retry, nextRetryContext := r.maybeRetryStructuredOutput(config.SpecPath, config.Spec.Schemas, step, finalized, structuredPolicy, attempt); retry {
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
				if retry, nextRetryContext, aborted := r.maybeRetryCodexProviderFailure(ctx, stepIndex, step, finalized, providerRetryPolicy, &providerRetryCount, retryContext); aborted.Status != "" {
					return stepAction{}, aborted, nil
				} else if retry {
					retryContext = nextRetryContext
					continue attemptLoop
				}
				if retry, nextRetryContext := r.maybeRetryStructuredOutput(config.SpecPath, config.Spec.Schemas, step, finalized, structuredPolicy, attempt); retry {
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

func resolveCodexProviderRetryPolicy(runtime exprruntime.Runtime, defaults map[string]any, stepIndex int, step spec.Step) (codexProviderRetryPolicy, *state.Failure) {
	if step.ExecutorType() != "codex" {
		return codexProviderRetryPolicy{}, nil
	}

	raw, ok := defaults["tpm"]
	if !ok {
		return codexProviderRetryPolicy{}, nil
	}

	resolved, err := runtime.Resolve(raw)
	if err != nil {
		return codexProviderRetryPolicy{}, &state.Failure{
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
				return codexProviderRetryPolicy{}, &state.Failure{
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
				return codexProviderRetryPolicy{}, &state.Failure{
					Code:    "invalid_defaults",
					Message: fmt.Sprintf("step %d defaults.tpm is invalid: retries must resolve to a non-negative integer", stepIndex),
				}
			}
			retries = parsedRetries
		}
		if !rateSpecified && !retriesSpecified {
			return codexProviderRetryPolicy{}, &state.Failure{
				Code:    "invalid_defaults",
				Message: fmt.Sprintf("step %d defaults.tpm is invalid: rate or retries is required", stepIndex),
			}
		}
	default:
		if _, ok := parsePositiveNumber(resolved); !ok {
			return codexProviderRetryPolicy{}, &state.Failure{
				Code:    "invalid_defaults",
				Message: fmt.Sprintf("step %d defaults.tpm is invalid: must resolve to a positive number", stepIndex),
			}
		}
	}

	return codexProviderRetryPolicy{
		Enabled:    true,
		MaxRetries: retries,
	}, nil
}

func (r *Runner) maybeRetryCodexProviderFailure(
	ctx context.Context,
	stepIndex int,
	step spec.Step,
	result state.StepResult,
	policy codexProviderRetryPolicy,
	retryCount *int,
	retryContext retryContext,
) (bool, retryContext, state.StepResult) {
	if step.ExecutorType() != "codex" || !policy.Enabled || retryCount == nil || *retryCount >= policy.MaxRetries {
		return false, retryContext, state.StepResult{}
	}
	sessionID, retryable := providerRetryContinuationSessionID(result)
	if !retryable {
		return false, retryContext, state.StepResult{}
	}
	if sessionID == "" {
		if retryContext.UsedFreshFallback {
			return false, retryContext, state.StepResult{}
		}
		retryContext.UsedFreshFallback = true
		retryContext.ContinuationSessionID = ""
		retryContext.ContinuationPrompt = ""
	} else {
		retryContext.ContinuationSessionID = sessionID
		retryContext.ContinuationPrompt = defaultCodexResumePrompt
	}

	waitFn := r.retryWait
	if waitFn == nil {
		waitFn = waitWithContext
	}
	if err := waitFn(ctx, defaultProviderRetryAfter); err != nil {
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
				Message: fmt.Sprintf("step %d canceled while waiting to retry after transient provider failure", stepIndex),
			},
		})
	}

	*retryCount++
	return true, retryContext, state.StepResult{}
}

func resolveStructuredRetryPolicy(runtime exprruntime.Runtime, defaults map[string]any, stepIndex int, step spec.Step) (structuredRetryPolicy, *state.Failure) {
	if !isAgentExecutor(step.ExecutorType()) {
		return structuredRetryPolicy{}, nil
	}

	policy := structuredRetryPolicy{
		Enabled:     agentValueSchemaResponse(step),
		MaxAttempts: defaultStructuredAttempts,
	}

	raw, ok, err := rawExecutorDefault(defaults, step.ExecutorType(), "structured_retry")
	if err != nil {
		return structuredRetryPolicy{}, &state.Failure{
			Code:    "invalid_defaults",
			Message: fmt.Sprintf("step %d defaults.executors.%s.structured_retry is invalid: %v", stepIndex, step.ExecutorType(), err),
		}
	}
	if !ok {
		return policy, nil
	}

	fields, ok := raw.(map[string]any)
	if !ok {
		return structuredRetryPolicy{}, &state.Failure{
			Code:    "invalid_defaults",
			Message: fmt.Sprintf("step %d defaults.executors.%s.structured_retry is invalid: must be a map", stepIndex, step.ExecutorType()),
		}
	}

	if attemptsRaw, ok := fields["attempts"]; ok {
		resolved, err := runtime.Resolve(attemptsRaw)
		if err != nil {
			return structuredRetryPolicy{}, &state.Failure{
				Code:    "invalid_defaults",
				Message: fmt.Sprintf("step %d defaults.executors.%s.structured_retry is invalid: resolve attempts: %v", stepIndex, step.ExecutorType(), err),
			}
		}
		attempts, ok := parsePositiveInt(resolved)
		if !ok {
			return structuredRetryPolicy{}, &state.Failure{
				Code:    "invalid_defaults",
				Message: fmt.Sprintf("step %d defaults.executors.%s.structured_retry is invalid: attempts must resolve to an integer greater than or equal to 1", stepIndex, step.ExecutorType()),
			}
		}
		policy.MaxAttempts = attempts
	}

	if promptRaw, ok := fields["prompt"]; ok {
		prompt, err := runtime.ResolveString(promptRaw)
		if err != nil {
			return structuredRetryPolicy{}, &state.Failure{
				Code:    "invalid_defaults",
				Message: fmt.Sprintf("step %d defaults.executors.%s.structured_retry is invalid: prompt must resolve to a non-empty string: %v", stepIndex, step.ExecutorType(), err),
			}
		}
		policy.Prompt = prompt
		policy.PromptOverride = true
	}

	return policy, nil
}

func (r *Runner) maybeRetryStructuredOutput(specPath string, workflowSchemas map[string]any, step spec.Step, result state.StepResult, policy structuredRetryPolicy, attempt int) (bool, retryContext) {
	if !policy.Enabled || attempt >= policy.MaxAttempts || !isStructuredOutputFailure(result) {
		return false, retryContext{}
	}

	prompt := policy.Prompt
	if !policy.PromptOverride {
		prompt = defaultStructuredRetryPrompt(specPath, workflowSchemas, step)
	}

	return true, retryContext{
		ContinuationSessionID: structuredRetrySessionID(step.ExecutorType(), result),
		ContinuationPrompt:    prompt,
	}
}

func defaultStructuredRetryPrompt(specPath string, workflowSchemas map[string]any, step spec.Step) string {
	schemaJSON, ok := structuredRetrySchemaJSON(specPath, workflowSchemas, step)
	if !ok {
		return defaultStructuredPrompt
	}
	return defaultStructuredPrompt + "\n\nRequired JSON Schema:\n" + schemaJSON
}

func structuredRetrySchemaJSON(specPath string, workflowSchemas map[string]any, step spec.Step) (string, bool) {
	cfg, ok, err := loadResponseConfig(step)
	if err != nil || !ok || cfg.schema == nil || cfg.from.kind != "value" {
		return "", false
	}

	document, err := structuredRetrySchemaDocument(specPath, workflowSchemas, step, cfg.schema)
	if err != nil {
		return "", false
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return "", false
	}
	return string(data), true
}

func structuredRetrySchemaDocument(specPath string, workflowSchemas map[string]any, step spec.Step, rawSchema any) (any, error) {
	if sourcePath, ok, err := schema.ResolveResponseSchemaPath(rawSchema, specPath); err != nil {
		return nil, err
	} else if ok {
		document, _, err := schema.LoadResponseSchemaFile(sourcePath)
		if err != nil {
			return nil, err
		}
		return structuredRetryProviderDocument(step, document)
	}

	document, err := schema.ExpandedDocument(rawSchema, workflowSchemas)
	if err != nil {
		return nil, err
	}
	return structuredRetryProviderDocument(step, document)
}

func structuredRetryProviderDocument(step spec.Step, document any) (any, error) {
	if step.ExecutorType() != "codex" {
		return document, nil
	}
	validated, err := schema.ValidateProviderDocument(document)
	if err != nil {
		return nil, err
	}
	return validated, nil
}

func isStructuredOutputFailure(result state.StepResult) bool {
	if result.Status != state.StepStatusFailed || result.Error == nil {
		return false
	}
	return result.Error.Code == "invalid_provider_output" || result.Error.Code == responseCodeSchemaMismatch
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

func parsePositiveInt(value any) (int, bool) {
	parsed, ok := parseNonNegativeInt(value)
	if !ok || parsed < 1 {
		return 0, false
	}
	return parsed, true
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

func rawExecutorDefault(defaults map[string]any, executorType string, key string) (any, bool, error) {
	rawExecutors, ok := defaults["executors"]
	if !ok {
		return nil, false, nil
	}
	executors, ok := rawExecutors.(map[string]any)
	if !ok {
		return nil, false, fmt.Errorf("defaults.executors must be a map")
	}

	rawExecutorDefaults, ok := executors[executorType]
	if !ok {
		return nil, false, nil
	}
	executorDefaults, ok := rawExecutorDefaults.(map[string]any)
	if !ok {
		return nil, false, fmt.Errorf("defaults.executors.%s must be a map", executorType)
	}

	raw, ok := executorDefaults[key]
	return raw, ok, nil
}

func isAgentExecutor(executorType string) bool {
	switch executorType {
	case "codex", "claude", "crush":
		return true
	default:
		return false
	}
}

func agentValueSchemaResponse(step spec.Step) bool {
	if !isAgentExecutor(step.ExecutorType()) {
		return false
	}

	cfg, ok, err := loadResponseConfig(step)
	if err != nil || !ok || cfg.schema == nil {
		return false
	}
	return cfg.from.kind == "value"
}

func structuredRetrySessionID(executorType string, result state.StepResult) string {
	switch executorType {
	case "codex", "claude":
	default:
		return ""
	}

	if id := continuationSessionIDFromMetadata(result); id != "" {
		return id
	}
	if providerError, ok := providerErrorDetails(result); ok {
		for _, key := range []string{"session_id", "thread_id"} {
			value, _ := providerError[key].(string)
			if strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		}
	}
	return ""
}

func continuationSessionIDFromMetadata(result state.StepResult) string {
	if len(result.Artifacts.Files) == 0 {
		return ""
	}
	path := result.Artifacts.Files["metadata"]
	if path == "" {
		return ""
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var metadata map[string]any
	if err := json.Unmarshal(data, &metadata); err != nil {
		return ""
	}
	value, _ := metadata[continuationMetadataKey].(string)
	return strings.TrimSpace(value)
}

func isRetryableProviderStepFailure(result state.StepResult) bool {
	providerError, ok := providerErrorDetails(result)
	if !ok {
		return false
	}

	for _, key := range []string{"code", "type", "message"} {
		value, _ := providerError[key].(string)
		if isRetryableProviderErrorText(value) {
			return true
		}
	}
	return false
}

func providerRetryContinuationSessionID(result state.StepResult) (string, bool) {
	providerError, ok := providerErrorDetails(result)
	if !ok || !isRetryableProviderStepFailure(result) {
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

func isRetryableProviderErrorText(value string) bool {
	text := strings.ToLower(strings.TrimSpace(value))
	if text == "" {
		return false
	}
	return strings.Contains(text, "429") ||
		strings.Contains(text, "too many requests") ||
		strings.Contains(text, "rate_limit") ||
		strings.Contains(text, "ratelimit") ||
		strings.Contains(text, highDemandProviderMessage)
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
