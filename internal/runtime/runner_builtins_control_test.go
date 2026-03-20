package runtime

import (
	"context"
	"testing"

	"github.com/iw2rmb/amata/internal/spec"
	"github.com/iw2rmb/amata/internal/state"
)

func TestRunnerSwitchUsesFirstMatchingCaseAndReturnsStructuredResult(t *testing.T) {
	t.Parallel()

	config := testConfig(t, sampleDoc(map[string]spec.Flow{
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
							"matched": `$.prev.meta["matched"]`,
							"case":    `$.prev.meta["case"]`,
							"picked":  `$.prev.value["picked"]`,
							"seed":    `$.prev.value["seed"]`,
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

	config := testConfig(t, sampleDoc(map[string]spec.Flow{
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
							"matched": `$.prev.meta["matched"]`,
							"value":   `$.prev.value`,
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

func TestRunnerForEachIteratesItemsWithBindingsAndReturnsStructuredResult(t *testing.T) {
	t.Parallel()

	config := testConfig(t, sampleDoc(map[string]spec.Flow{
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
						"as":    "folder",
						"steps": []spec.Step{
							{
								ID: "body",
								Fields: map[string]any{
									"expr": map[string]any{
										"item":   `$.item`,
										"folder": `$.folder`,
										"index":  `$.index`,
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
							"count":     `$.prev.meta["count"]`,
							"index":     `$.prev.meta["index"]`,
							"item":      `$.prev.meta["item"]`,
							"bodyItem":  `$.prev.value["item"]`,
							"bodyAlias": `$.prev.value["folder"]`,
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
	if snapshot.Status != state.RunStatusSucceeded {
		t.Fatalf("snapshot status = %q, want succeeded", snapshot.Status)
	}
	if got, want := len(snapshot.Steps), 5; got != want {
		t.Fatalf("step count = %d, want %d", got, want)
	}
	if got := snapshot.Steps[1].Value.(map[string]any)["folder"]; got != "alpha" {
		t.Fatalf("first iteration alias = %#v, want alpha", got)
	}
	if got := snapshot.Steps[2].Value.(map[string]any)["folder"]; got != "beta" {
		t.Fatalf("second iteration alias = %#v, want beta", got)
	}

	loopValue := snapshot.Steps[3].Value.(map[string]any)
	if got := intValue(t, loopValue["count"]); got != 2 {
		t.Fatalf("for_each count = %d, want 2", got)
	}
	if got := intValue(t, loopValue["index"]); got != 1 {
		t.Fatalf("for_each index = %d, want 1", got)
	}
	if got := loopValue["item"]; got != "beta" {
		t.Fatalf("for_each item = %#v, want beta", got)
	}
	if got := loopValue["value"].(map[string]any)["folder"]; got != "beta" {
		t.Fatalf("for_each nested alias = %#v, want beta", got)
	}

	after := snapshot.Steps[4].Value.(map[string]any)
	if got := intValue(t, after["count"]); got != 2 {
		t.Fatalf("after count = %d, want 2", got)
	}
	if got := intValue(t, after["index"]); got != 1 {
		t.Fatalf("after index = %d, want 1", got)
	}
	if got := after["item"]; got != "beta" {
		t.Fatalf("after item = %#v, want beta", got)
	}
	if got := after["bodyItem"]; got != "beta" {
		t.Fatalf("after nested item = %#v, want beta", got)
	}
	if got := after["bodyAlias"]; got != "beta" {
		t.Fatalf("after nested alias = %#v, want beta", got)
	}
}

func TestRunnerForEachEmptyItemsReturnsNoNestedOutput(t *testing.T) {
	t.Parallel()

	config := testConfig(t, sampleDoc(map[string]spec.Flow{
		"main": {
			Steps: []spec.Step{
				{
					ID: "seed",
					Fields: map[string]any{
						"expr": []any{},
					},
				},
				{
					ID:   "loop",
					Type: "for_each",
					Fields: map[string]any{
						"items": `$.prev.value`,
						"steps": []spec.Step{
							{
								ID: "body",
								Fields: map[string]any{
									"expr": "unexpected",
								},
							},
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
	if got, want := len(snapshot.Steps), 2; got != want {
		t.Fatalf("step count = %d, want %d", got, want)
	}
	value := snapshot.Steps[1].Value.(map[string]any)
	if got := intValue(t, value["count"]); got != 0 {
		t.Fatalf("for_each count = %d, want 0", got)
	}
	if got := value["value"]; got != nil {
		t.Fatalf("for_each value = %#v, want nil", got)
	}
}

func TestRunnerRecursiveCallCarriesFrameLocalPrevAndReturnsOneStack(t *testing.T) {
	t.Parallel()

	config := testConfig(t, sampleDoc(map[string]spec.Flow{
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
						"expr": `$.prev.value`,
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
											"expr": `$.prev.value`,
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
						"expr": `$.prev.value`,
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

	config := testConfig(t, sampleDoc(map[string]spec.Flow{
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
	}))

	mustPersist(t, config)

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

func TestRunnerCallUnwrapsNestedControlPayloadInContext(t *testing.T) {
	t.Parallel()

	config := testConfig(t, sampleDoc(map[string]spec.Flow{
		"main": {
			Steps: []spec.Step{
				{
					ID:   "run-child",
					Type: "call",
					Fields: map[string]any{
						"flow": "child",
					},
				},
				{
					ID: "inspect",
					Fields: map[string]any{
						"expr": map[string]any{
							"hasItem": `$.prev.value["hasItem"]`,
							"flow":    `$.prev.meta["flow"]`,
						},
					},
				},
			},
		},
		"child": {
			Steps: []spec.Step{
				{
					ID: "seed",
					Fields: map[string]any{
						"expr": map[string]any{
							"hasItem": false,
						},
					},
				},
				{
					ID:   "branch",
					Type: "switch",
					Fields: map[string]any{
						"cases": []any{
							map[string]any{
								"when": true,
								"steps": []spec.Step{
									{
										ID: "return-seed",
										Fields: map[string]any{
											"expr": `$.prev.value`,
										},
									},
								},
							},
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
	if snapshot.Status != state.RunStatusSucceeded {
		t.Fatalf("snapshot status = %q, want succeeded", snapshot.Status)
	}

	final := snapshot.Steps[len(snapshot.Steps)-1].Value.(map[string]any)
	if got := final["hasItem"]; got != false {
		t.Fatalf("hasItem = %#v, want false", got)
	}
	if got := final["flow"]; got != "child" {
		t.Fatalf("flow = %#v, want child", got)
	}
}
