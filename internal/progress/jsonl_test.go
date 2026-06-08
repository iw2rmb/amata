package progress

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestJSONLControllerCompactsRedundantOutput(t *testing.T) {
	t.Parallel()

	step := Step{
		Flow:      "main",
		Index:     0,
		ID:        "step-1",
		Type:      "expr",
		Status:    StepStatusRunning,
		Artifacts: Artifacts{},
	}
	agentStep := Step{
		Flow:   "main",
		Index:  1,
		ID:     "agent-1",
		Type:   "codex",
		Status: StepStatusSucceeded,
		Descriptor: &DescriptorData{
			PrimaryText:         "model",
			DetailText:          []string{"large prompt"},
			FinalSummaryDetails: []string{"model"},
		},
		Artifacts: Artifacts{},
	}

	testCases := []struct {
		name                            string
		event                           Event
		wantSnapshotKeys                []string
		wantDescriptorDetailTextOmitted bool
	}{
		{
			name: "step_started omits active and steps",
			event: Event{
				Kind: EventStepStarted,
				Step: &step,
				Snapshot: Snapshot{
					RunID:  "run-1",
					Status: RunStatusRunning,
					Active: []Step{step},
					Steps:  []Step{step},
				},
			},
			wantSnapshotKeys: []string{"run_id", "status"},
		},
		{
			name: "step_finished omits active and steps",
			event: Event{
				Kind: EventStepFinished,
				Step: &step,
				Snapshot: Snapshot{
					RunID:  "run-1",
					Status: RunStatusRunning,
					Active: []Step{step},
					Steps:  []Step{step},
				},
			},
			wantSnapshotKeys: []string{"run_id", "status"},
		},
		{
			name: "run_finished omits completed steps",
			event: Event{
				Kind: EventRunFinished,
				Snapshot: Snapshot{
					RunID:  "run-1",
					Status: RunStatusSucceeded,
					Active: []Step{step},
					Steps:  []Step{step},
				},
			},
			wantSnapshotKeys: []string{"run_id", "status"},
		},
		{
			name: "run_resumed keeps active steps",
			event: Event{
				Kind: EventRunResumed,
				Snapshot: Snapshot{
					RunID:  "run-1",
					Status: RunStatusRunning,
					Active: []Step{step},
					Steps:  []Step{step},
				},
			},
			wantSnapshotKeys: []string{"run_id", "status", "active", "steps"},
		},
		{
			name: "finished agent step omits repeated prompt detail",
			event: Event{
				Kind: EventStepFinished,
				Step: &agentStep,
				Snapshot: Snapshot{
					RunID:  "run-1",
					Status: RunStatusRunning,
					Active: []Step{agentStep},
					Steps:  []Step{agentStep},
				},
			},
			wantSnapshotKeys:                []string{"run_id", "status"},
			wantDescriptorDetailTextOmitted: true,
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var buffer bytes.Buffer
			controller := NewJSONLController(&buffer)
			controller.WriteProgress(testCase.event)

			var payload map[string]any
			line := strings.TrimSpace(buffer.String())
			if err := json.Unmarshal([]byte(line), &payload); err != nil {
				t.Fatalf("decode event line: %v", err)
			}

			snapshot, ok := payload["snapshot"].(map[string]any)
			if !ok {
				t.Fatalf("snapshot payload = %#v, want object", payload["snapshot"])
			}

			for _, key := range testCase.wantSnapshotKeys {
				if _, exists := snapshot[key]; !exists {
					t.Fatalf("snapshot missing key %q: %#v", key, snapshot)
				}
			}
			if testCase.event.Kind == EventStepStarted || testCase.event.Kind == EventStepFinished || testCase.event.Kind == EventRunFinished {
				if _, exists := snapshot["active"]; exists {
					t.Fatalf("snapshot.active = %#v, want omitted", snapshot["active"])
				}
				if _, exists := snapshot["steps"]; exists {
					t.Fatalf("snapshot.steps = %#v, want omitted", snapshot["steps"])
				}
			}
			if testCase.event.Step != nil && testCase.event.Step.Descriptor != nil {
				stepPayload, ok := payload["step"].(map[string]any)
				if !ok {
					t.Fatalf("step payload = %#v, want object", payload["step"])
				}
				descriptor, ok := stepPayload["descriptor"].(map[string]any)
				if !ok {
					t.Fatalf("descriptor payload = %#v, want object", stepPayload["descriptor"])
				}
				if testCase.wantDescriptorDetailTextOmitted {
					if _, exists := descriptor["detail_text"]; exists {
						t.Fatalf("descriptor.detail_text = %#v, want omitted", descriptor["detail_text"])
					}
				}
			}
		})
	}
}
