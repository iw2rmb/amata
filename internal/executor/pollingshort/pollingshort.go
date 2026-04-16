package pollingshort

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/iw2rmb/amata/internal/executor"
	exprruntime "github.com/iw2rmb/amata/internal/expr"
	"github.com/iw2rmb/amata/internal/state"
)

const (
	defaultRequestMethod         = "POST"
	defaultRequestTimeout        = "30s"
	defaultConfirmMethod         = "GET"
	defaultConfirmInterval       = "3s"
	defaultConfirmTimeout        = "20m"
	defaultConfirmRequestTimeout = "30s"
)

type Executor struct {
	httpClient  httpDoer
	now         func() time.Time
	wait        func(context.Context, time.Duration) error
	checkpoints checkpointStore
}

type requestConfig struct {
	URL     string
	Method  string
	Headers map[string]string
	Body    any
	Timeout time.Duration
}

type confirmConfig struct {
	URLRaw         any
	MethodRaw      any
	HeadersRaw     any
	Interval       time.Duration
	Timeout        time.Duration
	RequestTimeout time.Duration
}

func New() executor.Executor {
	return &Executor{
		httpClient:  &http.Client{},
		now:         time.Now,
		wait:        waitWithContext,
		checkpoints: fileCheckpointStore{},
	}
}

func (e *Executor) Execute(ctx context.Context, stepCtx executor.StepContext) state.StepResult {
	e.ensureDefaults()
	key := checkpointKey(stepCtx)

	poll, found, err := e.checkpoints.Load(key)
	if err != nil {
		return invalidCheckpointFailure(stepCtx.StepIndex, err)
	}
	if found {
		if poll.Terminal != nil {
			return poll.Terminal.result()
		}
		return e.runConfirmLoop(ctx, stepCtx, key, poll)
	}

	request, err := resolveRequestConfig(stepCtx.Runtime, stepCtx.Step.Fields["request"])
	if err != nil {
		return executor.Failed("invalid_request", fmt.Sprintf("step %d request is invalid: %v", stepCtx.StepIndex, err))
	}

	requestResponse, err := e.performHTTPWithTimeout(ctx, request.Timeout, httpRequestSpec{
		URL:     request.URL,
		Method:  request.Method,
		Headers: request.Headers,
		Body:    request.Body,
	})
	if err != nil {
		return executor.Failed("request_failed", fmt.Sprintf("step %d request failed: %v", stepCtx.StepIndex, err))
	}
	if !statusIsSuccess(requestResponse.Status) {
		return executor.Failed("request_http_status", fmt.Sprintf("step %d request returned status %d", stepCtx.StepIndex, requestResponse.Status))
	}

	poll = pollState{
		Request: requestResponse,
		Confirm: confirmState{},
		// Confirm timeout budget starts after a successful request and continues across resume.
		ConfirmStarted: e.now(),
	}
	if err := e.checkpoints.Save(key, poll); err != nil {
		return invalidCheckpointFailure(stepCtx.StepIndex, err)
	}

	return e.runConfirmLoop(ctx, stepCtx, key, poll)
}

func (e *Executor) CleanupCheckpoint(_ context.Context, key executor.CheckpointKey) error {
	e.ensureDefaults()
	return e.checkpoints.Delete(key)
}

func (e *Executor) runConfirmLoop(ctx context.Context, stepCtx executor.StepContext, key executor.CheckpointKey, poll pollState) state.StepResult {
	confirm, err := resolveConfirmConfig(stepCtx.Runtime, stepCtx.Step.Fields["confirm"])
	if err != nil {
		return executor.Failed("invalid_confirm", fmt.Sprintf("step %d confirm is invalid: %v", stepCtx.StepIndex, err))
	}

	confirmDeadline := poll.ConfirmStarted.Add(confirm.Timeout)
	for {
		now := e.now()
		remaining := confirmDeadline.Sub(now)
		if remaining <= 0 {
			return executor.Failed("confirm_timeout", fmt.Sprintf("step %d confirm timeout exceeded", stepCtx.StepIndex))
		}

		if poll.Confirm.Attempts > 0 {
			nextAttempt := poll.Confirm.LastAttempt.Add(confirm.Interval)
			if now.Before(nextAttempt) {
				waitDuration := nextAttempt.Sub(now)
				if waitDuration > remaining {
					waitDuration = remaining
				}
				if err := e.wait(ctx, waitDuration); err != nil {
					return executor.Failed("confirm_failed", fmt.Sprintf("step %d confirm wait failed: %v", stepCtx.StepIndex, err))
				}
				continue
			}
		}

		attemptRuntime := stepCtx.Runtime.WithBindings(map[string]any{
			"value": poll.value(),
		})
		confirmRequest, err := resolveConfirmRequestConfig(attemptRuntime, confirm)
		if err != nil {
			return executor.Failed("invalid_confirm", fmt.Sprintf("step %d confirm is invalid: %v", stepCtx.StepIndex, err))
		}

		now = e.now()
		remaining = confirmDeadline.Sub(now)
		if remaining <= 0 {
			return executor.Failed("confirm_timeout", fmt.Sprintf("step %d confirm timeout exceeded", stepCtx.StepIndex))
		}

		requestTimeout := confirm.RequestTimeout
		if requestTimeout > remaining {
			requestTimeout = remaining
		}
		attemptNumber := poll.Confirm.Attempts + 1
		confirmResponse, err := e.performHTTPWithTimeout(ctx, requestTimeout, confirmRequest)
		if err != nil {
			return executor.Failed("confirm_failed", fmt.Sprintf("step %d confirm attempt %d failed: %v", stepCtx.StepIndex, attemptNumber, err))
		}
		if !statusIsSuccess(confirmResponse.Status) {
			return executor.Failed("confirm_http_status", fmt.Sprintf("step %d confirm attempt %d returned status %d", stepCtx.StepIndex, attemptNumber, confirmResponse.Status))
		}

		poll.Confirm.Attempts = attemptNumber
		poll.Confirm.Response = &confirmResponse
		poll.Confirm.LastAttempt = e.now()
		if err := e.checkpoints.Save(key, poll); err != nil {
			return invalidCheckpointFailure(stepCtx.StepIndex, err)
		}

		evalRuntime := stepCtx.Runtime.WithBindings(map[string]any{
			"value": poll.value(),
		})
		done, failure := resolveCondition(stepCtx.StepIndex, evalRuntime, stepCtx.Step.Fields["done_when"], "done_when", "invalid_done_when")
		if failure != nil {
			return *failure
		}
		success, failure := resolveCondition(stepCtx.StepIndex, evalRuntime, stepCtx.Step.Fields["success_when"], "success_when", "invalid_success_when")
		if failure != nil {
			return *failure
		}
		if !done {
			continue
		}

		if success {
			result := executor.Succeeded(poll.value())
			poll.Terminal = terminalFromResult(result)
			if err := e.checkpoints.Save(key, poll); err != nil {
				return invalidCheckpointFailure(stepCtx.StepIndex, err)
			}
			return result
		}

		result := executor.NormalizeResult(state.StepResult{
			Status: state.StepStatusFailed,
			Value:  poll.value(),
			Error: &state.Failure{
				Code:    "polling_unsuccessful",
				Message: fmt.Sprintf("step %d polling completed unsuccessfully", stepCtx.StepIndex),
			},
			Artifacts: executor.EmptyArtifacts(),
		})
		poll.Terminal = terminalFromResult(result)
		if err := e.checkpoints.Save(key, poll); err != nil {
			return invalidCheckpointFailure(stepCtx.StepIndex, err)
		}
		return result
	}
}

func (e *Executor) performHTTPWithTimeout(ctx context.Context, timeout time.Duration, request httpRequestSpec) (httpResponse, error) {
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return performHTTPRequest(requestCtx, e.httpClient, request)
}

func (e *Executor) ensureDefaults() {
	if e.httpClient == nil {
		e.httpClient = &http.Client{}
	}
	if e.now == nil {
		e.now = time.Now
	}
	if e.wait == nil {
		e.wait = waitWithContext
	}
	if e.checkpoints == nil {
		e.checkpoints = fileCheckpointStore{}
	}
}

func checkpointKey(stepCtx executor.StepContext) executor.CheckpointKey {
	return executor.CheckpointKey{
		RunDir:    stepCtx.RunDir,
		FrameID:   stepCtx.FrameID,
		StepIndex: stepCtx.StepIndex,
	}
}

func resolveRequestConfig(runtime exprruntime.Runtime, raw any) (requestConfig, error) {
	fields, err := requireObject(raw, "request")
	if err != nil {
		return requestConfig{}, err
	}

	urlRaw, ok := fields["url"]
	if !ok {
		return requestConfig{}, fmt.Errorf("request.url is required")
	}
	url, err := resolveNonEmptyString(runtime, urlRaw, "request.url")
	if err != nil {
		return requestConfig{}, err
	}

	method, err := resolveNonEmptyString(runtime, valueOrDefault(fields, "method", defaultRequestMethod), "request.method")
	if err != nil {
		return requestConfig{}, err
	}
	headers, err := resolveHeaders(runtime, valueOrDefault(fields, "headers", map[string]any{}), "request.headers")
	if err != nil {
		return requestConfig{}, err
	}
	body, err := resolveBody(runtime, fields)
	if err != nil {
		return requestConfig{}, err
	}
	timeout, err := resolveDuration(runtime, valueOrDefault(fields, "timeout", defaultRequestTimeout), "request.timeout")
	if err != nil {
		return requestConfig{}, err
	}

	return requestConfig{
		URL:     url,
		Method:  method,
		Headers: headers,
		Body:    body,
		Timeout: timeout,
	}, nil
}

func resolveConfirmConfig(runtime exprruntime.Runtime, raw any) (confirmConfig, error) {
	fields, err := requireObject(raw, "confirm")
	if err != nil {
		return confirmConfig{}, err
	}

	urlRaw, ok := fields["url"]
	if !ok {
		return confirmConfig{}, fmt.Errorf("confirm.url is required")
	}
	interval, err := resolveDuration(runtime, valueOrDefault(fields, "interval", defaultConfirmInterval), "confirm.interval")
	if err != nil {
		return confirmConfig{}, err
	}
	timeout, err := resolveDuration(runtime, valueOrDefault(fields, "timeout", defaultConfirmTimeout), "confirm.timeout")
	if err != nil {
		return confirmConfig{}, err
	}
	requestTimeout, err := resolveDuration(runtime, valueOrDefault(fields, "request_timeout", defaultConfirmRequestTimeout), "confirm.request_timeout")
	if err != nil {
		return confirmConfig{}, err
	}

	return confirmConfig{
		URLRaw:         urlRaw,
		MethodRaw:      valueOrDefault(fields, "method", defaultConfirmMethod),
		HeadersRaw:     valueOrDefault(fields, "headers", map[string]any{}),
		Interval:       interval,
		Timeout:        timeout,
		RequestTimeout: requestTimeout,
	}, nil
}

func resolveConfirmRequestConfig(runtime exprruntime.Runtime, confirm confirmConfig) (httpRequestSpec, error) {
	url, err := resolveNonEmptyString(runtime, confirm.URLRaw, "confirm.url")
	if err != nil {
		return httpRequestSpec{}, err
	}
	method, err := resolveNonEmptyString(runtime, confirm.MethodRaw, "confirm.method")
	if err != nil {
		return httpRequestSpec{}, err
	}
	headers, err := resolveHeaders(runtime, confirm.HeadersRaw, "confirm.headers")
	if err != nil {
		return httpRequestSpec{}, err
	}

	return httpRequestSpec{
		URL:     url,
		Method:  method,
		Headers: headers,
	}, nil
}

func requireObject(raw any, field string) (map[string]any, error) {
	if raw == nil {
		return nil, fmt.Errorf("%s is required", field)
	}
	object, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an object", field)
	}
	return object, nil
}

func valueOrDefault(fields map[string]any, key string, fallback any) any {
	value, ok := fields[key]
	if !ok {
		return fallback
	}
	return value
}

func resolveBody(runtime exprruntime.Runtime, fields map[string]any) (any, error) {
	bodyRaw, ok := fields["body"]
	if !ok {
		return nil, nil
	}
	body, err := runtime.Resolve(bodyRaw)
	if err != nil {
		return nil, fmt.Errorf("request.body is invalid: %w", err)
	}
	return body, nil
}

func resolveNonEmptyString(runtime exprruntime.Runtime, raw any, field string) (string, error) {
	resolved, err := runtime.Resolve(raw)
	if err != nil {
		return "", fmt.Errorf("%s is invalid: %w", field, err)
	}
	text, ok := resolved.(string)
	if !ok {
		return "", fmt.Errorf("%s must resolve to a string", field)
	}
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("%s must not be empty", field)
	}
	return text, nil
}

func resolveHeaders(runtime exprruntime.Runtime, raw any, field string) (map[string]string, error) {
	resolved, err := runtime.Resolve(raw)
	if err != nil {
		return nil, fmt.Errorf("%s is invalid: %w", field, err)
	}
	if resolved == nil {
		return map[string]string{}, nil
	}

	headers, ok := resolved.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must resolve to a map", field)
	}

	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	resolvedHeaders := make(map[string]string, len(headers))
	for _, key := range keys {
		value, ok := headers[key].(string)
		if !ok {
			return nil, fmt.Errorf("%s[%q] must resolve to a string", field, key)
		}
		resolvedHeaders[key] = value
	}

	return resolvedHeaders, nil
}

func resolveDuration(runtime exprruntime.Runtime, raw any, field string) (time.Duration, error) {
	resolved, err := runtime.Resolve(raw)
	if err != nil {
		return 0, fmt.Errorf("%s is invalid: %w", field, err)
	}

	text, ok := resolved.(string)
	if !ok {
		return 0, fmt.Errorf("%s must resolve to a duration string", field)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, fmt.Errorf("%s must not be empty", field)
	}

	duration, err := time.ParseDuration(text)
	if err != nil {
		return 0, fmt.Errorf("%s must be a valid duration", field)
	}
	if duration <= 0 {
		return 0, fmt.Errorf("%s must be greater than zero", field)
	}

	return duration, nil
}

func resolveCondition(stepIndex int, runtime exprruntime.Runtime, raw any, field string, code string) (bool, *state.StepResult) {
	if raw == nil {
		result := executor.Failed(code, fmt.Sprintf("step %d %s is invalid: %s is required", stepIndex, field, field))
		return false, &result
	}

	resolved, err := runtime.Resolve(raw)
	if err != nil {
		result := executor.Failed(code, fmt.Sprintf("step %d %s is invalid: %v", stepIndex, field, err))
		return false, &result
	}

	value, ok := resolved.(bool)
	if !ok {
		result := executor.Failed(code, fmt.Sprintf("step %d %s must be a boolean", stepIndex, field))
		return false, &result
	}

	return value, nil
}

func invalidCheckpointFailure(stepIndex int, err error) state.StepResult {
	return executor.Failed("invalid_checkpoint", fmt.Sprintf("step %d checkpoint is invalid: %v", stepIndex, err))
}

func statusIsSuccess(status int) bool {
	return status >= 200 && status <= 299
}

func waitWithContext(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return nil
	}

	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
