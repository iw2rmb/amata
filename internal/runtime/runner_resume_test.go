package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	executorapi "github.com/iw2rmb/amata/internal/executor"
	"github.com/iw2rmb/amata/internal/progress"
	"github.com/iw2rmb/amata/internal/spec"
	"github.com/iw2rmb/amata/internal/state"
)

func TestRunnerResumeContinuesFromFirstIncompleteStep(t *testing.T) {
	t.Parallel()

	config := testConfig(t, sampleDoc(map[string]spec.Flow{
		"main": {
			Steps: []spec.Step{
				{ID: "step-1", Type: "fake"},
				{ID: "step-2", Type: "fake"},
				{ID: "step-3", Type: "fake"},
			},
		},
	}))

	mustPersist(t, config)

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

func TestRunnerResumeRestoresCtxPrevChain(t *testing.T) {
	t.Parallel()

	config := testConfig(t, sampleDoc(map[string]spec.Flow{
		"main": {
			Steps: []spec.Step{
				{ID: "step-1", Fields: map[string]any{"expr": "one"}},
				{ID: "step-2", Fields: map[string]any{"expr": "two"}},
				{
					ID: "step-3",
					Fields: map[string]any{
						"expr": map[string]any{
							"current": `$.prev.value`,
							"prior":   `$.prev.prev.value`,
						},
					},
				},
			},
		},
	}))

	mustPersist(t, config)

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
			Type:   "expr",
			Status: state.StepStatusSucceeded,
			Value:  "one",
		},
	}); err != nil {
		t.Fatalf("append step 1: %v", err)
	}
	if _, err := store.Append(state.RunEvent{
		Kind: state.EventStepRecorded,
		Step: &state.StepResult{
			Index:  1,
			ID:     "step-2",
			Type:   "expr",
			Status: state.StepStatusSucceeded,
			Value:  "two",
		},
	}); err != nil {
		t.Fatalf("append step 2: %v", err)
	}

	resumeConfig, err := LoadRunConfig(config.RunDir)
	if err != nil {
		t.Fatalf("load run config: %v", err)
	}

	snapshot, err := NewRunner(nil).Resume(context.Background(), resumeConfig)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}

	value := snapshot.Steps[2].Value.(map[string]any)
	if got := value["current"]; got != "two" {
		t.Fatalf("current = %#v, want two", got)
	}
	if got := value["prior"]; got != "one" {
		t.Fatalf("prior = %#v, want one", got)
	}
}

func TestRunnerResumeFinalizesCompletedChildFrameBeforeRunningParentNextStep(t *testing.T) {
	t.Parallel()

	config := testConfig(t, sampleDoc(map[string]spec.Flow{
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
	}))

	mustPersist(t, config)

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
	registry := builtinRegistry()
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

func TestRunnerResumeEmitsLiveProgressForReturnedControl(t *testing.T) {
	t.Parallel()

	config := testConfig(t, sampleDoc(map[string]spec.Flow{
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
	}))

	mustPersist(t, config)

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

	registry := builtinRegistry()
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
	if got := events[0].Snapshot.Active[0].Descriptor; got == nil || got.PrimaryText != "loop" {
		t.Fatalf("resume active step descriptor = %#v, want loop target flow", got)
	}
	if got := events[1].Step.Descriptor; got == nil || !reflect.DeepEqual(got.FinalSummaryDetails, []string{"loop"}) {
		t.Fatalf("returned control descriptor = %#v, want flow summary", got)
	}
	if got := len(events[1].Snapshot.Active); got != 0 {
		t.Fatalf("active step count after returned control = %d, want 0", got)
	}
}

func TestRunnerResumeFinalizesDurableFailureWithoutRunningLaterSteps(t *testing.T) {
	t.Parallel()

	config := testConfig(t, sampleDoc(map[string]spec.Flow{
		"main": {
			Steps: []spec.Step{
				{ID: "step-1", Type: "fake"},
				{ID: "step-2", Type: "fake"},
			},
		},
	}))

	mustPersist(t, config)

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
	var resumeFailed RunFailedError
	if !errors.As(err, &resumeFailed) {
		t.Fatalf("resume error = %v, want RunFailedError", err)
	}
	if got := calls; len(got) != 0 {
		t.Fatalf("resume executed steps = %#v, want none", got)
	}
}

func TestRunnerResumeKeepsTerminalFailureState(t *testing.T) {
	t.Parallel()

	config := testConfig(t, sampleDoc(map[string]spec.Flow{
		"main": {
			Steps: []spec.Step{
				{ID: "step-1", Type: "fake"},
			},
		},
	}))

	mustPersist(t, config)

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

			config := testConfig(t, sampleDoc(map[string]spec.Flow{
				"main": {
					Steps: []spec.Step{
						{ID: "step-1", Type: "fake"},
						{ID: "step-2", Type: "fake"},
						{ID: "step-3", Type: "fake"},
					},
				},
			}))

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
				assertRunFailed(t, err, testCase.wantFailureCode)
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
				assertRunFailed(t, err, testCase.wantFailureCode)
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
