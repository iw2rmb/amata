package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
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
						ID: "skip-expr",
						Fields: map[string]any{
							"expr": "ignored",
							"when": false,
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
	if snapshot.Steps[1].Status != state.StepStatusSkipped {
		t.Fatalf("skip-expr status = %q, want skipped", snapshot.Steps[1].Status)
	}
	if snapshot.Steps[2].Status != state.StepStatusSkipped {
		t.Fatalf("skip-assert status = %q, want skipped", snapshot.Steps[2].Status)
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

type fakeExecutor struct {
	calls   *[]string
	results map[string]state.StepResult
}

func (e *fakeExecutor) Execute(_ context.Context, ctx executorapi.StepContext) state.StepResult {
	*e.calls = append(*e.calls, ctx.Step.ID)
	if result, ok := e.results[ctx.Step.ID]; ok {
		return result
	}
	return state.StepResult{Status: state.StepStatusSucceeded}
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
