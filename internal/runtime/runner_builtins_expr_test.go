package runtime

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/iw2rmb/amata/internal/spec"
	"github.com/iw2rmb/amata/internal/state"
)

func TestRunnerBuiltinsExprAndWhenSkip(t *testing.T) {
	t.Parallel()

	config := testConfig(t, sampleDoc(map[string]spec.Flow{
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
	}))

	mustPersist(t, config)

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

func TestRunnerCtxPrevChainTraversesSucceededStepsOnly(t *testing.T) {
	t.Parallel()

	config := testConfig(t, sampleDoc(map[string]spec.Flow{
		"main": {
			Steps: []spec.Step{
				{
					ID: "first",
					Fields: map[string]any{
						"expr": map[string]any{"n": 1},
					},
				},
				{
					ID: "second",
					Fields: map[string]any{
						"expr": map[string]any{"n": 2},
					},
				},
				{
					ID: "skip",
					Fields: map[string]any{
						"expr": "ignored",
						"when": false,
					},
				},
				{
					ID: "read-chain",
					Fields: map[string]any{
						"expr": map[string]any{
							"current": `$.prev.value["n"]`,
							"prior":   `$.prev.prev.value["n"]`,
						},
					},
				},
			},
		},
	}))

	mustPersist(t, config)

	snapshot, err := NewRunner(nil).Run(context.Background(), config)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	value := snapshot.Steps[3].Value.(map[string]any)
	if got := intValue(t, value["current"]); got != 2 {
		t.Fatalf("current = %d, want 2", got)
	}
	if got := intValue(t, value["prior"]); got != 1 {
		t.Fatalf("prior = %d, want 1", got)
	}
}

func TestRunnerCtxPrevChainInheritsIntoChildFlow(t *testing.T) {
	t.Parallel()

	config := testConfig(t, sampleDoc(map[string]spec.Flow{
		"main": {
			Steps: []spec.Step{
				{ID: "first", Fields: map[string]any{"expr": "one"}},
				{ID: "second", Fields: map[string]any{"expr": "two"}},
				{
					ID:   "child",
					Type: "call",
					Fields: map[string]any{
						"flow": "child",
					},
				},
			},
		},
		"child": {
			Steps: []spec.Step{
				{
					ID: "read",
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

	snapshot, err := NewRunner(nil).Run(context.Background(), config)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	childStepIndex := -1
	for index, step := range snapshot.Steps {
		if step.ID == "read" {
			childStepIndex = index
			break
		}
	}
	if childStepIndex < 0 {
		t.Fatalf("child step not found in snapshot")
	}

	value := snapshot.Steps[childStepIndex].Value.(map[string]any)
	if got := value["current"]; got != "two" {
		t.Fatalf("current = %#v, want two", got)
	}
	if got := value["prior"]; got != "one" {
		t.Fatalf("prior = %#v, want one", got)
	}
}

func TestRunnerCtxPrevIDIsNotAvailableToExpressions(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name            string
		steps           []spec.Step
		failedStepIndex int
	}{
		{
			name: "direct prev.id",
			steps: []spec.Step{
				{ID: "seed", Fields: map[string]any{"expr": "value"}},
				{ID: "read-id", Fields: map[string]any{"expr": `$.prev.id`}},
			},
			failedStepIndex: 1,
		},
		{
			name: "nested prev.prev.id",
			steps: []spec.Step{
				{ID: "first", Fields: map[string]any{"expr": "one"}},
				{ID: "second", Fields: map[string]any{"expr": "two"}},
				{ID: "read-id", Fields: map[string]any{"expr": `$.prev.prev.id`}},
			},
			failedStepIndex: 2,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			config := testConfig(t, sampleDoc(map[string]spec.Flow{
				"main": {Steps: tc.steps},
			}))
			mustPersist(t, config)

			snapshot, err := NewRunner(nil).Run(context.Background(), config)
			failed := assertRunFailed(t, err, "invalid_expr")
			if !strings.Contains(failed.Failure.Message, "id") {
				t.Fatalf("failure message = %q, want id lookup failure", failed.Failure.Message)
			}
			if got := snapshot.Steps[tc.failedStepIndex].Status; got != state.StepStatusFailed {
				t.Fatalf("step status = %q, want failed", got)
			}
		})
	}
}

func TestRunnerBuiltinAssertFailsStructurally(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		steps       []spec.Step
		wantMessage string
	}{
		{
			name: "literal false",
			steps: []spec.Step{
				{ID: "check", Fields: map[string]any{"assert": false, "message": "nope"}},
			},
			wantMessage: "nope",
		},
		{
			name: "shared runtime expression",
			steps: []spec.Step{
				{ID: "produce", Fields: map[string]any{
					"expr": map[string]any{"approved": false, "message": "templated failure"},
				}},
				{ID: "check", Fields: map[string]any{
					"assert":  `$.prev.value["approved"]`,
					"message": `{{ ctx.prev.value["message"] }}`,
				}},
			},
			wantMessage: "templated failure",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			config := testConfig(t, sampleDoc(map[string]spec.Flow{
				"main": {Steps: tc.steps},
			}))
			mustPersist(t, config)

			_, err := NewRunner(nil).Run(context.Background(), config)
			failed := assertRunFailed(t, err, "assertion_failed")
			if failed.Failure.Message != tc.wantMessage {
				t.Fatalf("failure message = %q, want %q", failed.Failure.Message, tc.wantMessage)
			}
		})
	}
}

func TestRunnerExpressionsTemplatesAndExpectShareRuntimeTypes(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"approved": true,
		"items":    []any{"x", int64(3)},
	}

	config := testConfig(t, sampleDocWithParams(map[string]any{
		"payload": payload,
	}, map[string]spec.Flow{
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
	}))

	mustPersist(t, config)

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
