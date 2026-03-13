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
	"auto/internal/progress"
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

func TestRunnerResumeFinalizesCompletedChildFrameBeforeRunningParentNextStep(t *testing.T) {
	t.Parallel()

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Flows: map[string]spec.Flow{
			"main": {
				Steps: []spec.Step{
					{
						ID:   "loop",
						Type: "call",
						Fields: map[string]any{
							"flow": "loop",
						},
					},
					{ID: "after", Type: "fake"},
				},
			},
			"loop": {
				Steps: []spec.Step{
					{ID: "loop-step", Type: "fake"},
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
		Kind: state.EventFramePushed,
		Frame: &state.FlowFrame{
			Flow:      "loop",
			StepCount: 1,
			Return: &state.FrameReturn{
				StepType:  "call",
				StepIndex: 0,
				StepID:    "loop",
				Flow:      "loop",
			},
		},
	}); err != nil {
		t.Fatalf("append frame push: %v", err)
	}
	if _, err := store.Append(state.RunEvent{
		Kind: state.EventStepRecorded,
		Step: &state.StepResult{
			Index:  0,
			ID:     "loop-step",
			Type:   "fake",
			Status: state.StepStatusSucceeded,
			Value: map[string]any{
				"n": 1,
			},
			Artifacts: executorapi.EmptyArtifacts(),
		},
	}); err != nil {
		t.Fatalf("append child step: %v", err)
	}

	resumeConfig, err := LoadRunConfig(config.RunDir)
	if err != nil {
		t.Fatalf("load run config: %v", err)
	}

	calls := []string{}
	registry := NewRegistry()
	if err := registry.Register("fake", func() executorapi.Executor {
		return &fakeExecutor{
			calls: &calls,
			results: map[string]state.StepResult{
				"after": {
					Status: state.StepStatusSucceeded,
				},
				"loop-step": {
					Status: state.StepStatusSucceeded,
				},
			},
		}
	}); err != nil {
		t.Fatalf("register fake executor: %v", err)
	}

	snapshot, err := NewRunner(registry).Resume(context.Background(), resumeConfig)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if got, want := calls, []string{"after"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("resume calls = %#v, want %#v", got, want)
	}
	if snapshot.Status != state.RunStatusSucceeded {
		t.Fatalf("snapshot status = %q, want succeeded", snapshot.Status)
	}
	if got, want := len(snapshot.Steps), 3; got != want {
		t.Fatalf("step count = %d, want %d", got, want)
	}
	if snapshot.Steps[1].ID != "loop" || snapshot.Steps[1].Type != "call" {
		t.Fatalf("returned control step = %#v, want call result", snapshot.Steps[1])
	}
}

func TestRunnerEmitsLiveProgressForNestedSwitchAndCall(t *testing.T) {
	t.Parallel()

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Flows: map[string]spec.Flow{
			"main": {
				Steps: []spec.Step{
					{
						ID:   "switch-step",
						Type: "switch",
						Fields: map[string]any{
							"cases": []any{
								map[string]any{
									"when": true,
									"steps": []any{
										map[string]any{
											"id":   "branch-step",
											"type": "fake",
										},
									},
								},
							},
						},
					},
					{
						ID:   "call-step",
						Type: "call",
						Fields: map[string]any{
							"flow": "child",
						},
					},
				},
			},
			"child": {
				Steps: []spec.Step{
					{ID: "child-step", Type: "fake"},
				},
			},
		},
	})

	if err := PersistRunSpec(config); err != nil {
		t.Fatalf("persist run spec: %v", err)
	}

	registry := NewRegistry()
	calls := []string{}
	if err := registry.Register("fake", func() executorapi.Executor {
		return &fakeExecutor{
			calls: &calls,
			results: map[string]state.StepResult{
				"branch-step": {Status: state.StepStatusSucceeded},
				"child-step":  {Status: state.StepStatusSucceeded},
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

	assertProgressKindsAndSteps(t, events,
		[]progress.EventKind{
			progress.EventRunStarted,
			progress.EventStepStarted,
			progress.EventStepStarted,
			progress.EventStepFinished,
			progress.EventStepFinished,
			progress.EventStepStarted,
			progress.EventStepStarted,
			progress.EventStepFinished,
			progress.EventStepFinished,
			progress.EventRunFinished,
		},
		[]string{
			"",
			"switch-step",
			"branch-step",
			"branch-step",
			"switch-step",
			"call-step",
			"child-step",
			"child-step",
			"call-step",
			"",
		},
	)

	finished := events[len(events)-1]
	if finished.Status != progress.RunStatusSucceeded {
		t.Fatalf("final event status = %q, want succeeded", finished.Status)
	}
	if got := len(finished.Snapshot.Active); got != 0 {
		t.Fatalf("active step count = %d, want 0", got)
	}
	if got := len(finished.Snapshot.Steps); got != 4 {
		t.Fatalf("completed step count = %d, want 4", got)
	}
}

func TestRunnerResumeEmitsLiveProgressForReturnedControl(t *testing.T) {
	t.Parallel()

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Flows: map[string]spec.Flow{
			"main": {
				Steps: []spec.Step{
					{
						ID:   "loop",
						Type: "call",
						Fields: map[string]any{
							"flow": "loop",
						},
					},
					{ID: "after", Type: "fake"},
				},
			},
			"loop": {
				Steps: []spec.Step{
					{ID: "loop-step", Type: "fake"},
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
		Kind: state.EventFramePushed,
		Frame: &state.FlowFrame{
			Flow:      "loop",
			StepCount: 1,
			Return: &state.FrameReturn{
				StepType:  "call",
				StepIndex: 0,
				StepID:    "loop",
				Flow:      "loop",
			},
		},
	}); err != nil {
		t.Fatalf("append frame push: %v", err)
	}
	if _, err := store.Append(state.RunEvent{
		Kind: state.EventStepRecorded,
		Step: &state.StepResult{
			Index:     0,
			ID:        "loop-step",
			Type:      "fake",
			Status:    state.StepStatusSucceeded,
			Artifacts: executorapi.EmptyArtifacts(),
		},
	}); err != nil {
		t.Fatalf("append child step: %v", err)
	}

	resumeConfig, err := LoadRunConfig(config.RunDir)
	if err != nil {
		t.Fatalf("load run config: %v", err)
	}

	registry := NewRegistry()
	calls := []string{}
	if err := registry.Register("fake", func() executorapi.Executor {
		return &fakeExecutor{
			calls: &calls,
			results: map[string]state.StepResult{
				"after": {Status: state.StepStatusSucceeded},
			},
		}
	}); err != nil {
		t.Fatalf("register fake executor: %v", err)
	}

	var events []progress.Event
	sink := progress.SinkFunc(func(event progress.Event) {
		events = append(events, event)
	})

	snapshot, err := NewRunner(registry, WithRunnerProgressSink(sink)).Resume(context.Background(), resumeConfig)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if snapshot.Status != state.RunStatusSucceeded {
		t.Fatalf("snapshot status = %q, want succeeded", snapshot.Status)
	}

	assertProgressKindsAndSteps(t, events,
		[]progress.EventKind{
			progress.EventRunResumed,
			progress.EventStepFinished,
			progress.EventStepStarted,
			progress.EventStepFinished,
			progress.EventRunFinished,
		},
		[]string{
			"",
			"loop",
			"after",
			"after",
			"",
		},
	)
	if got := events[1].Step.Type; got != "call" {
		t.Fatalf("returned control step type = %q, want call", got)
	}
	if got, want := len(events[0].Snapshot.Active), 1; got != want {
		t.Fatalf("resume active step count = %d, want %d", got, want)
	}
	if got := events[0].Snapshot.Active[0]; got.ID != "loop" || got.Type != "call" || got.Status != progress.StepStatusRunning {
		t.Fatalf("resume active step = %#v, want running loop call", got)
	}
	if got := len(events[1].Snapshot.Active); got != 0 {
		t.Fatalf("active step count after returned control = %d, want 0", got)
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

func TestRunnerSwitchUsesFirstMatchingCaseAndReturnsStructuredResult(t *testing.T) {
	t.Parallel()

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Flows: map[string]spec.Flow{
			"main": {
				Steps: []spec.Step{
					{
						ID: "seed",
						Fields: map[string]any{
							"expr": map[string]any{
								"selection": "first",
								"seed":      "carried",
							},
						},
					},
					{
						ID:   "choose",
						Type: "switch",
						Fields: map[string]any{
							"cases": []any{
								map[string]any{
									"when": map[string]any{"expr": `ctx.prev.value["selection"] == "first"`},
									"steps": []spec.Step{
										{
											ID: "first-branch",
											Fields: map[string]any{
												"expr": map[string]any{
													"picked": "first",
													"seed":   `$.prev.value["seed"]`,
												},
											},
										},
									},
								},
								map[string]any{
									"when": true,
									"steps": []spec.Step{
										{
											ID: "second-branch",
											Fields: map[string]any{
												"expr": "second",
											},
										},
									},
								},
							},
						},
					},
					{
						ID: "after",
						Fields: map[string]any{
							"expr": map[string]any{
								"matched": `$.prev.value["matched"]`,
								"case":    `$.prev.value["case"]`,
								"picked":  `$.prev.value["value"]["picked"]`,
								"seed":    `$.prev.value["value"]["seed"]`,
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
	if snapshot.Status != state.RunStatusSucceeded {
		t.Fatalf("snapshot status = %q, want succeeded", snapshot.Status)
	}
	if got, want := len(snapshot.Steps), 4; got != want {
		t.Fatalf("step count = %d, want %d", got, want)
	}
	if snapshot.Steps[1].ID != "first-branch" {
		t.Fatalf("branch step id = %q, want first-branch", snapshot.Steps[1].ID)
	}
	if snapshot.Steps[2].ID != "choose" {
		t.Fatalf("switch result id = %q, want choose", snapshot.Steps[2].ID)
	}
	if snapshot.Steps[2].Type != "switch" {
		t.Fatalf("switch result type = %q, want switch", snapshot.Steps[2].Type)
	}

	switchValue := snapshot.Steps[2].Value.(map[string]any)
	if got := switchValue["matched"]; got != true {
		t.Fatalf("switch matched = %#v, want true", got)
	}
	if got := intValue(t, switchValue["case"]); got != 0 {
		t.Fatalf("switch case = %d, want 0", got)
	}
	if got := switchValue["value"].(map[string]any)["picked"]; got != "first" {
		t.Fatalf("switch nested value = %#v, want first", got)
	}

	for _, step := range snapshot.Steps {
		if step.ID == "second-branch" {
			t.Fatalf("second branch executed unexpectedly")
		}
	}

	after := snapshot.Steps[3].Value.(map[string]any)
	if got := after["matched"]; got != true {
		t.Fatalf("after matched = %#v, want true", got)
	}
	if got := intValue(t, after["case"]); got != 0 {
		t.Fatalf("after case = %d, want 0", got)
	}
	if got := after["picked"]; got != "first" {
		t.Fatalf("after picked = %#v, want first", got)
	}
	if got := after["seed"]; got != "carried" {
		t.Fatalf("after seed = %#v, want carried", got)
	}
}

func TestRunnerSwitchWithoutMatchDoesNotReuseIncomingPrevAsBranchOutput(t *testing.T) {
	t.Parallel()

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Flows: map[string]spec.Flow{
			"main": {
				Steps: []spec.Step{
					{
						ID: "seed",
						Fields: map[string]any{
							"expr": map[string]any{
								"carry": "value",
							},
						},
					},
					{
						ID:   "choose",
						Type: "switch",
						Fields: map[string]any{
							"cases": []any{
								map[string]any{
									"when": false,
									"steps": []spec.Step{
										{
											ID: "never",
											Fields: map[string]any{
												"expr": "unexpected",
											},
										},
									},
								},
							},
						},
					},
					{
						ID: "after",
						Fields: map[string]any{
							"expr": map[string]any{
								"matched": `$.prev.value["matched"]`,
								"value":   `$.prev.value["value"]`,
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

	switchValue := snapshot.Steps[1].Value.(map[string]any)
	if got := switchValue["matched"]; got != false {
		t.Fatalf("switch matched = %#v, want false", got)
	}
	if got := switchValue["value"]; got != nil {
		t.Fatalf("switch value = %#v, want nil", got)
	}

	after := snapshot.Steps[2].Value.(map[string]any)
	if got := after["matched"]; got != false {
		t.Fatalf("after matched = %#v, want false", got)
	}
	if got := after["value"]; got != nil {
		t.Fatalf("after value = %#v, want nil", got)
	}
}

func TestRunnerRecursiveCallCarriesFrameLocalPrevAndReturnsOneStack(t *testing.T) {
	t.Parallel()

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Flows: map[string]spec.Flow{
			"main": {
				Steps: []spec.Step{
					{
						ID: "seed",
						Fields: map[string]any{
							"expr": map[string]any{
								"n":   3,
								"sum": 0,
							},
						},
					},
					{
						ID:   "loop",
						Type: "call",
						Fields: map[string]any{
							"flow": "loop",
						},
					},
					{
						ID: "after",
						Fields: map[string]any{
							"expr": `$.prev.value["value"]`,
						},
					},
				},
			},
			"loop": {
				Steps: []spec.Step{
					{
						ID:   "branch",
						Type: "switch",
						Fields: map[string]any{
							"cases": []any{
								map[string]any{
									"when": map[string]any{"expr": `ctx.prev.value["n"] <= 0`},
									"steps": []spec.Step{
										{
											ID: "done",
											Fields: map[string]any{
												"expr": `$.prev.value`,
											},
										},
									},
								},
								map[string]any{
									"when": map[string]any{"expr": `ctx.prev.value["n"] > 0`},
									"steps": []spec.Step{
										{
											ID: "decrement",
											Fields: map[string]any{
												"expr": map[string]any{
													"n":   map[string]any{"expr": `ctx.prev.value["n"] - 1`},
													"sum": map[string]any{"expr": `ctx.prev.value["sum"] + ctx.prev.value["n"]`},
												},
											},
										},
										{
											ID:   "recurse",
											Type: "call",
											Fields: map[string]any{
												"flow": "loop",
											},
										},
										{
											ID: "unwrap",
											Fields: map[string]any{
												"expr": `$.prev.value["value"]`,
											},
										},
									},
								},
							},
						},
					},
					{
						ID: "return",
						Fields: map[string]any{
							"expr": `$.prev.value["value"]`,
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
	if snapshot.Status != state.RunStatusSucceeded {
		t.Fatalf("snapshot status = %q, want succeeded", snapshot.Status)
	}
	if got, want := len(snapshot.Frames), 1; got != want {
		t.Fatalf("frame count = %d, want %d", got, want)
	}
	if got := snapshot.Frames[0].Flow; got != "main" {
		t.Fatalf("top frame flow = %q, want main", got)
	}
	if got := snapshot.Frames[0].NextStep; got != 3 {
		t.Fatalf("top frame next step = %d, want 3", got)
	}

	final := snapshot.Steps[len(snapshot.Steps)-1].Value.(map[string]any)
	if got := intValue(t, final["n"]); got != 0 {
		t.Fatalf("final n = %d, want 0", got)
	}
	if got := intValue(t, final["sum"]); got != 6 {
		t.Fatalf("final sum = %d, want 6", got)
	}

	callResult := snapshot.Steps[len(snapshot.Steps)-2]
	if callResult.Type != "call" {
		t.Fatalf("call result type = %q, want call", callResult.Type)
	}
	if got := callResult.Value.(map[string]any)["flow"]; got != "loop" {
		t.Fatalf("call flow = %#v, want loop", got)
	}
}

func TestRunnerCallEmptyFlowReturnsNoNestedOutput(t *testing.T) {
	t.Parallel()

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Flows: map[string]spec.Flow{
			"main": {
				Steps: []spec.Step{
					{
						ID: "seed",
						Fields: map[string]any{
							"expr": map[string]any{
								"carry": "value",
							},
						},
					},
					{
						ID:   "empty",
						Type: "call",
						Fields: map[string]any{
							"flow": "empty",
						},
					},
				},
			},
			"empty": {},
		},
	})

	if err := PersistRunSpec(config); err != nil {
		t.Fatalf("persist run spec: %v", err)
	}

	snapshot, err := NewRunner(nil).Run(context.Background(), config)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	value := snapshot.Steps[1].Value.(map[string]any)
	if got := value["flow"]; got != "empty" {
		t.Fatalf("call flow = %#v, want empty", got)
	}
	if got := value["status"]; got != string(state.StepStatusSucceeded) {
		t.Fatalf("call status = %#v, want succeeded", got)
	}
	if got := value["value"]; got != nil {
		t.Fatalf("call value = %#v, want nil", got)
	}
}

func TestRunnerCtxPrevIDIsNotAvailableToExpressions(t *testing.T) {
	t.Parallel()

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Flows: map[string]spec.Flow{
			"main": {
				Steps: []spec.Step{
					{
						ID: "seed",
						Fields: map[string]any{
							"expr": "value",
						},
					},
					{
						ID: "read-id",
						Fields: map[string]any{
							"expr": `$.prev.id`,
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
	var failedErr RunFailedError
	if !errors.As(err, &failedErr) {
		t.Fatalf("run error = %v, want RunFailedError", err)
	}
	if failedErr.Failure.Code != "invalid_expr" {
		t.Fatalf("failure code = %q, want invalid_expr", failedErr.Failure.Code)
	}
	if !strings.Contains(failedErr.Failure.Message, "id") {
		t.Fatalf("failure message = %q, want id lookup failure", failedErr.Failure.Message)
	}
	if got := snapshot.Steps[1].Status; got != state.StepStatusFailed {
		t.Fatalf("step status = %q, want failed", got)
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

func TestRunnerResponseFromPublishesValidatedValue(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		response map[string]any
		result   state.StepResult
		want     string
	}{
		{
			name: "executor value",
			response: map[string]any{
				"schema": map[string]any{"$ref": "#/schemas/selected_value"},
			},
			result: state.StepResult{
				Status: state.StepStatusSucceeded,
				Value: map[string]any{
					"selected": "value",
				},
				Artifacts: executorapi.EmptyArtifacts(),
			},
			want: "value",
		},
		{
			name: "stdout artifact",
			response: map[string]any{
				"from":   "stdout",
				"schema": map[string]any{"$ref": "#/schemas/selected_value"},
			},
			result: state.StepResult{
				Status: state.StepStatusSucceeded,
				Value:  map[string]any{"selected": "ignored"},
				Artifacts: state.Artifacts{
					Stdout: "__stdout__",
					Files:  map[string]string{},
				},
			},
			want: "stdout",
		},
		{
			name: "stderr artifact",
			response: map[string]any{
				"from":   "stderr",
				"schema": map[string]any{"$ref": "#/schemas/selected_value"},
			},
			result: state.StepResult{
				Status: state.StepStatusSucceeded,
				Value:  map[string]any{"selected": "ignored"},
				Artifacts: state.Artifacts{
					Stderr: "__stderr__",
					Files:  map[string]string{},
				},
			},
			want: "stderr",
		},
		{
			name: "named artifact",
			response: map[string]any{
				"from":   "artifact:report",
				"schema": map[string]any{"$ref": "#/schemas/selected_value"},
			},
			result: state.StepResult{
				Status: state.StepStatusSucceeded,
				Value:  map[string]any{"selected": "ignored"},
				Artifacts: state.Artifacts{
					Files: map[string]string{
						"report": "__report__",
					},
				},
			},
			want: "report",
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
				Schemas: map[string]any{
					"selected_value": map[string]any{
						"type":                 "object",
						"required":             []any{"selected"},
						"additionalProperties": false,
						"properties": map[string]any{
							"selected": "string",
						},
					},
				},
				Flows: map[string]spec.Flow{
					"main": {
						Steps: []spec.Step{
							{
								ID:   "resolve",
								Type: "fake",
								Fields: map[string]any{
									"response": testCase.response,
								},
							},
							{
								ID:   "consume",
								Type: "expr",
								Fields: map[string]any{
									"expr": `$.prev.value["selected"]`,
								},
							},
						},
					},
				},
			})

			testResult := cloneStepResult(testCase.result)
			switch testCase.name {
			case "stdout artifact":
				testResult.Artifacts.Stdout = writeArtifactFixture(t, config.RunDir, "stdout.json", `{"selected":"stdout"}`)
			case "stderr artifact":
				testResult.Artifacts.Stderr = writeArtifactFixture(t, config.RunDir, "stderr.json", `{"selected":"stderr"}`)
			case "named artifact":
				testResult.Artifacts.Files["report"] = writeArtifactFixture(t, config.RunDir, "report.json", `{"selected":"report"}`)
			}

			if err := PersistRunSpec(config); err != nil {
				t.Fatalf("persist run spec: %v", err)
			}

			registry := builtinRegistry()
			mustRegister(registry, "fake", func() executorapi.Executor {
				return &fakeExecutor{
					calls: new([]string),
					execute: func(executorapi.StepContext) state.StepResult {
						return cloneStepResult(testResult)
					},
				}
			})

			snapshot, err := NewRunner(registry).Run(context.Background(), config)
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if got := snapshot.Steps[0].Value.(map[string]any)["selected"]; got != testCase.want {
				t.Fatalf("resolved step value = %#v, want %q", got, testCase.want)
			}
			if got := snapshot.Steps[1].Value; got != testCase.want {
				t.Fatalf("downstream value = %#v, want %q", got, testCase.want)
			}
		})
	}
}

func TestRunnerExposesSpecPathAndDirInRuntimeContext(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	specDir := filepath.Join(baseDir, "bundle")
	workspaceRoot := filepath.Join(baseDir, "repo")
	stateDir := filepath.Join(workspaceRoot, ".amata")
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		t.Fatalf("mkdir spec dir: %v", err)
	}
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("mkdir workspace root: %v", err)
	}

	helperPath := filepath.Join(specDir, "helper.txt")
	if err := os.WriteFile(helperPath, []byte("bundle helper"), 0o644); err != nil {
		t.Fatalf("write helper file: %v", err)
	}

	config := Config{
		RunID:    "run-001",
		RunDir:   filepath.Join(stateDir, "runs", "run-001"),
		SpecPath: filepath.Join(specDir, "workflow.yaml"),
		Workspace: workspace.Config{
			Root:     workspaceRoot,
			StateDir: stateDir,
		},
		Spec: spec.Document{
			Version: spec.Version,
			Name:    "sample",
			Entry:   "main",
			Flows: map[string]spec.Flow{
				"main": {
					Steps: []spec.Step{
						{
							ID: "spec-dir",
							Fields: map[string]any{
								"expr": "$.spec.dir",
							},
						},
						{
							ID: "spec-path",
							Fields: map[string]any{
								"expr": "$.spec.path",
							},
						},
						{
							ID: "read-helper",
							Fields: map[string]any{
								"command": []any{
									"sh",
									"-lc",
									"cat '{{ ctx.spec.dir }}/helper.txt'",
								},
							},
						},
					},
				},
			},
		},
	}

	if err := PersistRunSpec(config); err != nil {
		t.Fatalf("persist run spec: %v", err)
	}

	snapshot, err := NewRunner(nil).Run(context.Background(), config)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := snapshot.Steps[0].Value; got != specDir {
		t.Fatalf("spec dir = %#v, want %q", got, specDir)
	}
	if got := snapshot.Steps[1].Value; got != config.SpecPath {
		t.Fatalf("spec path = %#v, want %q", got, config.SpecPath)
	}
	if got := strings.TrimSpace(readFile(t, snapshot.Steps[2].Artifacts.Stdout)); got != "bundle helper" {
		t.Fatalf("helper stdout = %q, want bundle helper", got)
	}
}

func TestRunnerNormalizesPreviousTypedSlicesForExpressions(t *testing.T) {
	t.Parallel()

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Flows: map[string]spec.Flow{
			"main": {
				Steps: []spec.Step{
					{ID: "typed-slice", Type: "fake"},
					{ID: "read-path", Type: "fake"},
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
			execute: func(ctx executorapi.StepContext) state.StepResult {
				if ctx.Step.ID == "read-path" {
					value, err := ctx.Runtime.Resolve(map[string]any{
						"expr": `ctx.prev.value["paths"][0]`,
					})
					if err != nil {
						return state.StepResult{
							Status: state.StepStatusFailed,
							Error: &state.Failure{
								Code:    "resolve_failed",
								Message: err.Error(),
							},
						}
					}
					return state.StepResult{
						Status: state.StepStatusSucceeded,
						Value:  value,
					}
				}

				return state.StepResult{
					Status: state.StepStatusSucceeded,
					Value: map[string]any{
						"paths": []string{"alpha", "beta"},
					},
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
	if got := snapshot.Steps[1].Value; got != "alpha" {
		t.Fatalf("expr value = %#v, want %q", got, "alpha")
	}
}

func TestRunnerResponseSchemaMismatchFailsStructurally(t *testing.T) {
	t.Parallel()

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Schemas: map[string]any{
			"selected_value": map[string]any{
				"type":                 "object",
				"required":             []any{"selected"},
				"additionalProperties": false,
				"properties": map[string]any{
					"selected": "string",
				},
			},
		},
		Flows: map[string]spec.Flow{
			"main": {
				Steps: []spec.Step{
					{
						ID:   "resolve",
						Type: "fake",
						Fields: map[string]any{
							"response": map[string]any{
								"schema": map[string]any{"$ref": "#/schemas/selected_value"},
							},
						},
					},
					{ID: "after", Type: "fake"},
				},
			},
		},
	})

	if err := PersistRunSpec(config); err != nil {
		t.Fatalf("persist run spec: %v", err)
	}

	calls := []string{}
	registry := builtinRegistry()
	mustRegister(registry, "fake", func() executorapi.Executor {
		return &fakeExecutor{
			calls: &calls,
			results: map[string]state.StepResult{
				"resolve": {
					Status: state.StepStatusSucceeded,
					Value: map[string]any{
						"selected": true,
					},
					Artifacts: executorapi.EmptyArtifacts(),
				},
				"after": {Status: state.StepStatusSucceeded},
			},
		}
	})

	snapshot, err := NewRunner(registry).Run(context.Background(), config)
	var failedErr RunFailedError
	if !errors.As(err, &failedErr) {
		t.Fatalf("run error = %v, want RunFailedError", err)
	}
	if failedErr.Failure.Code != "response_schema_mismatch" {
		t.Fatalf("failure code = %q, want response_schema_mismatch", failedErr.Failure.Code)
	}
	if got, want := calls, []string{"resolve"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %#v, want %#v", got, want)
	}
	if snapshot.Steps[0].Status != state.StepStatusFailed {
		t.Fatalf("resolve status = %q, want failed", snapshot.Steps[0].Status)
	}
}

func TestRunnerResponseInvalidSchemaFailsStructurally(t *testing.T) {
	t.Parallel()

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Flows: map[string]spec.Flow{
			"main": {
				Steps: []spec.Step{
					{
						ID:   "resolve",
						Type: "fake",
						Fields: map[string]any{
							"response": map[string]any{
								"schema": map[string]any{"$ref": "#/schemas/missing"},
							},
						},
					},
					{ID: "after", Type: "fake"},
				},
			},
		},
	})

	if err := PersistRunSpec(config); err != nil {
		t.Fatalf("persist run spec: %v", err)
	}

	calls := []string{}
	registry := builtinRegistry()
	mustRegister(registry, "fake", func() executorapi.Executor {
		return &fakeExecutor{
			calls: &calls,
			results: map[string]state.StepResult{
				"resolve": {
					Status:    state.StepStatusSucceeded,
					Value:     map[string]any{"selected": "value"},
					Artifacts: executorapi.EmptyArtifacts(),
				},
				"after": {Status: state.StepStatusSucceeded},
			},
		}
	})

	snapshot, err := NewRunner(registry).Run(context.Background(), config)
	var failedErr RunFailedError
	if !errors.As(err, &failedErr) {
		t.Fatalf("run error = %v, want RunFailedError", err)
	}
	if failedErr.Failure.Code != "invalid_response_schema" {
		t.Fatalf("failure code = %q, want invalid_response_schema", failedErr.Failure.Code)
	}
	if got, want := calls, []string{"resolve"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %#v, want %#v", got, want)
	}
	if snapshot.Steps[0].Status != state.StepStatusFailed {
		t.Fatalf("resolve status = %q, want failed", snapshot.Steps[0].Status)
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

func TestRunnerExpectFailureStopsRecursiveLoopOnCurrentStep(t *testing.T) {
	t.Parallel()

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Flows: map[string]spec.Flow{
			"main": {
				Steps: []spec.Step{
					{
						ID: "seed",
						Fields: map[string]any{
							"expr": map[string]any{
								"n":   2,
								"sum": 0,
							},
						},
					},
					{
						ID:   "loop",
						Type: "call",
						Fields: map[string]any{
							"flow": "loop",
						},
					},
					{
						ID: "after",
						Fields: map[string]any{
							"expr": "unreachable",
						},
					},
				},
			},
			"loop": {
				Steps: []spec.Step{
					{
						ID:   "branch",
						Type: "switch",
						Fields: map[string]any{
							"cases": []any{
								map[string]any{
									"when": map[string]any{"expr": `ctx.prev.value["n"] <= 0`},
									"steps": []spec.Step{
										{
											ID: "done",
											Fields: map[string]any{
												"expr": `$.prev.value`,
											},
										},
									},
								},
								map[string]any{
									"when": map[string]any{"expr": `ctx.prev.value["n"] > 0`},
									"steps": []spec.Step{
										{
											ID: "decrement",
											Fields: map[string]any{
												"expr": map[string]any{
													"n":   map[string]any{"expr": `ctx.prev.value["n"] - 1`},
													"sum": map[string]any{"expr": `ctx.prev.value["sum"] + ctx.prev.value["n"]`},
												},
												"expect": map[string]any{
													"expr": `ctx.value["n"] > 0`,
												},
											},
										},
										{
											ID:   "recurse",
											Type: "call",
											Fields: map[string]any{
												"flow": "loop",
											},
										},
									},
								},
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
	var failedErr RunFailedError
	if !errors.As(err, &failedErr) {
		t.Fatalf("run error = %v, want RunFailedError", err)
	}
	if failedErr.Failure.Code != "expectation_failed" {
		t.Fatalf("failure code = %q, want expectation_failed", failedErr.Failure.Code)
	}
	if snapshot.Status != state.RunStatusFailed {
		t.Fatalf("snapshot status = %q, want failed", snapshot.Status)
	}
	if got := snapshot.Steps[len(snapshot.Steps)-1].ID; got != "decrement" {
		t.Fatalf("failed step id = %q, want decrement", got)
	}
	if got := snapshot.Steps[len(snapshot.Steps)-1].Status; got != state.StepStatusFailed {
		t.Fatalf("failed step status = %q, want failed", got)
	}
	if got := intValue(t, snapshot.Steps[len(snapshot.Steps)-1].Value.(map[string]any)["n"]); got != 0 {
		t.Fatalf("failed step value n = %d, want 0", got)
	}
	for _, step := range snapshot.Steps {
		if step.ID == "recurse" || step.ID == "after" {
			t.Fatalf("unexpected step executed after failed expect: %q", step.ID)
		}
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

func assertProgressKindsAndSteps(t *testing.T, events []progress.Event, wantKinds []progress.EventKind, wantStepIDs []string) {
	t.Helper()

	if len(events) != len(wantKinds) {
		t.Fatalf("event count = %d, want %d", len(events), len(wantKinds))
	}
	if len(wantKinds) != len(wantStepIDs) {
		t.Fatalf("want step id count = %d, want kinds count = %d", len(wantStepIDs), len(wantKinds))
	}

	gotKinds := make([]progress.EventKind, 0, len(events))
	gotStepIDs := make([]string, 0, len(events))
	for _, event := range events {
		gotKinds = append(gotKinds, event.Kind)
		if event.Step == nil {
			gotStepIDs = append(gotStepIDs, "")
			continue
		}
		gotStepIDs = append(gotStepIDs, event.Step.ID)
	}

	if !slices.Equal(gotKinds, wantKinds) {
		t.Fatalf("event kinds = %#v, want %#v", gotKinds, wantKinds)
	}
	if !reflect.DeepEqual(gotStepIDs, wantStepIDs) {
		t.Fatalf("event step ids = %#v, want %#v", gotStepIDs, wantStepIDs)
	}
}

func cloneStepResult(result state.StepResult) state.StepResult {
	result.Artifacts = cloneArtifacts(result.Artifacts)
	result.Value = cloneJSONValue(result.Value)
	result.Error = cloneFailure(result.Error)
	return result
}

func cloneArtifacts(artifacts state.Artifacts) state.Artifacts {
	cloned := state.Artifacts{
		Stdout: artifacts.Stdout,
		Stderr: artifacts.Stderr,
	}
	if len(artifacts.Files) > 0 {
		cloned.Files = make(map[string]string, len(artifacts.Files))
		for name, path := range artifacts.Files {
			cloned.Files[name] = path
		}
	} else {
		cloned.Files = map[string]string{}
	}
	return cloned
}

func cloneFailure(failure *state.Failure) *state.Failure {
	if failure == nil {
		return nil
	}
	cloned := *failure
	return &cloned
}

func cloneJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		cloned := make(map[string]any, len(typed))
		for key, child := range typed {
			cloned[key] = cloneJSONValue(child)
		}
		return cloned
	case []any:
		cloned := make([]any, len(typed))
		for index, child := range typed {
			cloned[index] = cloneJSONValue(child)
		}
		return cloned
	default:
		return value
	}
}

func writeArtifactFixture(t *testing.T, runDir string, name string, contents string) string {
	t.Helper()

	path := filepath.Join(runDir, "fixtures", name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir fixture dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", name, err)
	}
	return path
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

func intValue(t *testing.T, value any) int64 {
	t.Helper()

	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	default:
		t.Fatalf("numeric value type = %T, want int-like", value)
		return 0
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
