package runtime

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/iw2rmb/amata/internal/progress"
	"github.com/iw2rmb/amata/internal/spec"
	"github.com/iw2rmb/amata/internal/state"

	executorapi "github.com/iw2rmb/amata/internal/executor"
)

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

	mustPersist(t, config)

	registry := builtinRegistry()
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
	if got, want := activeStepIDs(events[1].Snapshot), []string{"switch-step"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("active steps after switch start = %#v, want %#v", got, want)
	}
	if got, want := activeStepIDs(events[2].Snapshot), []string{"switch-step", "branch-step"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("active steps while switch branch runs = %#v, want %#v", got, want)
	}
	if got, want := completedStepIDs(events[3].Snapshot), []string{"branch-step"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("completed steps after branch finish = %#v, want %#v", got, want)
	}
	if got, want := completedStepIDs(events[4].Snapshot), []string{"branch-step", "switch-step"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("completed steps after switch finish = %#v, want %#v", got, want)
	}
	if got, want := activeStepIDs(events[5].Snapshot), []string{"call-step"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("active steps after call start = %#v, want %#v", got, want)
	}
	if got, want := activeStepIDs(events[6].Snapshot), []string{"call-step", "child-step"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("active steps while child flow runs = %#v, want %#v", got, want)
	}
	if got, want := completedStepIDs(events[7].Snapshot), []string{"branch-step", "switch-step", "child-step"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("completed steps after child finish = %#v, want %#v", got, want)
	}
	if got, want := completedStepIDs(events[8].Snapshot), []string{"branch-step", "switch-step", "child-step", "call-step"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("completed steps after call finish = %#v, want %#v", got, want)
	}
}

func TestRunnerLiveProgressIncludesStepDescriptors(t *testing.T) {
	t.Parallel()

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Flows: map[string]spec.Flow{
			"main": {
				Steps: []spec.Step{
					{
						ID:   "shell-step",
						Type: "shell",
						Fields: map[string]any{
							"command": []any{"echo", "descriptor"},
						},
					},
				},
			},
		},
	})

	mustPersist(t, config)

	var events []progress.Event
	sink := progress.SinkFunc(func(event progress.Event) {
		events = append(events, event)
	})

	_, err := NewRunner(nil, WithRunnerProgressSink(sink)).Run(context.Background(), config)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if got := events[1].Step.Descriptor; got == nil || got.PrimaryText != "echo descriptor" {
		t.Fatalf("started step descriptor = %#v, want primary text", got)
	}
	if got := events[2].Step.Descriptor; got == nil || !reflect.DeepEqual(got.FinalSummaryDetails, []string{"exit 0"}) {
		t.Fatalf("finished step descriptor = %#v, want exit summary", got)
	}
}

func TestRunnerLiveProgressIncludesGitCommitCompletedLineSummary(t *testing.T) {
	t.Parallel()

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Flows: map[string]spec.Flow{
			"main": {
				Steps: []spec.Step{
					{
						ID:   "commit-step",
						Type: "git.commit",
						Fields: map[string]any{
							"message": "test commit",
						},
					},
				},
			},
		},
	})

	initGitRepository(t, config.Workspace.Root)
	writeFile(t, filepath.Join(config.Workspace.Root, "note.txt"), "before\n")
	runGit(t, config.Workspace.Root, "add", "note.txt")
	runGit(t, config.Workspace.Root, "commit", "-m", "init")
	writeFile(t, filepath.Join(config.Workspace.Root, "note.txt"), "before\nafter\n")

	mustPersist(t, config)

	var events []progress.Event
	sink := progress.SinkFunc(func(event progress.Event) {
		events = append(events, event)
	})

	_, err := NewRunner(nil, WithRunnerProgressSink(sink)).Run(context.Background(), config)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	assertProgressKindsAndSteps(t, events,
		[]progress.EventKind{
			progress.EventRunStarted,
			progress.EventStepStarted,
			progress.EventStepFinished,
			progress.EventRunFinished,
		},
		[]string{
			"",
			"commit-step",
			"commit-step",
			"",
		},
	)

	if got := events[1].Step.Descriptor; got == nil || !reflect.DeepEqual(got.DetailText, []string{"test commit"}) {
		t.Fatalf("started git.commit descriptor = %#v, want commit message detail", got)
	}

	finished := events[2].Step
	if finished == nil || finished.Descriptor == nil {
		t.Fatalf("finished git.commit descriptor missing: %#v", finished)
	}
	metadata, ok := mapValueField(finished.Value, "metadata")
	if !ok {
		t.Fatalf("git.commit value metadata missing: %#v", finished.Value)
	}
	shortCommit, ok := stringValueField(metadata, "shortCommit")
	if !ok || shortCommit == "" {
		t.Fatalf("git.commit shortCommit missing: %#v", metadata)
	}
	if got, want := finished.Descriptor.FinalSummaryDetails, []string{shortCommit, "files 1 +1 -0"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("finished git.commit summary = %#v, want %#v", got, want)
	}
	if got, want := finished.Descriptor.DetailText, []string{"test commit", "+1 -0 note.txt"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("finished git.commit details = %#v, want %#v", got, want)
	}
	if got, want := completedStepIDs(events[2].Snapshot), []string{"commit-step"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("completed steps after git.commit = %#v, want %#v", got, want)
	}
}
