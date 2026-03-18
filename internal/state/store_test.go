package state_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iw2rmb/amata/internal/state"
)

func TestStoreAppendsEventsImmutablyAndRebuildsSnapshot(t *testing.T) {
	t.Parallel()

	runDir := t.TempDir()
	store := state.NewStore(runDir)

	if _, err := store.Append(state.RunEvent{
		Kind: state.EventRunInitialized,
		Frame: &state.FlowFrame{
			Flow:      "main",
			StepCount: 3,
		},
		Command: "run",
	}); err != nil {
		t.Fatalf("append init event: %v", err)
	}

	initialEvents := readFile(t, store.EventsPath())

	if _, err := store.Append(state.RunEvent{
		Kind: state.EventStepRecorded,
		Step: &state.StepResult{
			Index:  0,
			ID:     "first",
			Type:   "fake",
			Status: state.StepStatusSucceeded,
		},
	}); err != nil {
		t.Fatalf("append step 0 event: %v", err)
	}

	if _, err := store.Append(state.RunEvent{
		Kind: state.EventStepRecorded,
		Step: &state.StepResult{
			Index:  1,
			ID:     "second",
			Type:   "fake",
			Status: state.StepStatusFailed,
			Error: &state.Failure{
				Code:    "boom",
				Message: "boom",
			},
		},
	}); err != nil {
		t.Fatalf("append step 1 event: %v", err)
	}

	if _, err := store.Append(state.RunEvent{
		Kind:   state.EventRunFinished,
		Status: state.RunStatusFailed,
		Failure: &state.Failure{
			Code:    "boom",
			Message: "boom",
		},
	}); err != nil {
		t.Fatalf("append final event: %v", err)
	}

	allEvents := readFile(t, store.EventsPath())
	if !strings.HasPrefix(allEvents, initialEvents) {
		t.Fatalf("events log was rewritten instead of appended")
	}
	if got := strings.Count(strings.TrimSpace(allEvents), "\n") + 1; got != 4 {
		t.Fatalf("event count = %d, want 4", got)
	}

	if err := os.Remove(store.SnapshotPath()); err != nil {
		t.Fatalf("remove snapshot: %v", err)
	}

	snapshot, err := store.LoadSnapshot()
	if err != nil {
		t.Fatalf("load rebuilt snapshot: %v", err)
	}

	if snapshot.Status != state.RunStatusFailed {
		t.Fatalf("snapshot.status = %q, want failed", snapshot.Status)
	}
	if len(snapshot.Frames) != 1 {
		t.Fatalf("frame count = %d, want 1", len(snapshot.Frames))
	}
	if snapshot.Frames[0].NextStep != 2 {
		t.Fatalf("next step = %d, want 2", snapshot.Frames[0].NextStep)
	}
	if len(snapshot.Steps) != 2 {
		t.Fatalf("step count = %d, want 2", len(snapshot.Steps))
	}
	if snapshot.Steps[1].Status != state.StepStatusFailed {
		t.Fatalf("step 1 status = %q, want failed", snapshot.Steps[1].Status)
	}
	if snapshot.Failure == nil || snapshot.Failure.Code != "boom" {
		t.Fatalf("snapshot failure = %#v, want boom", snapshot.Failure)
	}
}

func TestStoreRebuildsSnapshotWhenSnapshotFileIsCorrupt(t *testing.T) {
	t.Parallel()

	runDir := t.TempDir()
	store := state.NewStore(runDir)

	if _, err := store.Append(state.RunEvent{
		Kind: state.EventRunInitialized,
		Frame: &state.FlowFrame{
			Flow:      "main",
			StepCount: 1,
		},
	}); err != nil {
		t.Fatalf("append init event: %v", err)
	}
	if _, err := store.Append(state.RunEvent{
		Kind: state.EventStepRecorded,
		Step: &state.StepResult{
			Index:  0,
			ID:     "only",
			Type:   "fake",
			Status: state.StepStatusSucceeded,
		},
	}); err != nil {
		t.Fatalf("append step event: %v", err)
	}

	if err := os.WriteFile(store.SnapshotPath(), []byte("{invalid"), 0o644); err != nil {
		t.Fatalf("corrupt snapshot: %v", err)
	}

	snapshot, err := store.LoadSnapshot()
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if snapshot.LastSequence != 2 {
		t.Fatalf("last sequence = %d, want 2", snapshot.LastSequence)
	}
	if snapshot.Frames[0].NextStep != 1 {
		t.Fatalf("next step = %d, want 1", snapshot.Frames[0].NextStep)
	}
}

func TestStoreAssignsFrameAndPreviousRefsToRecordedSteps(t *testing.T) {
	t.Parallel()

	runDir := t.TempDir()
	store := state.NewStore(runDir)

	if _, err := store.Append(state.RunEvent{
		Kind: state.EventRunInitialized,
		Frame: &state.FlowFrame{
			Flow:      "main",
			StepCount: 2,
		},
	}); err != nil {
		t.Fatalf("append init event: %v", err)
	}

	if _, err := store.Append(state.RunEvent{
		Kind: state.EventStepRecorded,
		Step: &state.StepResult{
			Index:  0,
			ID:     "first",
			Type:   "expr",
			Status: state.StepStatusSucceeded,
			Value:  "one",
		},
	}); err != nil {
		t.Fatalf("append first step event: %v", err)
	}

	snapshot, err := store.Append(state.RunEvent{
		Kind: state.EventStepRecorded,
		Step: &state.StepResult{
			Index:  1,
			ID:     "second",
			Type:   "expr",
			Status: state.StepStatusSucceeded,
			Value:  "two",
		},
	})
	if err != nil {
		t.Fatalf("append second step event: %v", err)
	}

	if snapshot.Frames[0].ID == "" {
		t.Fatalf("frame id missing")
	}
	if snapshot.Steps[0].FrameID != snapshot.Frames[0].ID {
		t.Fatalf("first step frame id = %q, want %q", snapshot.Steps[0].FrameID, snapshot.Frames[0].ID)
	}
	if snapshot.Steps[1].Previous == nil {
		t.Fatalf("second step previous ref missing")
	}
	if got := snapshot.Steps[1].Previous.Sequence; got != snapshot.Steps[0].Sequence {
		t.Fatalf("second step previous sequence = %d, want %d", got, snapshot.Steps[0].Sequence)
	}
}

func TestStoreControlContinuedReplacesCompletedChildFrame(t *testing.T) {
	t.Parallel()

	runDir := t.TempDir()
	store := state.NewStore(runDir)

	if _, err := store.Append(state.RunEvent{
		Kind: state.EventRunInitialized,
		Frame: &state.FlowFrame{
			Flow:      "main",
			StepCount: 1,
		},
	}); err != nil {
		t.Fatalf("append init event: %v", err)
	}

	if _, err := store.Append(state.RunEvent{
		Kind: state.EventFramePushed,
		Frame: &state.FlowFrame{
			Flow:      "@for_each:main:0",
			StepCount: 1,
			NextStep:  1,
			Return: &state.FrameReturn{
				StepType:  "for_each",
				StepIndex: 0,
				StepID:    "loop",
				ForEach: &state.ForEachState{
					Items: []any{"alpha", "beta"},
					Index: 0,
					As:    "folder",
				},
			},
		},
	}); err != nil {
		t.Fatalf("append frame event: %v", err)
	}

	snapshot, err := store.Append(state.RunEvent{
		Kind: state.EventControlContinued,
		Frame: &state.FlowFrame{
			Flow:      "@for_each:main:0",
			StepCount: 1,
			Bindings: map[string]any{
				"item":   "beta",
				"folder": "beta",
				"index":  1,
			},
			Return: &state.FrameReturn{
				StepType:  "for_each",
				StepIndex: 0,
				StepID:    "loop",
				ForEach: &state.ForEachState{
					Items: []any{"alpha", "beta"},
					Index: 1,
					As:    "folder",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("append control continued event: %v", err)
	}

	if got, want := len(snapshot.Frames), 2; got != want {
		t.Fatalf("frame count = %d, want %d", got, want)
	}
	if got := snapshot.Frames[1].Bindings["folder"]; got != "beta" {
		t.Fatalf("continued frame alias = %#v, want beta", got)
	}
	if got := snapshot.Frames[1].Return.ForEach.Index; got != 1 {
		t.Fatalf("continued frame index = %d, want 1", got)
	}
}

func TestStoreRejectsOutOfOrderStepEvents(t *testing.T) {
	t.Parallel()

	runDir := t.TempDir()
	store := state.NewStore(runDir)

	if _, err := store.Append(state.RunEvent{
		Kind: state.EventRunInitialized,
		Frame: &state.FlowFrame{
			Flow:      "main",
			StepCount: 2,
		},
	}); err != nil {
		t.Fatalf("append init event: %v", err)
	}

	if _, err := store.Append(state.RunEvent{
		Kind: state.EventStepRecorded,
		Step: &state.StepResult{
			Index:  1,
			ID:     "skipped-zero",
			Type:   "fake",
			Status: state.StepStatusSucceeded,
		},
	}); err == nil {
		t.Fatalf("expected out-of-order step append to fail")
	}
}

func TestStoreRebuildsSnapshotWithFailureArtifactsIntact(t *testing.T) {
	t.Parallel()

	runDir := t.TempDir()
	store := state.NewStore(runDir)

	preservedDir := filepath.Join(runDir, "artifacts", "step-02-failed")
	if err := os.MkdirAll(filepath.Join(preservedDir, "files"), 0o755); err != nil {
		t.Fatalf("mkdir preserved dir: %v", err)
	}
	paths := map[string]string{
		filepath.Join(preservedDir, "stdout.txt"):          "stdout",
		filepath.Join(preservedDir, "stderr.txt"):          "stderr",
		filepath.Join(preservedDir, "files", "report.txt"): "report",
	}
	for path, contents := range paths {
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	events := []state.RunEvent{
		{
			Kind: state.EventRunInitialized,
			Frame: &state.FlowFrame{
				Flow:      "main",
				StepCount: 3,
			},
			Command: "run",
		},
		{
			Kind: state.EventStepRecorded,
			Step: &state.StepResult{
				Index:  0,
				ID:     "step-1",
				Type:   "fake",
				Status: state.StepStatusSucceeded,
			},
		},
		{
			Kind: state.EventStepRecorded,
			Step: &state.StepResult{
				Index:  1,
				ID:     "step-2",
				Type:   "fake",
				Status: state.StepStatusSkipped,
			},
		},
		{
			Kind: state.EventStepRecorded,
			Step: &state.StepResult{
				Index:  2,
				ID:     "step-3",
				Type:   "fake",
				Status: state.StepStatusFailed,
				Error: &state.Failure{
					Code:    "boom",
					Message: "boom",
				},
				Artifacts: state.Artifacts{
					Stdout: filepath.Join(preservedDir, "stdout.txt"),
					Stderr: filepath.Join(preservedDir, "stderr.txt"),
					Files: map[string]string{
						"report": filepath.Join(preservedDir, "files", "report.txt"),
					},
				},
			},
		},
		{
			Kind:   state.EventRunFinished,
			Status: state.RunStatusFailed,
			Failure: &state.Failure{
				Code:    "boom",
				Message: "boom",
			},
		},
	}

	for _, event := range events {
		if _, err := store.Append(event); err != nil {
			t.Fatalf("append event %q: %v", event.Kind, err)
		}
	}

	if err := os.Remove(store.SnapshotPath()); err != nil {
		t.Fatalf("remove snapshot: %v", err)
	}

	snapshot, err := store.LoadSnapshot()
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if snapshot.Status != state.RunStatusFailed {
		t.Fatalf("snapshot.status = %q, want failed", snapshot.Status)
	}
	failedStep := snapshot.Steps[2]
	if failedStep.Error == nil || failedStep.Error.Code != "boom" {
		t.Fatalf("failed step error = %#v, want boom", failedStep.Error)
	}
	if failedStep.Artifacts.Stdout != filepath.Join(preservedDir, "stdout.txt") {
		t.Fatalf("stdout path = %q, want preserved stdout", failedStep.Artifacts.Stdout)
	}
	if failedStep.Artifacts.Files["report"] != filepath.Join(preservedDir, "files", "report.txt") {
		t.Fatalf("report path = %q, want preserved report", failedStep.Artifacts.Files["report"])
	}
}

func TestStoreRebuildsNestedFlowFrames(t *testing.T) {
	t.Parallel()

	runDir := t.TempDir()
	store := state.NewStore(runDir)

	parentPrev := &state.StepResult{
		Index:  0,
		ID:     "seed",
		Type:   "expr",
		Status: state.StepStatusSucceeded,
		Value: map[string]any{
			"n": 2,
		},
		Artifacts: state.Artifacts{Files: map[string]string{}},
	}

	events := []state.RunEvent{
		{
			Kind: state.EventRunInitialized,
			Frame: &state.FlowFrame{
				Flow:      "main",
				StepCount: 2,
			},
			Command: "run",
		},
		{
			Kind: state.EventStepRecorded,
			Step: parentPrev,
		},
		{
			Kind: state.EventFramePushed,
			Frame: &state.FlowFrame{
				Flow:      "child",
				StepCount: 1,
				Previous:  state.StepRefFor(*parentPrev),
				Return: &state.FrameReturn{
					StepType:  "call",
					StepIndex: 1,
					StepID:    "recurse",
					Flow:      "child",
				},
			},
		},
		{
			Kind: state.EventStepRecorded,
			Step: &state.StepResult{
				Index:  0,
				ID:     "child-step",
				Type:   "expr",
				Status: state.StepStatusSucceeded,
				Value: map[string]any{
					"n": 1,
				},
				Artifacts: state.Artifacts{Files: map[string]string{}},
			},
		},
		{
			Kind: state.EventControlReturned,
			Step: &state.StepResult{
				Index:  1,
				ID:     "recurse",
				Type:   "call",
				Status: state.StepStatusSucceeded,
				Value: map[string]any{
					"flow":  "child",
					"value": map[string]any{"n": 1},
				},
				Artifacts: state.Artifacts{Files: map[string]string{}},
			},
		},
	}

	for _, event := range events {
		if _, err := store.Append(event); err != nil {
			t.Fatalf("append event %q: %v", event.Kind, err)
		}
	}

	if err := os.Remove(store.SnapshotPath()); err != nil {
		t.Fatalf("remove snapshot: %v", err)
	}

	snapshot, err := store.LoadSnapshot()
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if got, want := len(snapshot.Frames), 1; got != want {
		t.Fatalf("frame count = %d, want %d", got, want)
	}
	if got := snapshot.Frames[0].NextStep; got != 2 {
		t.Fatalf("next step = %d, want 2", got)
	}
	if snapshot.Frames[0].Previous == nil {
		t.Fatalf("top frame previous missing")
	}
	framePrevious := snapshot.StepByRef(snapshot.Frames[0].Previous)
	if framePrevious == nil {
		t.Fatalf("top frame previous step missing")
	}
	if got := framePrevious.Type; got != "call" {
		t.Fatalf("top frame previous type = %q, want call", got)
	}
	if got := framePrevious.Value.(map[string]any)["flow"]; got != "child" {
		t.Fatalf("top frame previous flow = %#v, want child", got)
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
