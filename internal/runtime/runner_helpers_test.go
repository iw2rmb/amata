package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	executorapi "github.com/iw2rmb/amata/internal/executor"
	"github.com/iw2rmb/amata/internal/jsonutil"
	"github.com/iw2rmb/amata/internal/progress"
	"github.com/iw2rmb/amata/internal/spec"
	"github.com/iw2rmb/amata/internal/state"
	"github.com/iw2rmb/amata/internal/testutil"
	"github.com/iw2rmb/amata/internal/workspace"
)

var readFile = testutil.ReadFile

var writeFile = testutil.WriteFile

var initGitRepository = testutil.InitGitRepo

var runGit = testutil.RunGit

type fakeExecutor struct {
	calls              *[]string
	results            map[string]state.StepResult
	execute            func(executorapi.StepContext) state.StepResult
	executeWithContext func(context.Context, executorapi.StepContext) state.StepResult
}

func (e *fakeExecutor) Execute(execCtx context.Context, ctx executorapi.StepContext) state.StepResult {
	*e.calls = append(*e.calls, ctx.Step.ID)
	if e.executeWithContext != nil {
		return e.executeWithContext(execCtx, ctx)
	}
	if e.execute != nil {
		return e.execute(ctx)
	}
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

func mustPersist(t *testing.T, config Config) {
	t.Helper()

	if err := PersistRunSpec(config); err != nil {
		t.Fatalf("persist run spec: %v", err)
	}
}

func assertRunFailed(t *testing.T, err error, wantCode string) RunFailedError {
	t.Helper()

	var failed RunFailedError
	if !errors.As(err, &failed) {
		t.Fatalf("run error = %v, want RunFailedError", err)
	}
	if failed.Failure.Code != wantCode {
		t.Fatalf("failure code = %q, want %q", failed.Failure.Code, wantCode)
	}
	return failed
}

func blockingFakeExecutor(calls *[]string) *fakeExecutor {
	return &fakeExecutor{
		calls: calls,
		executeWithContext: func(execCtx context.Context, _ executorapi.StepContext) state.StepResult {
			<-execCtx.Done()
			return state.StepResult{
				Status: state.StepStatusFailed,
				Error: &state.Failure{
					Code:    "canceled",
					Message: "canceled",
				},
			}
		},
	}
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

func activeStepIDs(snapshot progress.Snapshot) []string {
	return progressStepIDs(snapshot.Active)
}

func completedStepIDs(snapshot progress.Snapshot) []string {
	return progressStepIDs(snapshot.Steps)
}

func progressStepIDs(steps []progress.Step) []string {
	ids := make([]string, 0, len(steps))
	for _, step := range steps {
		ids = append(ids, step.ID)
	}
	return ids
}

func cloneStepResult(result state.StepResult) state.StepResult {
	result.Artifacts = cloneArtifacts(result.Artifacts)
	result.Value = jsonutil.CloneValue(result.Value)
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

func sampleDoc(flows map[string]spec.Flow) spec.Document {
	return spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Flows:   flows,
	}
}

func sampleDocWithParams(params map[string]any, flows map[string]spec.Flow) spec.Document {
	return spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Params:  params,
		Flows:   flows,
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
