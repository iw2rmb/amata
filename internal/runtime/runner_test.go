package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	executorapi "auto/internal/executor"
	"auto/internal/spec"
	"auto/internal/state"
	"auto/internal/workspace"
)

func TestRunnerResumeContinuesFromFirstIncompleteStep(t *testing.T) {
	t.Parallel()

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Flows: map[string]spec.Flow{
			"main": {
				Steps: []spec.Step{
					{ID: "step-1", Type: "fake"},
					{ID: "step-2", Type: "fake"},
					{ID: "step-3", Type: "fake"},
				},
			},
		},
	})

	if err := PersistRunSpec(config); err != nil {
		t.Fatalf("persist run spec: %v", err)
	}

	store := state.NewStore(config.RunDir)
	if _, err := store.Append(state.RunEvent{
		Kind: state.EventRunInitialized,
		Frame: &state.FlowFrame{
			Flow:      config.Spec.Entry,
			StepCount: 3,
		},
		Command: "run",
	}); err != nil {
		t.Fatalf("append init: %v", err)
	}
	if _, err := store.Append(state.RunEvent{
		Kind: state.EventStepRecorded,
		Step: &state.StepResult{
			Index:  0,
			ID:     "step-1",
			Type:   "fake",
			Status: state.StepStatusSucceeded,
		},
	}); err != nil {
		t.Fatalf("append step 1: %v", err)
	}

	resumeConfig, err := LoadRunConfig(config.RunDir)
	if err != nil {
		t.Fatalf("load run config: %v", err)
	}

	secondCalls := []string{}
	secondRegistry := NewRegistry()
	if err := secondRegistry.Register("fake", func() executorapi.Executor {
		return &fakeExecutor{
			calls: &secondCalls,
			results: map[string]state.StepResult{
				"step-2": {Status: state.StepStatusSucceeded},
				"step-3": {Status: state.StepStatusSucceeded},
			},
		}
	}); err != nil {
		t.Fatalf("register resume executor: %v", err)
	}

	resumedSnapshot, err := NewRunner(secondRegistry).Resume(context.Background(), resumeConfig)
	if err != nil {
		t.Fatalf("resume run: %v", err)
	}
	if resumedSnapshot.Status != state.RunStatusSucceeded {
		t.Fatalf("resumed snapshot.status = %q, want succeeded", resumedSnapshot.Status)
	}
	if len(resumedSnapshot.Steps) != 3 {
		t.Fatalf("resumed step count = %d, want 3", len(resumedSnapshot.Steps))
	}
	if got, want := secondCalls, []string{"step-2", "step-3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("resume calls = %#v, want %#v", got, want)
	}
	if resumedSnapshot.Steps[0].ID != "step-1" {
		t.Fatalf("first step id = %q, want step-1", resumedSnapshot.Steps[0].ID)
	}
}

func TestRunnerBuiltinsShellCapturesArtifactsAndNormalizesCWD(t *testing.T) {
	t.Parallel()

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Flows: map[string]spec.Flow{
			"main": {
				Steps: []spec.Step{
					{
						ID: "shell-step",
						Fields: map[string]any{
							"command": "printf 'hello'; printf 'warn' >&2; pwd > report.txt",
							"cwd":     "nested",
							"files": map[string]any{
								"report": "nested/report.txt",
							},
						},
					},
				},
			},
		},
	})

	if err := os.MkdirAll(filepath.Join(config.Workspace.Root, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir nested cwd: %v", err)
	}
	if err := PersistRunSpec(config); err != nil {
		t.Fatalf("persist run spec: %v", err)
	}

	snapshot, err := NewRunner(nil).Run(context.Background(), config)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	result := snapshot.Steps[0]
	if result.Status != state.StepStatusSucceeded {
		t.Fatalf("step status = %q, want succeeded", result.Status)
	}
	if got := result.Value.(map[string]any)["exitCode"].(float64); got != 0 {
		t.Fatalf("exitCode = %#v, want 0", got)
	}
	if got := strings.TrimSpace(readFile(t, result.Artifacts.Stdout)); got != "hello" {
		t.Fatalf("stdout = %q, want hello", got)
	}
	if got := strings.TrimSpace(readFile(t, result.Artifacts.Stderr)); got != "warn" {
		t.Fatalf("stderr = %q, want warn", got)
	}

	reportPath := result.Artifacts.Files["report"]
	if reportPath == "" {
		t.Fatalf("named report artifact missing")
	}
	if got := strings.TrimSpace(readFile(t, reportPath)); got != filepath.Join(config.Workspace.Root, "nested") {
		t.Fatalf("captured cwd = %q, want %q", got, filepath.Join(config.Workspace.Root, "nested"))
	}
}

func TestRunnerBuiltinsShellResolveTemplatedScalars(t *testing.T) {
	t.Parallel()

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Params: map[string]any{
			"filename": "report",
			"content":  "templated",
		},
		Flows: map[string]spec.Flow{
			"main": {
				Steps: []spec.Step{
					{
						ID: "shell-step",
						Fields: map[string]any{
							"command": []any{
								"sh",
								"-lc",
								"printf '{{ ctx.params.content }}' > {{ ctx.params.filename }}.txt",
							},
							"cwd": "{{ ctx.workspace.root }}/nested",
							"files": map[string]any{
								"report": "{{ ctx.workspace.root }}/nested/{{ ctx.params.filename }}.txt",
							},
						},
					},
				},
			},
		},
	})

	if err := os.MkdirAll(filepath.Join(config.Workspace.Root, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir nested cwd: %v", err)
	}
	if err := PersistRunSpec(config); err != nil {
		t.Fatalf("persist run spec: %v", err)
	}

	snapshot, err := NewRunner(nil).Run(context.Background(), config)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	reportPath := snapshot.Steps[0].Artifacts.Files["report"]
	if reportPath == "" {
		t.Fatalf("named report artifact missing")
	}
	if got := strings.TrimSpace(readFile(t, reportPath)); got != "templated" {
		t.Fatalf("captured report = %q, want templated", got)
	}
}

func TestRunnerPersistsRunMetadataAndArtifactsUnderRunDirectory(t *testing.T) {
	t.Parallel()

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Flows: map[string]spec.Flow{
			"main": {
				Steps: []spec.Step{
					{
						ID: "shell-step",
						Fields: map[string]any{
							"command": "printf 'hello'; printf 'warn' >&2; printf 'report' > report.txt",
							"files": map[string]any{
								"report": "report.txt",
							},
						},
					},
				},
			},
		},
	})

	if err := PersistRunSpec(config); err != nil {
		t.Fatalf("persist run spec: %v", err)
	}

	snapshot, err := NewRunner(nil).Run(context.Background(), config)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	expectedPaths := []string{
		filepath.Join(config.RunDir, "spec.yaml"),
		filepath.Join(config.RunDir, "events.ndjson"),
		filepath.Join(config.RunDir, "snapshot.json"),
		snapshot.Steps[0].Artifacts.Stdout,
		snapshot.Steps[0].Artifacts.Stderr,
		snapshot.Steps[0].Artifacts.Files["report"],
	}
	for _, path := range expectedPaths {
		if !strings.HasPrefix(path, config.RunDir+string(os.PathSeparator)) {
			t.Fatalf("path %q does not live under run dir %q", path, config.RunDir)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
	}
}

func TestRunnerBuiltinShellRejectsInvalidFilesBeforeCommandRuns(t *testing.T) {
	t.Parallel()

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Flows: map[string]spec.Flow{
			"main": {
				Steps: []spec.Step{
					{
						ID: "shell-step",
						Fields: map[string]any{
							"command": "touch should-not-exist.txt",
							"files":   []any{"bad"},
						},
					},
				},
			},
		},
	})

	if err := PersistRunSpec(config); err != nil {
		t.Fatalf("persist run spec: %v", err)
	}

	_, err := NewRunner(nil).Run(context.Background(), config)
	var failedErr RunFailedError
	if !errors.As(err, &failedErr) {
		t.Fatalf("run error = %v, want RunFailedError", err)
	}
	if failedErr.Failure.Code != "invalid_files" {
		t.Fatalf("failure code = %q, want invalid_files", failedErr.Failure.Code)
	}
	if _, err := os.Stat(filepath.Join(config.Workspace.Root, "should-not-exist.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("command side effect err = %v, want not exists", err)
	}
}

func TestRunnerBuiltinShellKeepsStdIOArtifactsWhenNamedFileCaptureFails(t *testing.T) {
	t.Parallel()

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Flows: map[string]spec.Flow{
			"main": {
				Steps: []spec.Step{
					{
						ID: "shell-step",
						Fields: map[string]any{
							"command": "printf 'hello'; printf 'warn' >&2",
							"files": map[string]any{
								"missing": "missing.txt",
							},
						},
					},
				},
			},
		},
	})

	if err := PersistRunSpec(config); err != nil {
		t.Fatalf("persist run spec: %v", err)
	}

	_, err := NewRunner(nil).Run(context.Background(), config)
	var failedErr RunFailedError
	if !errors.As(err, &failedErr) {
		t.Fatalf("run error = %v, want RunFailedError", err)
	}
	if failedErr.Failure.Code != "artifact_capture_failed" {
		t.Fatalf("failure code = %q, want artifact_capture_failed", failedErr.Failure.Code)
	}

	snapshot, err := state.NewStore(config.RunDir).LoadSnapshot()
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	result := snapshot.Steps[0]
	if result.Artifacts.Stdout == "" {
		t.Fatalf("stdout artifact path missing")
	}
	if result.Artifacts.Stderr == "" {
		t.Fatalf("stderr artifact path missing")
	}
	if got := strings.TrimSpace(readFile(t, result.Artifacts.Stdout)); got != "hello" {
		t.Fatalf("stdout = %q, want hello", got)
	}
	if got := strings.TrimSpace(readFile(t, result.Artifacts.Stderr)); got != "warn" {
		t.Fatalf("stderr = %q, want warn", got)
	}
}

func TestRunnerBuiltinsExprAndWhenSkip(t *testing.T) {
	t.Parallel()

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Flows: map[string]spec.Flow{
			"main": {
				Steps: []spec.Step{
					{
						ID: "typed-expr",
						Fields: map[string]any{
							"expr": map[string]any{
								"ok":    true,
								"count": 2,
								"items": []any{"x", 3},
							},
						},
					},
					{
						ID: "when-shorthand",
						Fields: map[string]any{
							"expr": "ran",
							"when": `$.prev.value["ok"]`,
						},
					},
					{
						ID: "skip-expr",
						Fields: map[string]any{
							"expr": "ignored",
							"when": map[string]any{
								"expr": `False`,
							},
						},
					},
					{
						ID: "skip-assert",
						Fields: map[string]any{
							"assert": false,
							"when":   false,
						},
					},
				},
			},
		},
	})

	if err := PersistRunSpec(config); err != nil {
		t.Fatalf("persist run spec: %v", err)
	}

	snapshot, err := NewRunner(nil).Run(context.Background(), config)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if snapshot.Steps[0].Status != state.StepStatusSucceeded {
		t.Fatalf("expr status = %q, want succeeded", snapshot.Steps[0].Status)
	}
	if got := snapshot.Steps[0].Value.(map[string]any)["ok"]; got != true {
		t.Fatalf("expr value ok = %#v, want true", got)
	}
	if snapshot.Steps[1].Status != state.StepStatusSucceeded {
		t.Fatalf("when-shorthand status = %q, want succeeded", snapshot.Steps[1].Status)
	}
	if snapshot.Steps[2].Status != state.StepStatusSkipped {
		t.Fatalf("skip-expr status = %q, want skipped", snapshot.Steps[2].Status)
	}
	if snapshot.Steps[3].Status != state.StepStatusSkipped {
		t.Fatalf("skip-assert status = %q, want skipped", snapshot.Steps[3].Status)
	}
}

func TestRunnerBuiltinAssertFailsStructurally(t *testing.T) {
	t.Parallel()

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Flows: map[string]spec.Flow{
			"main": {
				Steps: []spec.Step{
					{
						ID: "check",
						Fields: map[string]any{
							"assert":  false,
							"message": "nope",
						},
					},
				},
			},
		},
	})

	if err := PersistRunSpec(config); err != nil {
		t.Fatalf("persist run spec: %v", err)
	}

	_, err := NewRunner(nil).Run(context.Background(), config)
	var failedErr RunFailedError
	if !errors.As(err, &failedErr) {
		t.Fatalf("run error = %v, want RunFailedError", err)
	}
	if failedErr.Failure.Code != "assertion_failed" {
		t.Fatalf("failure code = %q, want assertion_failed", failedErr.Failure.Code)
	}
	if failedErr.Failure.Message != "nope" {
		t.Fatalf("failure message = %q, want nope", failedErr.Failure.Message)
	}
}

func TestRunnerBuiltinAssertUsesSharedRuntime(t *testing.T) {
	t.Parallel()

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Flows: map[string]spec.Flow{
			"main": {
				Steps: []spec.Step{
					{
						ID: "produce",
						Fields: map[string]any{
							"expr": map[string]any{
								"approved": false,
								"message":  "templated failure",
							},
						},
					},
					{
						ID: "check",
						Fields: map[string]any{
							"assert":  `$.prev.value["approved"]`,
							"message": `{{ ctx.prev.value["message"] }}`,
						},
					},
				},
			},
		},
	})

	if err := PersistRunSpec(config); err != nil {
		t.Fatalf("persist run spec: %v", err)
	}

	_, err := NewRunner(nil).Run(context.Background(), config)
	var failedErr RunFailedError
	if !errors.As(err, &failedErr) {
		t.Fatalf("run error = %v, want RunFailedError", err)
	}
	if failedErr.Failure.Code != "assertion_failed" {
		t.Fatalf("failure code = %q, want assertion_failed", failedErr.Failure.Code)
	}
	if failedErr.Failure.Message != "templated failure" {
		t.Fatalf("failure message = %q, want templated failure", failedErr.Failure.Message)
	}
}

func TestRunnerExpressionsTemplatesAndExpectShareRuntimeTypes(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"approved": true,
		"items":    []any{"x", int64(3)},
	}

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Params: map[string]any{
			"payload": payload,
		},
		Flows: map[string]spec.Flow{
			"main": {
				Steps: []spec.Step{
					{
						ID: "expr-shorthand",
						Fields: map[string]any{
							"expr": "$.params.payload",
						},
					},
					{
						ID: "template-whole",
						Fields: map[string]any{
							"expr": "{{ ctx.params.payload }}",
						},
					},
					{
						ID: "literal-escape",
						Fields: map[string]any{
							"expr": "$$.params.payload",
						},
					},
					{
						ID: "expect-current-step",
						Fields: map[string]any{
							"expr": map[string]any{
								"approved": true,
							},
							"expect": map[string]any{
								"expr": `ctx.value["approved"]`,
							},
						},
					},
				},
			},
		},
	})

	if err := PersistRunSpec(config); err != nil {
		t.Fatalf("persist run spec: %v", err)
	}

	snapshot, err := NewRunner(nil).Run(context.Background(), config)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	gotPayload, ok := snapshot.Steps[0].Value.(map[string]any)
	if !ok {
		t.Fatalf("expr shorthand value type = %T, want map[string]any", snapshot.Steps[0].Value)
	}
	if gotPayload["approved"] != true {
		t.Fatalf("expr shorthand approved = %#v, want true", gotPayload["approved"])
	}
	if !reflect.DeepEqual(snapshot.Steps[1].Value, snapshot.Steps[0].Value) {
		t.Fatalf("template value = %#v, want %#v", snapshot.Steps[1].Value, snapshot.Steps[0].Value)
	}
	if snapshot.Steps[2].Value != "$.params.payload" {
		t.Fatalf("escaped value = %#v, want %q", snapshot.Steps[2].Value, "$.params.payload")
	}
	if snapshot.Steps[3].Status != state.StepStatusSucceeded {
		t.Fatalf("expect step status = %q, want succeeded", snapshot.Steps[3].Status)
	}
}

func TestRunnerExpectDoesNotOverrideExecutionFailure(t *testing.T) {
	t.Parallel()

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Flows: map[string]spec.Flow{
			"main": {
				Steps: []spec.Step{
					{
						ID: "shell-failure",
						Fields: map[string]any{
							"command": "exit 2",
							"expect": map[string]any{
								"expr": "False",
							},
						},
					},
				},
			},
		},
	})

	if err := PersistRunSpec(config); err != nil {
		t.Fatalf("persist run spec: %v", err)
	}

	_, err := NewRunner(nil).Run(context.Background(), config)
	var failedErr RunFailedError
	if !errors.As(err, &failedErr) {
		t.Fatalf("run error = %v, want RunFailedError", err)
	}
	if failedErr.Failure.Code != "shell_failed" {
		t.Fatalf("failure code = %q, want shell_failed", failedErr.Failure.Code)
	}
}

func TestRunnerResumeFinalizesDurableFailureWithoutRunningLaterSteps(t *testing.T) {
	t.Parallel()

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Flows: map[string]spec.Flow{
			"main": {
				Steps: []spec.Step{
					{ID: "step-1", Type: "fake"},
					{ID: "step-2", Type: "fake"},
				},
			},
		},
	})

	if err := PersistRunSpec(config); err != nil {
		t.Fatalf("persist run spec: %v", err)
	}

	store := state.NewStore(config.RunDir)
	if _, err := store.Append(state.RunEvent{
		Kind: state.EventRunInitialized,
		Frame: &state.FlowFrame{
			Flow:      config.Spec.Entry,
			StepCount: 2,
		},
		Command: "run",
	}); err != nil {
		t.Fatalf("append init: %v", err)
	}
	if _, err := store.Append(state.RunEvent{
		Kind: state.EventStepRecorded,
		Step: &state.StepResult{
			Index:  0,
			ID:     "step-1",
			Type:   "fake",
			Status: state.StepStatusFailed,
			Error: &state.Failure{
				Code:    "broken",
				Message: "broken",
			},
		},
	}); err != nil {
		t.Fatalf("append failed step: %v", err)
	}

	resumeConfig, err := LoadRunConfig(config.RunDir)
	if err != nil {
		t.Fatalf("load run config: %v", err)
	}

	calls := []string{}
	registry := NewRegistry()
	if err := registry.Register("fake", func() executorapi.Executor {
		return &fakeExecutor{calls: &calls}
	}); err != nil {
		t.Fatalf("register executor: %v", err)
	}

	_, err = NewRunner(registry).Resume(context.Background(), resumeConfig)
	var failedErr RunFailedError
	if !errors.As(err, &failedErr) {
		t.Fatalf("resume error = %v, want RunFailedError", err)
	}
	if got := calls; len(got) != 0 {
		t.Fatalf("resume executed steps = %#v, want none", got)
	}
}

func TestRunnerPersistsSkippedStepBeforeAdvancing(t *testing.T) {
	t.Parallel()

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Flows: map[string]spec.Flow{
			"main": {
				Steps: []spec.Step{
					{ID: "step-1", Type: "fake"},
					{ID: "step-2", Type: "fake"},
				},
			},
		},
	})

	if err := PersistRunSpec(config); err != nil {
		t.Fatalf("persist run spec: %v", err)
	}

	calls := []string{}
	registry := NewRegistry()
	if err := registry.Register("fake", func() executorapi.Executor {
		return &fakeExecutor{
			calls: &calls,
			results: map[string]state.StepResult{
				"step-1": {Status: state.StepStatusSkipped},
				"step-2": {Status: state.StepStatusSucceeded},
			},
		}
	}); err != nil {
		t.Fatalf("register executor: %v", err)
	}

	snapshot, err := NewRunner(registry).Run(context.Background(), config)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got, want := calls, []string{"step-1", "step-2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %#v, want %#v", got, want)
	}
	if snapshot.Status != state.RunStatusSucceeded {
		t.Fatalf("snapshot.status = %q, want succeeded", snapshot.Status)
	}
	if len(snapshot.Steps) != 2 {
		t.Fatalf("step count = %d, want 2", len(snapshot.Steps))
	}
	if snapshot.Steps[0].Status != state.StepStatusSkipped {
		t.Fatalf("step 1 status = %q, want skipped", snapshot.Steps[0].Status)
	}
	if snapshot.Frames[0].NextStep != 2 {
		t.Fatalf("next step = %d, want 2", snapshot.Frames[0].NextStep)
	}
}

func TestRunnerResumeKeepsTerminalFailureState(t *testing.T) {
	t.Parallel()

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Flows: map[string]spec.Flow{
			"main": {
				Steps: []spec.Step{
					{ID: "step-1", Type: "fake"},
				},
			},
		},
	})

	if err := PersistRunSpec(config); err != nil {
		t.Fatalf("persist run spec: %v", err)
	}

	registry := NewRegistry()
	if err := registry.Register("fake", func() executorapi.Executor {
		return &fakeExecutor{
			calls: new([]string),
			results: map[string]state.StepResult{
				"step-1": {
					Status: state.StepStatusFailed,
					Error: &state.Failure{
						Code:    "broken",
						Message: "broken",
					},
				},
			},
		}
	}); err != nil {
		t.Fatalf("register executor: %v", err)
	}

	_, err := NewRunner(registry).Run(context.Background(), config)
	var initialErr RunFailedError
	if !errors.As(err, &initialErr) {
		t.Fatalf("run error = %v, want RunFailedError", err)
	}

	store := state.NewStore(config.RunDir)
	beforeEvents, err := os.ReadFile(store.EventsPath())
	if err != nil {
		t.Fatalf("read events before resume: %v", err)
	}

	resumeConfig, err := LoadRunConfig(config.RunDir)
	if err != nil {
		t.Fatalf("load run config: %v", err)
	}

	_, err = NewRunner(NewRegistry()).Resume(context.Background(), resumeConfig)
	var resumeErr RunFailedError
	if !errors.As(err, &resumeErr) {
		t.Fatalf("resume error = %v, want RunFailedError", err)
	}

	afterEvents, err := os.ReadFile(store.EventsPath())
	if err != nil {
		t.Fatalf("read events after resume: %v", err)
	}
	if !reflect.DeepEqual(afterEvents, beforeEvents) {
		t.Fatalf("terminal failure resume rewrote event history")
	}

	snapshot, err := store.LoadSnapshot()
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if snapshot.Status != state.RunStatusFailed {
		t.Fatalf("snapshot.status = %q, want failed", snapshot.Status)
	}
}

func TestRunnerResumeReloadsTerminalStateFromEvents(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name              string
		results           map[string]state.StepResult
		wantCalls         []string
		wantStatuses      []state.StepStatus
		wantRunStatus     state.RunStatus
		wantFailureCode   string
		wantFailureStepID string
	}{
		{
			name: "succeeded run",
			results: map[string]state.StepResult{
				"step-1": {Status: state.StepStatusSucceeded, Value: "one"},
				"step-2": {Status: state.StepStatusSkipped},
				"step-3": {Status: state.StepStatusSucceeded, Value: "three"},
			},
			wantCalls:     []string{"step-1", "step-2", "step-3"},
			wantStatuses:  []state.StepStatus{state.StepStatusSucceeded, state.StepStatusSkipped, state.StepStatusSucceeded},
			wantRunStatus: state.RunStatusSucceeded,
		},
		{
			name: "failed run",
			results: map[string]state.StepResult{
				"step-1": {Status: state.StepStatusSucceeded, Value: "one"},
				"step-2": {Status: state.StepStatusSkipped},
				"step-3": {
					Status: state.StepStatusFailed,
					Error: &state.Failure{
						Code:    "boom",
						Message: "boom",
					},
					Artifacts: state.Artifacts{
						Stdout: "preserved/stdout.txt",
						Stderr: "preserved/stderr.txt",
						Files: map[string]string{
							"report": "preserved/report.txt",
						},
					},
				},
			},
			wantCalls:         []string{"step-1", "step-2", "step-3"},
			wantStatuses:      []state.StepStatus{state.StepStatusSucceeded, state.StepStatusSkipped, state.StepStatusFailed},
			wantRunStatus:     state.RunStatusFailed,
			wantFailureCode:   "boom",
			wantFailureStepID: "step-3",
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			config := testConfig(t, spec.Document{
				Version: spec.Version,
				Name:    "sample",
				Entry:   "main",
				Flows: map[string]spec.Flow{
					"main": {
						Steps: []spec.Step{
							{ID: "step-1", Type: "fake"},
							{ID: "step-2", Type: "fake"},
							{ID: "step-3", Type: "fake"},
						},
					},
				},
			})

			if err := PersistRunSpec(config); err != nil {
				t.Fatalf("persist run spec: %v", err)
			}

			writePreservedArtifacts(t, config.RunDir)
			resolvedResults := resolveResultPaths(config.RunDir, testCase.results)

			initialCalls := []string{}
			initialRegistry := NewRegistry()
			if err := initialRegistry.Register("fake", func() executorapi.Executor {
				return &fakeExecutor{
					calls:   &initialCalls,
					results: resolvedResults,
				}
			}); err != nil {
				t.Fatalf("register executor: %v", err)
			}

			snapshot, err := NewRunner(initialRegistry).Run(context.Background(), config)
			if testCase.wantRunStatus == state.RunStatusFailed {
				var failedErr RunFailedError
				if !errors.As(err, &failedErr) {
					t.Fatalf("run error = %v, want RunFailedError", err)
				}
				snapshot, err = state.NewStore(config.RunDir).LoadSnapshot()
				if err != nil {
					t.Fatalf("load failed snapshot: %v", err)
				}
			} else if err != nil {
				t.Fatalf("run: %v", err)
			}

			if !reflect.DeepEqual(initialCalls, testCase.wantCalls) {
				t.Fatalf("initial calls = %#v, want %#v", initialCalls, testCase.wantCalls)
			}
			assertSnapshotStatuses(t, snapshot, testCase.wantStatuses, testCase.wantRunStatus)

			store := state.NewStore(config.RunDir)
			if err := os.Remove(store.SnapshotPath()); err != nil {
				t.Fatalf("remove snapshot: %v", err)
			}

			resumeConfig, err := LoadRunConfig(config.RunDir)
			if err != nil {
				t.Fatalf("load run config: %v", err)
			}

			resumeCalls := []string{}
			resumeRegistry := NewRegistry()
			if err := resumeRegistry.Register("fake", func() executorapi.Executor {
				return &fakeExecutor{
					calls:   &resumeCalls,
					results: resolvedResults,
				}
			}); err != nil {
				t.Fatalf("register resume executor: %v", err)
			}

			resumedSnapshot, err := NewRunner(resumeRegistry).Resume(context.Background(), resumeConfig)
			if testCase.wantRunStatus == state.RunStatusFailed {
				var failedErr RunFailedError
				if !errors.As(err, &failedErr) {
					t.Fatalf("resume error = %v, want RunFailedError", err)
				}
				if failedErr.Failure.Code != testCase.wantFailureCode {
					t.Fatalf("resume failure code = %q, want %q", failedErr.Failure.Code, testCase.wantFailureCode)
				}
				resumedSnapshot, err = store.LoadSnapshot()
				if err != nil {
					t.Fatalf("load rebuilt snapshot: %v", err)
				}
			} else if err != nil {
				t.Fatalf("resume: %v", err)
			}

			if len(resumeCalls) != 0 {
				t.Fatalf("resume calls = %#v, want none", resumeCalls)
			}
			assertSnapshotStatuses(t, resumedSnapshot, testCase.wantStatuses, testCase.wantRunStatus)

			if testCase.wantFailureStepID != "" {
				failedStep := resumedSnapshot.Steps[len(resumedSnapshot.Steps)-1]
				if failedStep.ID != testCase.wantFailureStepID {
					t.Fatalf("failed step id = %q, want %q", failedStep.ID, testCase.wantFailureStepID)
				}
				if failedStep.Error == nil || failedStep.Error.Code != testCase.wantFailureCode {
					t.Fatalf("failed step error = %#v, want code %q", failedStep.Error, testCase.wantFailureCode)
				}
				for _, artifactPath := range []string{
					failedStep.Artifacts.Stdout,
					failedStep.Artifacts.Stderr,
					failedStep.Artifacts.Files["report"],
				} {
					if !strings.HasPrefix(artifactPath, filepath.Join(config.RunDir, "preserved")+string(os.PathSeparator)) {
						t.Fatalf("artifact path = %q, want under preserved run dir", artifactPath)
					}
					if _, err := os.Stat(artifactPath); err != nil {
						t.Fatalf("stat preserved artifact %s: %v", artifactPath, err)
					}
				}
			}
		})
	}
}

type fakeExecutor struct {
	calls   *[]string
	results map[string]state.StepResult
	execute func(executorapi.StepContext) state.StepResult
}

func (e *fakeExecutor) Execute(_ context.Context, ctx executorapi.StepContext) state.StepResult {
	*e.calls = append(*e.calls, ctx.Step.ID)
	if e.execute != nil {
		return e.execute(ctx)
	}
	if result, ok := e.results[ctx.Step.ID]; ok {
		return result
	}
	return state.StepResult{Status: state.StepStatusSucceeded}
}

func writePreservedArtifacts(t *testing.T, runDir string) {
	t.Helper()

	preservedDir := filepath.Join(runDir, "preserved")
	if err := os.MkdirAll(preservedDir, 0o755); err != nil {
		t.Fatalf("mkdir preserved dir: %v", err)
	}

	files := map[string]string{
		"stdout.txt": "stdout",
		"stderr.txt": "stderr",
		"report.txt": "report",
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(preservedDir, name), []byte(contents), 0o644); err != nil {
			t.Fatalf("write preserved artifact %s: %v", name, err)
		}
	}
}

func resolveResultPaths(runDir string, results map[string]state.StepResult) map[string]state.StepResult {
	resolved := make(map[string]state.StepResult, len(results))
	for stepID, result := range results {
		if result.Artifacts.Stdout != "" && !filepath.IsAbs(result.Artifacts.Stdout) {
			result.Artifacts.Stdout = filepath.Join(runDir, result.Artifacts.Stdout)
		}
		if result.Artifacts.Stderr != "" && !filepath.IsAbs(result.Artifacts.Stderr) {
			result.Artifacts.Stderr = filepath.Join(runDir, result.Artifacts.Stderr)
		}
		if len(result.Artifacts.Files) > 0 {
			files := make(map[string]string, len(result.Artifacts.Files))
			for name, path := range result.Artifacts.Files {
				if filepath.IsAbs(path) {
					files[name] = path
					continue
				}
				files[name] = filepath.Join(runDir, path)
			}
			result.Artifacts.Files = files
		}
		resolved[stepID] = result
	}
	return resolved
}

func assertSnapshotStatuses(t *testing.T, snapshot state.Snapshot, wantStatuses []state.StepStatus, wantRunStatus state.RunStatus) {
	t.Helper()

	if snapshot.Status != wantRunStatus {
		t.Fatalf("snapshot.status = %q, want %q", snapshot.Status, wantRunStatus)
	}
	if len(snapshot.Steps) != len(wantStatuses) {
		t.Fatalf("step count = %d, want %d", len(snapshot.Steps), len(wantStatuses))
	}

	gotStatuses := make([]state.StepStatus, 0, len(snapshot.Steps))
	for _, step := range snapshot.Steps {
		gotStatuses = append(gotStatuses, step.Status)
	}
	if !slices.Equal(gotStatuses, wantStatuses) {
		t.Fatalf("step statuses = %#v, want %#v", gotStatuses, wantStatuses)
	}
}

func testConfig(t *testing.T, document spec.Document) Config {
	t.Helper()

	root := t.TempDir()
	stateDir := filepath.Join(root, "state")

	return Config{
		RunID:    "run-001",
		RunDir:   filepath.Join(stateDir, "runs", "run-001"),
		SpecPath: filepath.Join(root, "workflow.yaml"),
		Workspace: workspace.Config{
			Root:     root,
			StateDir: stateDir,
		},
		Spec: document,
	}
}

func TestResumeCLIUsesPersistedRunSpecFromRunDirectory(t *testing.T) {
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	cwd := t.TempDir()
	if err := os.Chdir(cwd); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		if chdirErr := os.Chdir(originalWD); chdirErr != nil {
			t.Fatalf("restore cwd: %v", chdirErr)
		}
	})

	specPath := filepath.Join(cwd, "workflow.yaml")
	specBody := `
version: amata/v1
name: sample
entry: main
workspace:
  root: .
  state_dir: state
flows:
  main: {}
`
	if err := os.WriteFile(specPath, []byte(specBody), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	if err := RunCLI([]string{"run", specPath, "--run-id", "run-001"}, nil, nil); err != nil {
		t.Fatalf("run cli: %v", err)
	}
	if err := os.Remove(specPath); err != nil {
		t.Fatalf("remove original spec: %v", err)
	}

	if err := RunCLI([]string{"resume", "run-001"}, nil, nil); err != nil {
		t.Fatalf("resume cli: %v", err)
	}
}

func TestLocateRunDirFailsWhenRunIDIsAmbiguous(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	paths := []string{
		filepath.Join(cwd, ".amata", "runs", "run-001", "spec.yaml"),
		filepath.Join(cwd, "state", "runs", "run-001", "spec.yaml"),
	}
	for _, path := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte("placeholder"), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	_, err := locateRunDir(cwd, "run-001")
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("locateRunDir error = %v, want ambiguous failure", err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return string(data)
}
