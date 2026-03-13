package state_test

import (
	"os"
	"strings"
	"testing"

	"auto/internal/state"
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

func readFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return string(data)
}
