package runtime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	executorapi "github.com/iw2rmb/amata/internal/executor"
	"github.com/iw2rmb/amata/internal/progress"
	"github.com/iw2rmb/amata/internal/spec"
	"github.com/iw2rmb/amata/internal/state"
)

func TestRunnerStallPolicies(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name            string
		defaults        map[string]any
		stepFields      map[string]any
		wantSuccess     bool
		wantFailureCode string
		wantAttempts    int
	}{
		{
			name:         "rerun retries step with same inputs",
			stepFields:   map[string]any{"stall": map[string]any{"after": "10ms", "type": "rerun"}},
			wantSuccess:  true,
			wantAttempts: 2,
		},
		{
			name: "executor defaults when step stall missing",
			defaults: map[string]any{
				"executors": map[string]any{
					"fake": map[string]any{"stall": map[string]any{"after": "10ms", "type": "error"}},
				},
			},
			wantFailureCode: "step_stalled",
			wantAttempts:    1,
		},
		{
			name: "step stall overrides executor default",
			defaults: map[string]any{
				"executors": map[string]any{
					"fake": map[string]any{"stall": map[string]any{"after": "10ms", "type": "error"}},
				},
			},
			stepFields:   map[string]any{"stall": map[string]any{"after": "10ms", "type": "rerun"}},
			wantSuccess:  true,
			wantAttempts: 2,
		},
		{
			name:            "error fails step",
			stepFields:      map[string]any{"stall": map[string]any{"after": "10ms", "type": "error"}},
			wantFailureCode: "step_stalled",
			wantAttempts:    1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			step := spec.Step{ID: "blocked", Type: "fake", Fields: tc.stepFields}
			config := testConfig(t, spec.Document{
				Version:  spec.Version,
				Name:     "sample",
				Entry:    "main",
				Defaults: tc.defaults,
				Flows: map[string]spec.Flow{
					"main": {Steps: []spec.Step{step}},
				},
			})
			mustPersist(t, config)

			attempts := 0
			registry := builtinRegistry()
			if err := registry.Register("fake", func() executorapi.Executor {
				return &fakeExecutor{
					calls: new([]string),
					executeWithContext: func(execCtx context.Context, _ executorapi.StepContext) state.StepResult {
						attempts++
						if attempts == 1 {
							<-execCtx.Done()
							return state.StepResult{
								Status: state.StepStatusFailed,
								Error:  &state.Failure{Code: "canceled", Message: "canceled"},
							}
						}
						return state.StepResult{Status: state.StepStatusSucceeded, Value: "ok"}
					},
				}
			}); err != nil {
				t.Fatalf("register fake executor: %v", err)
			}

			snapshot, err := NewRunner(registry).Run(context.Background(), config)
			if tc.wantSuccess {
				if err != nil {
					t.Fatalf("run: %v", err)
				}
				if got := snapshot.Steps[0].Value; got != "ok" {
					t.Fatalf("step value = %#v, want ok", got)
				}
			} else {
				assertRunFailed(t, err, tc.wantFailureCode)
			}
			if attempts != tc.wantAttempts {
				t.Fatalf("attempts = %d, want %d", attempts, tc.wantAttempts)
			}
		})
	}
}

func TestRunnerStallErrorFailsFastWhenExecutorIgnoresCancellation(t *testing.T) {
	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Flows: map[string]spec.Flow{
			"main": {
				Steps: []spec.Step{
					{
						ID:   "blocked",
						Type: "fake",
						Fields: map[string]any{
							"stall": map[string]any{
								"after": "10ms",
								"type":  "error",
							},
						},
					},
				},
			},
		},
	})

	mustPersist(t, config)

	registry := builtinRegistry()
	if err := registry.Register("fake", func() executorapi.Executor {
		return &fakeExecutor{
			calls: new([]string),
			executeWithContext: func(context.Context, executorapi.StepContext) state.StepResult {
				time.Sleep(3 * time.Second)
				return state.StepResult{Status: state.StepStatusSucceeded}
			},
		}
	}); err != nil {
		t.Fatalf("register fake executor: %v", err)
	}

	started := time.Now()
	_, err := NewRunner(registry).Run(context.Background(), config)
	elapsed := time.Since(started)

	assertRunFailed(t, err, "step_stalled")
	if elapsed >= 2500*time.Millisecond {
		t.Fatalf("run elapsed = %s, want fast stall failure before 2.5s", elapsed)
	}
}

func TestRunnerContextDeadlineFailsFastWhenExecutorIgnoresCancellation(t *testing.T) {
	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Defaults: map[string]any{
			"executors": map[string]any{
				"fake": map[string]any{
					"stall": map[string]any{
						"after": "1m",
						"type":  "error",
					},
				},
			},
		},
		Flows: map[string]spec.Flow{
			"main": {
				Steps: []spec.Step{
					{
						ID:   "blocked",
						Type: "fake",
					},
				},
			},
		},
	})

	mustPersist(t, config)

	registry := builtinRegistry()
	if err := registry.Register("fake", func() executorapi.Executor {
		return &fakeExecutor{
			calls: new([]string),
			executeWithContext: func(context.Context, executorapi.StepContext) state.StepResult {
				time.Sleep(3 * time.Second)
				return state.StepResult{Status: state.StepStatusSucceeded}
			},
		}
	}); err != nil {
		t.Fatalf("register fake executor: %v", err)
	}

	runCtx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	started := time.Now()
	_, err := NewRunner(registry).Run(runCtx, config)
	elapsed := time.Since(started)

	assertRunFailed(t, err, "deadline_exceeded")
	if elapsed >= 2500*time.Millisecond {
		t.Fatalf("run elapsed = %s, want deadline failure before 2.5s", elapsed)
	}
}

func TestRunnerStallCallUsesFallbackFlowResult(t *testing.T) {
	t.Parallel()

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Flows: map[string]spec.Flow{
			"main": {
				Steps: []spec.Step{
					{
						ID:   "blocked",
						Type: "fake",
						Fields: map[string]any{
							"stall": map[string]any{
								"after": "10ms",
								"type":  "call",
								"flow":  "recovery",
							},
						},
					},
					{
						ID: "after",
						Fields: map[string]any{
							"expr": "$.prev.value",
						},
					},
				},
			},
			"recovery": {
				Steps: []spec.Step{
					{ID: "recover", Type: "fake"},
				},
			},
		},
	})

	mustPersist(t, config)

	registry := builtinRegistry()
	if err := registry.Register("fake", func() executorapi.Executor {
		return &fakeExecutor{
			calls: new([]string),
			executeWithContext: func(execCtx context.Context, ctx executorapi.StepContext) state.StepResult {
				if ctx.Step.ID == "blocked" {
					<-execCtx.Done()
					return state.StepResult{
						Status: state.StepStatusFailed,
						Error: &state.Failure{
							Code:    "canceled",
							Message: "canceled",
						},
					}
				}
				return state.StepResult{
					Status: state.StepStatusSucceeded,
					Value:  "recovered",
				}
			},
		}
	}); err != nil {
		t.Fatalf("register fake executor: %v", err)
	}

	snapshot, err := NewRunner(registry).Run(context.Background(), config)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := snapshot.Steps[0].Type; got != "fake" {
		t.Fatalf("stalled step type = %q, want fake", got)
	}
	if got := snapshot.Steps[0].Value; got != "recovered" {
		t.Fatalf("stalled step value = %#v, want recovered", got)
	}
	if got := snapshot.Steps[1].Value; got != "recovered" {
		t.Fatalf("after value = %#v, want recovered", got)
	}
}

func TestRunnerStallRerunEmitsFailedAndRestartedLiveProgress(t *testing.T) {
	t.Parallel()

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Flows: map[string]spec.Flow{
			"main": {
				Steps: []spec.Step{
					{
						ID:   "blocked",
						Type: "fake",
						Fields: map[string]any{
							"stall": map[string]any{
								"after": "10ms",
								"type":  "rerun",
							},
						},
					},
				},
			},
		},
	})
	mustPersist(t, config)

	attempts := 0
	registry := builtinRegistry()
	if err := registry.Register("fake", func() executorapi.Executor {
		return &fakeExecutor{
			calls: new([]string),
			executeWithContext: func(execCtx context.Context, _ executorapi.StepContext) state.StepResult {
				attempts++
				if attempts == 1 {
					<-execCtx.Done()
					return state.StepResult{
						Status: state.StepStatusFailed,
						Error: &state.Failure{
							Code:    "canceled",
							Message: "canceled",
						},
					}
				}
				return state.StepResult{
					Status: state.StepStatusSucceeded,
					Value:  "ok",
				}
			},
		}
	}); err != nil {
		t.Fatalf("register fake executor: %v", err)
	}

	var events []progress.Event
	sink := progress.SinkFunc(func(event progress.Event) {
		events = append(events, event)
	})

	snapshot, err := NewRunner(registry, WithRunnerProgressSink(sink)).Run(context.Background(), config)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if snapshot.Status != state.RunStatusSucceeded {
		t.Fatalf("snapshot status = %q, want succeeded", snapshot.Status)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}

	assertProgressKindsAndSteps(t, events,
		[]progress.EventKind{
			progress.EventRunStarted,
			progress.EventStepStarted,
			progress.EventStepFinished,
			progress.EventStepStarted,
			progress.EventStepFinished,
			progress.EventRunFinished,
		},
		[]string{
			"",
			"blocked",
			"blocked",
			"blocked",
			"blocked",
			"",
		},
	)

	failedAttempt := events[2].Step
	if failedAttempt == nil || failedAttempt.Status != progress.StepStatusFailed {
		t.Fatalf("failed rerun attempt event = %#v, want failed status", failedAttempt)
	}
	if failedAttempt.Error == nil || failedAttempt.Error.Code != "step_stalled" {
		t.Fatalf("failed rerun attempt error = %#v, want step_stalled", failedAttempt.Error)
	}

	rerunStart := events[3].Step
	if rerunStart == nil || rerunStart.Status != progress.StepStatusRunning {
		t.Fatalf("rerun start event = %#v, want running step", rerunStart)
	}
	if rerunStart.Descriptor == nil || !strings.Contains(rerunStart.Descriptor.PrimaryText, "RERUN 2/INF") {
		t.Fatalf("rerun descriptor = %#v, want RERUN 2/INF marker", rerunStart.Descriptor)
	}
}

func TestRunnerStallRerunDoesNotTriggerWhenStepKeepsEmittingOutput(t *testing.T) {
	t.Parallel()

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Flows: map[string]spec.Flow{
			"main": {
				Steps: []spec.Step{
					{
						ID:   "active",
						Type: "fake",
						Fields: map[string]any{
							"stall": map[string]any{
								"after": "25ms",
								"type":  "rerun",
							},
						},
					},
				},
			},
		},
	})
	mustPersist(t, config)

	attempts := 0
	registry := builtinRegistry()
	if err := registry.Register("fake", func() executorapi.Executor {
		return &fakeExecutor{
			calls: new([]string),
			executeWithContext: func(_ context.Context, ctx executorapi.StepContext) state.StepResult {
				attempts++

				stepDir := executorapi.StepArtifactDir(ctx.RunDir, ctx.StepIndex, ctx.Step.ID, ctx.ExecutionLabel)
				if err := os.MkdirAll(stepDir, 0o755); err != nil {
					return state.StepResult{
						Status: state.StepStatusFailed,
						Error:  &state.Failure{Code: "mkdir_failed", Message: err.Error()},
					}
				}
				stdoutPath := filepath.Join(stepDir, "stdout.txt")
				if err := os.WriteFile(stdoutPath, []byte(""), 0o644); err != nil {
					return state.StepResult{
						Status: state.StepStatusFailed,
						Error:  &state.Failure{Code: "create_stdout_failed", Message: err.Error()},
					}
				}
				for i := 0; i < 8; i++ {
					f, err := os.OpenFile(stdoutPath, os.O_APPEND|os.O_WRONLY, 0o644)
					if err != nil {
						return state.StepResult{
							Status: state.StepStatusFailed,
							Error:  &state.Failure{Code: "open_stdout_failed", Message: err.Error()},
						}
					}
					if _, err := f.WriteString("tick\n"); err != nil {
						_ = f.Close()
						return state.StepResult{
							Status: state.StepStatusFailed,
							Error:  &state.Failure{Code: "write_stdout_failed", Message: err.Error()},
						}
					}
					_ = f.Close()
					time.Sleep(10 * time.Millisecond)
				}

				return state.StepResult{
					Status: state.StepStatusSucceeded,
					Value:  "ok",
				}
			},
		}
	}); err != nil {
		t.Fatalf("register fake executor: %v", err)
	}

	snapshot, err := NewRunner(registry).Run(context.Background(), config)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if snapshot.Status != state.RunStatusSucceeded {
		t.Fatalf("snapshot status = %q, want succeeded", snapshot.Status)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestRunnerCodexTPMRetryOnRateLimit(t *testing.T) {
	t.Parallel()

	rateLimitFailure := func(requestID string, sessionID string) state.StepResult {
		providerError := map[string]any{
			"message": "exceeded retry limit, last status: 429 Too Many Requests, request id: " + requestID,
		}
		if sessionID != "" {
			providerError["session_id"] = sessionID
		}
		return state.StepResult{
			Status: state.StepStatusFailed,
			Error: &state.Failure{
				Code:    "provider_crashed",
				Message: "codex failed",
				Details: map[string]any{
					"provider_error": providerError,
				},
			},
		}
	}
	reconnectThenRateLimitFailure := func(requestID string, sessionID string) state.StepResult {
		result := rateLimitFailure(requestID, sessionID)
		result.Error.Message = "codex failed after reconnect"
		return result
	}

	testCases := []struct {
		name                       string
		defaults                   map[string]any
		results                    []state.StepResult
		wantAttempts               int
		wantSleepCalls             int
		wantSuccess                bool
		wantFailureCode            string
		wantContinuationSessionIDs []string
		wantContinuationPrompts    []string
	}{
		{
			name: "retries once when defaults tpm is set and first failure is 429",
			defaults: map[string]any{
				"tpm": 60000,
			},
			results: []state.StepResult{
				rateLimitFailure("req_1", "sess-1"),
				{
					Status: state.StepStatusSucceeded,
					Value:  "ok",
				},
			},
			wantAttempts:               2,
			wantSleepCalls:             1,
			wantSuccess:                true,
			wantContinuationSessionIDs: []string{"", "sess-1"},
			wantContinuationPrompts:    []string{"", defaultCodexResumePrompt},
		},
		{
			name: "reconnect error followed by terminal 429 triggers retry",
			defaults: map[string]any{
				"tpm": 60000,
			},
			results: []state.StepResult{
				reconnectThenRateLimitFailure("req_reconnect_429", "sess-reconnect-429"),
				{
					Status: state.StepStatusSucceeded,
					Value:  "ok",
				},
			},
			wantAttempts:               2,
			wantSleepCalls:             1,
			wantSuccess:                true,
			wantContinuationSessionIDs: []string{"", "sess-reconnect-429"},
			wantContinuationPrompts:    []string{"", defaultCodexResumePrompt},
		},
		{
			name: "object form retries twice when retries is 2",
			defaults: map[string]any{
				"tpm": map[string]any{
					"rate":    60000,
					"retries": 2,
				},
			},
			results: []state.StepResult{
				rateLimitFailure("req_obj_1", "sess-obj-1"),
				rateLimitFailure("req_obj_2", "sess-obj-2"),
				{
					Status: state.StepStatusSucceeded,
					Value:  "ok",
				},
			},
			wantAttempts:               3,
			wantSleepCalls:             2,
			wantSuccess:                true,
			wantContinuationSessionIDs: []string{"", "sess-obj-1", "sess-obj-2"},
			wantContinuationPrompts:    []string{"", defaultCodexResumePrompt, defaultCodexResumePrompt},
		},
		{
			name: "object form with retries zero does not retry",
			defaults: map[string]any{
				"tpm": map[string]any{
					"rate":    60000,
					"retries": 0,
				},
			},
			results: []state.StepResult{
				rateLimitFailure("req_obj_3", "sess-obj-3"),
			},
			wantAttempts:               1,
			wantSleepCalls:             0,
			wantSuccess:                false,
			wantFailureCode:            "provider_crashed",
			wantContinuationSessionIDs: []string{""},
			wantContinuationPrompts:    []string{""},
		},
		{
			name: "fails when defaults tpm scalar is invalid",
			defaults: map[string]any{
				"tpm": "bad",
			},
			results: []state.StepResult{
				{
					Status: state.StepStatusSucceeded,
					Value:  "unused",
				},
			},
			wantAttempts:               0,
			wantSleepCalls:             0,
			wantSuccess:                false,
			wantFailureCode:            "invalid_defaults",
			wantContinuationSessionIDs: []string{},
			wantContinuationPrompts:    []string{},
		},
		{
			name: "object form retries without rate and uses fresh fallback once",
			defaults: map[string]any{
				"tpm": map[string]any{
					"retries": 2,
				},
			},
			results: []state.StepResult{
				rateLimitFailure("req_no_rate_1", ""),
				rateLimitFailure("req_no_rate_2", "sess-no-rate-2"),
				{
					Status: state.StepStatusSucceeded,
					Value:  "ok",
				},
			},
			wantAttempts:               3,
			wantSleepCalls:             2,
			wantSuccess:                true,
			wantContinuationSessionIDs: []string{"", "", "sess-no-rate-2"},
			wantContinuationPrompts:    []string{"", "", defaultCodexResumePrompt},
		},
		{
			name: "fails when defaults tpm object is empty",
			defaults: map[string]any{
				"tpm": map[string]any{},
			},
			results:                    []state.StepResult{{Status: state.StepStatusSucceeded, Value: "unused"}},
			wantAttempts:               0,
			wantSleepCalls:             0,
			wantSuccess:                false,
			wantFailureCode:            "invalid_defaults",
			wantContinuationSessionIDs: []string{},
			wantContinuationPrompts:    []string{},
		},
		{
			name: "fails when defaults tpm object retries is invalid",
			defaults: map[string]any{
				"tpm": map[string]any{
					"rate":    60000,
					"retries": -1,
				},
			},
			results:                    []state.StepResult{{Status: state.StepStatusSucceeded, Value: "unused"}},
			wantAttempts:               0,
			wantSleepCalls:             0,
			wantSuccess:                false,
			wantFailureCode:            "invalid_defaults",
			wantContinuationSessionIDs: []string{},
			wantContinuationPrompts:    []string{},
		},
		{
			name: "ignores defaults tpm object retry_preamble",
			defaults: map[string]any{
				"tpm": map[string]any{
					"rate":           60000,
					"retry_preamble": 123,
				},
			},
			results: []state.StepResult{
				rateLimitFailure("req_ignore_preamble_1", "sess-ignore-1"),
				{
					Status: state.StepStatusSucceeded,
					Value:  "ok",
				},
			},
			wantAttempts:               2,
			wantSleepCalls:             1,
			wantSuccess:                true,
			wantContinuationSessionIDs: []string{"", "sess-ignore-1"},
			wantContinuationPrompts:    []string{"", defaultCodexResumePrompt},
		},
		{
			name: "does not retry when defaults tpm is missing",
			results: []state.StepResult{
				rateLimitFailure("req_2", "sess-2"),
			},
			wantAttempts:               1,
			wantSleepCalls:             0,
			wantSuccess:                false,
			wantFailureCode:            "provider_crashed",
			wantContinuationSessionIDs: []string{""},
			wantContinuationPrompts:    []string{""},
		},
		{
			name: "scalar tpm keeps default retries at one",
			defaults: map[string]any{
				"tpm": 60000,
			},
			results: []state.StepResult{
				rateLimitFailure("req_3", "sess-3"),
				rateLimitFailure("req_4", "sess-4"),
			},
			wantAttempts:               2,
			wantSleepCalls:             1,
			wantSuccess:                false,
			wantFailureCode:            "provider_crashed",
			wantContinuationSessionIDs: []string{"", "sess-3"},
			wantContinuationPrompts:    []string{"", defaultCodexResumePrompt},
		},
		{
			name: "missing session id uses only one fresh fallback even with higher retries",
			defaults: map[string]any{
				"tpm": map[string]any{
					"retries": 3,
				},
			},
			results: []state.StepResult{
				rateLimitFailure("req_fresh_1", ""),
				rateLimitFailure("req_fresh_2", ""),
				rateLimitFailure("req_fresh_3", ""),
			},
			wantAttempts:               2,
			wantSleepCalls:             1,
			wantSuccess:                false,
			wantFailureCode:            "provider_crashed",
			wantContinuationSessionIDs: []string{"", ""},
			wantContinuationPrompts:    []string{"", ""},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			config := testConfig(t, spec.Document{
				Version:  spec.Version,
				Name:     "sample",
				Entry:    "main",
				Defaults: tc.defaults,
				Flows: map[string]spec.Flow{
					"main": {
						Steps: []spec.Step{
							{ID: "codex-step", Type: "codex"},
						},
					},
				},
			})
			mustPersist(t, config)

			attempts := 0
			continuationSessionIDs := []string{}
			continuationPrompts := []string{}
			registry := NewRegistry()
			if err := registry.Register("codex", func() executorapi.Executor {
				return &fakeExecutor{
					calls: new([]string),
					executeWithContext: func(_ context.Context, ctx executorapi.StepContext) state.StepResult {
						if attempts >= len(tc.results) {
							t.Fatalf("unexpected attempt %d", attempts+1)
						}
						continuationSessionIDs = append(continuationSessionIDs, ctx.ContinuationSessionID)
						continuationPrompts = append(continuationPrompts, ctx.ContinuationPrompt)
						result := tc.results[attempts]
						attempts++
						return cloneStepResult(result)
					},
				}
			}); err != nil {
				t.Fatalf("register codex executor: %v", err)
			}

			sleepCalls := 0
			var slept []time.Duration
			waitFn := func(_ context.Context, duration time.Duration) error {
				sleepCalls++
				slept = append(slept, duration)
				return nil
			}

			snapshot, err := NewRunner(registry, withRunnerRetryWait(waitFn)).Run(context.Background(), config)
			if tc.wantSuccess {
				if err != nil {
					t.Fatalf("run: %v", err)
				}
				if snapshot.Status != state.RunStatusSucceeded {
					t.Fatalf("snapshot status = %q, want succeeded", snapshot.Status)
				}
			} else {
				assertRunFailed(t, err, tc.wantFailureCode)
			}

			if attempts != tc.wantAttempts {
				t.Fatalf("attempts = %d, want %d", attempts, tc.wantAttempts)
			}
			if sleepCalls != tc.wantSleepCalls {
				t.Fatalf("sleep calls = %d, want %d", sleepCalls, tc.wantSleepCalls)
			}
			for _, duration := range slept {
				if duration != defaultCodexTPMRetryAfter {
					t.Fatalf("retry sleep duration = %s, want %s", duration, defaultCodexTPMRetryAfter)
				}
			}
			if !reflect.DeepEqual(continuationSessionIDs, tc.wantContinuationSessionIDs) {
				t.Fatalf("continuation session ids = %#v, want %#v", continuationSessionIDs, tc.wantContinuationSessionIDs)
			}
			if !reflect.DeepEqual(continuationPrompts, tc.wantContinuationPrompts) {
				t.Fatalf("continuation prompts = %#v, want %#v", continuationPrompts, tc.wantContinuationPrompts)
			}
		})
	}
}

func TestRunnerAgentStructuredOutputRecovery(t *testing.T) {
	t.Parallel()

	responseSchema := map[string]any{
		"type":                 "object",
		"required":             []any{"approved"},
		"additionalProperties": false,
		"properties": map[string]any{
			"approved": "boolean",
		},
	}
	inlineSchemaPrompt := defaultStructuredPrompt + "\n\nRequired JSON Schema:\n" + `{
  "additionalProperties": false,
  "properties": {
    "approved": {
      "type": "boolean"
    }
  },
  "required": [
    "approved"
  ],
  "type": "object"
}`
	codexSchemaPrompt := defaultStructuredPrompt + "\n\nRequired JSON Schema:\n" + `{
  "additionalProperties": false,
  "properties": {
    "$thinking": {
      "$comment": "Thinking (reasoning) notes",
      "type": "string"
    },
    "approved": {
      "type": "boolean"
    }
  },
  "required": [
    "approved"
  ],
  "type": "object"
}`
	referencedSchemaPrompt := defaultStructuredPrompt + "\n\nRequired JSON Schema:\n" + `{
  "$defs": {
    "workflow:approval_result": {
      "additionalProperties": false,
      "properties": {
        "approved": {
          "type": "boolean"
        }
      },
      "required": [
        "approved"
      ],
      "type": "object"
    }
  },
  "additionalProperties": false,
  "properties": {
    "approved": {
      "type": "boolean"
    }
  },
  "required": [
    "approved"
  ],
  "type": "object"
}`

	type attemptSpec struct {
		result  state.StepResult
		session string
		stdout  string
	}

	structuredFailure := func(code string) state.StepResult {
		return state.StepResult{
			Status: state.StepStatusFailed,
			Error: &state.Failure{
				Code:    code,
				Message: "structured output failed",
			},
		}
	}
	successValue := func(value any) state.StepResult {
		return state.StepResult{
			Status: state.StepStatusSucceeded,
			Value:  value,
		}
	}

	testCases := []struct {
		name                       string
		executorType               string
		schemas                    map[string]any
		response                   map[string]any
		structuredRetry            any
		attempts                   []attemptSpec
		wantSuccess                bool
		wantFailureCode            string
		wantValue                  any
		wantExecutorAttempts       int
		wantContinuationSessionIDs []string
		wantContinuationPrompts    []string
	}{
		{
			name:         "malformed provider output retries claude with default prompt and session",
			executorType: "claude",
			attempts: []attemptSpec{
				{result: structuredFailure("invalid_provider_output"), session: "sess-claude"},
				{result: successValue(map[string]any{"approved": true})},
			},
			wantSuccess:                true,
			wantValue:                  map[string]any{"approved": true},
			wantExecutorAttempts:       2,
			wantContinuationSessionIDs: []string{"", "sess-claude"},
			wantContinuationPrompts:    []string{"", inlineSchemaPrompt},
		},
		{
			name:         "schema mismatch retries codex with session",
			executorType: "codex",
			attempts: []attemptSpec{
				{
					result:  successValue(map[string]any{"approved": "yes", "$thinking": "bad type"}),
					session: "sess-codex",
				},
				{result: successValue(map[string]any{"approved": true, "$thinking": "fixed"})},
			},
			wantSuccess:                true,
			wantValue:                  map[string]any{"approved": true, "$thinking": "fixed"},
			wantExecutorAttempts:       2,
			wantContinuationSessionIDs: []string{"", "sess-codex"},
			wantContinuationPrompts:    []string{"", codexSchemaPrompt},
		},
		{
			name:         "schema ref expands in retry prompt",
			executorType: "claude",
			schemas: map[string]any{
				"approval_result": responseSchema,
			},
			response: map[string]any{"schema": "#/schemas/approval_result"},
			attempts: []attemptSpec{
				{result: structuredFailure("invalid_provider_output"), session: "sess-ref"},
				{result: successValue(map[string]any{"approved": true})},
			},
			wantSuccess:                true,
			wantValue:                  map[string]any{"approved": true},
			wantExecutorAttempts:       2,
			wantContinuationSessionIDs: []string{"", "sess-ref"},
			wantContinuationPrompts:    []string{"", referencedSchemaPrompt},
		},
		{
			name:         "crush retries fresh with prompt override",
			executorType: "crush",
			structuredRetry: map[string]any{
				"prompt": "Fix only JSON",
			},
			attempts: []attemptSpec{
				{result: structuredFailure("invalid_provider_output"), session: "ignored-session"},
				{result: successValue(map[string]any{"approved": true})},
			},
			wantSuccess:                true,
			wantValue:                  map[string]any{"approved": true},
			wantExecutorAttempts:       2,
			wantContinuationSessionIDs: []string{"", ""},
			wantContinuationPrompts:    []string{"", "Fix only JSON"},
		},
		{
			name:         "exhausted attempts fails with structured failure code",
			executorType: "claude",
			structuredRetry: map[string]any{
				"attempts": 2,
			},
			attempts: []attemptSpec{
				{result: structuredFailure("invalid_provider_output"), session: "sess-1"},
				{result: structuredFailure("invalid_provider_output"), session: "sess-2"},
			},
			wantFailureCode:            "invalid_provider_output",
			wantExecutorAttempts:       2,
			wantContinuationSessionIDs: []string{"", "sess-1"},
			wantContinuationPrompts:    []string{"", inlineSchemaPrompt},
		},
		{
			name:         "attempts one disables retry",
			executorType: "claude",
			structuredRetry: map[string]any{
				"attempts": 1,
			},
			attempts: []attemptSpec{
				{result: structuredFailure("invalid_provider_output"), session: "sess-disabled"},
			},
			wantFailureCode:            "invalid_provider_output",
			wantExecutorAttempts:       1,
			wantContinuationSessionIDs: []string{""},
			wantContinuationPrompts:    []string{""},
		},
		{
			name:         "stdout response validation remains fail fast",
			executorType: "claude",
			response: map[string]any{
				"from":   "stdout",
				"schema": responseSchema,
			},
			attempts: []attemptSpec{
				{result: successValue(map[string]any{"approved": true}), session: "sess-stdout", stdout: `{"approved":"bad"}`},
			},
			wantFailureCode:            responseCodeSchemaMismatch,
			wantExecutorAttempts:       1,
			wantContinuationSessionIDs: []string{""},
			wantContinuationPrompts:    []string{""},
		},
		{
			name:         "invalid structured retry defaults fail before provider execution",
			executorType: "claude",
			structuredRetry: map[string]any{
				"attempts": 0,
			},
			attempts:                   []attemptSpec{{result: successValue(map[string]any{"approved": true})}},
			wantFailureCode:            "invalid_defaults",
			wantExecutorAttempts:       0,
			wantContinuationSessionIDs: []string{},
			wantContinuationPrompts:    []string{},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			response := tc.response
			if response == nil {
				response = map[string]any{"schema": responseSchema}
			}
			defaults := map[string]any{}
			if tc.structuredRetry != nil {
				defaults = map[string]any{
					"executors": map[string]any{
						tc.executorType: map[string]any{
							"structured_retry": tc.structuredRetry,
						},
					},
				}
			}

			config := testConfig(t, spec.Document{
				Version:  spec.Version,
				Name:     "sample",
				Entry:    "main",
				Defaults: defaults,
				Schemas:  tc.schemas,
				Flows: map[string]spec.Flow{
					"main": {
						Steps: []spec.Step{
							{
								ID:   "agent-step",
								Type: tc.executorType,
								Fields: map[string]any{
									"response": response,
								},
							},
						},
					},
				},
			})
			mustPersist(t, config)

			executorAttempts := 0
			continuationSessionIDs := []string{}
			continuationPrompts := []string{}
			registry := NewRegistry()
			mustRegister(registry, tc.executorType, func() executorapi.Executor {
				return &fakeExecutor{
					calls: new([]string),
					execute: func(ctx executorapi.StepContext) state.StepResult {
						if executorAttempts >= len(tc.attempts) {
							t.Fatalf("unexpected attempt %d", executorAttempts+1)
						}
						continuationSessionIDs = append(continuationSessionIDs, ctx.ContinuationSessionID)
						continuationPrompts = append(continuationPrompts, ctx.ContinuationPrompt)

						attempt := tc.attempts[executorAttempts]
						executorAttempts++
						result := cloneStepResult(attempt.result)
						if attempt.session != "" {
							result.Artifacts.Files["metadata"] = writeProviderMetadataFixture(t, ctx, attempt.session)
						}
						if attempt.stdout != "" {
							result.Artifacts.Stdout = writeAttemptArtifactFixture(t, ctx, "stdout.txt", attempt.stdout)
						}
						return result
					},
				}
			})

			snapshot, err := NewRunner(registry).Run(context.Background(), config)
			if tc.wantSuccess {
				if err != nil {
					t.Fatalf("run: %v", err)
				}
				if snapshot.Status != state.RunStatusSucceeded {
					t.Fatalf("snapshot status = %q, want succeeded", snapshot.Status)
				}
				if !reflect.DeepEqual(snapshot.Steps[0].Value, tc.wantValue) {
					t.Fatalf("step value = %#v, want %#v", snapshot.Steps[0].Value, tc.wantValue)
				}
			} else {
				assertRunFailed(t, err, tc.wantFailureCode)
			}

			if executorAttempts != tc.wantExecutorAttempts {
				t.Fatalf("executor attempts = %d, want %d", executorAttempts, tc.wantExecutorAttempts)
			}
			if len(snapshot.Steps) != 1 {
				t.Fatalf("recorded steps = %d, want 1 final step", len(snapshot.Steps))
			}
			if !reflect.DeepEqual(continuationSessionIDs, tc.wantContinuationSessionIDs) {
				t.Fatalf("continuation session ids = %#v, want %#v", continuationSessionIDs, tc.wantContinuationSessionIDs)
			}
			if !reflect.DeepEqual(continuationPrompts, tc.wantContinuationPrompts) {
				t.Fatalf("continuation prompts = %#v, want %#v", continuationPrompts, tc.wantContinuationPrompts)
			}
		})
	}
}

func writeProviderMetadataFixture(t *testing.T, ctx executorapi.StepContext, sessionID string) string {
	t.Helper()

	return writeAttemptArtifactFixture(t, ctx, "provider-metadata.json", string(mustJSON(t, map[string]any{
		"continuation_session_id": sessionID,
	})))
}

func writeAttemptArtifactFixture(t *testing.T, ctx executorapi.StepContext, name string, contents string) string {
	t.Helper()

	stepDir := executorapi.StepArtifactDir(ctx.RunDir, ctx.StepIndex, ctx.Step.ID, ctx.ExecutionLabel)
	path := filepath.Join(stepDir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir attempt artifact dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write attempt artifact %s: %v", name, err)
	}
	return path
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json fixture: %v", err)
	}
	return data
}
