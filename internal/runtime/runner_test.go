package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	executorapi "github.com/iw2rmb/amata/internal/executor"
	"github.com/iw2rmb/amata/internal/spec"
	"github.com/iw2rmb/amata/internal/state"
)

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

	mustPersist(t, config)

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

func TestBuiltinRegistration(t *testing.T) {
	t.Parallel()

	registry := builtinRegistry()
	for _, name := range []string{"shell", "expr", "assert", "codex", "claude", "crush", "git.inspect", "git.commit"} {
		if _, ok := registry.Lookup(name); !ok {
			t.Errorf("builtin executor %q is not registered", name)
		}
	}
}

func TestRunnerRepeatedStepExecutionsUseDistinctArtifactDirectories(t *testing.T) {
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
							"expr": []any{"alpha", "beta"},
						},
					},
					{
						ID:   "loop",
						Type: "for_each",
						Fields: map[string]any{
							"items": `$.prev.value`,
							"steps": []spec.Step{
								{ID: "capture", Type: "fake"},
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
			execute: func(ctx executorapi.StepContext) state.StepResult {
				item, err := ctx.Runtime.Resolve("$.item")
				if err != nil {
					return state.StepResult{
						Status: state.StepStatusFailed,
						Error: &state.Failure{
							Code:    "resolve_failed",
							Message: err.Error(),
						},
					}
				}
				stepDir := executorapi.StepArtifactDir(ctx.RunDir, ctx.StepIndex, ctx.Step.ID, ctx.ExecutionLabel)
				if err := os.MkdirAll(stepDir, 0o755); err != nil {
					return state.StepResult{
						Status: state.StepStatusFailed,
						Error: &state.Failure{
							Code:    "artifact_dir_failed",
							Message: err.Error(),
						},
					}
				}
				stdoutPath := filepath.Join(stepDir, "stdout.txt")
				if err := os.WriteFile(stdoutPath, []byte(fmt.Sprint(item)), 0o644); err != nil {
					return state.StepResult{
						Status: state.StepStatusFailed,
						Error: &state.Failure{
							Code:    "artifact_write_failed",
							Message: err.Error(),
						},
					}
				}
				return state.StepResult{
					Status: state.StepStatusSucceeded,
					Value:  item,
					Artifacts: state.Artifacts{
						Stdout: stdoutPath,
						Files:  map[string]string{},
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

	bodySteps := []state.StepResult{}
	for _, step := range snapshot.Steps {
		if step.ID == "capture" {
			bodySteps = append(bodySteps, step)
		}
	}
	if len(bodySteps) != 2 {
		t.Fatalf("capture steps = %d, want 2", len(bodySteps))
	}
	if bodySteps[0].Artifacts.Stdout == bodySteps[1].Artifacts.Stdout {
		t.Fatalf("artifact paths = %#v, want distinct paths", []string{bodySteps[0].Artifacts.Stdout, bodySteps[1].Artifacts.Stdout})
	}
	for index, want := range []string{"alpha", "beta"} {
		if got := strings.TrimSpace(readFile(t, bodySteps[index].Artifacts.Stdout)); got != want {
			t.Fatalf("artifact[%d] = %q, want %q", index, got, want)
		}
	}
}
