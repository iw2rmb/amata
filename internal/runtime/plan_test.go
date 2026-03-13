package runtime

import (
	"testing"

	"auto/internal/spec"
)

func TestBuildFlowPlanAddsNestedSwitchBranchFlows(t *testing.T) {
	t.Parallel()

	plan, err := buildFlowPlan(spec.Document{
		Flows: map[string]spec.Flow{
			"main": {
				Steps: []spec.Step{
					{
						ID:   "outer",
						Type: "switch",
						Fields: map[string]any{
							"cases": []any{
								map[string]any{
									"when": true,
									"steps": []spec.Step{
										{
											ID:   "inner",
											Type: "switch",
											Fields: map[string]any{
												"cases": []any{
													map[string]any{
														"when": true,
														"steps": []spec.Step{
															{ID: "done", Type: "expr", Fields: map[string]any{"expr": "ok"}},
														},
													},
												},
											},
										},
									},
								},
								map[string]any{
									"when": false,
									"steps": []spec.Step{
										{ID: "skip", Type: "expr", Fields: map[string]any{"expr": "skip"}},
									},
								},
							},
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("buildFlowPlan() error = %v", err)
	}

	firstOuter, ok := plan.SwitchBranchFlow("main", 0, 0)
	if !ok {
		t.Fatalf("missing first outer branch flow")
	}
	if firstOuter != "@switch:main:0:0" {
		t.Fatalf("first outer branch flow = %q, want %q", firstOuter, "@switch:main:0:0")
	}

	secondOuter, ok := plan.SwitchBranchFlow("main", 0, 1)
	if !ok {
		t.Fatalf("missing second outer branch flow")
	}
	if secondOuter != "@switch:main:0:1" {
		t.Fatalf("second outer branch flow = %q, want %q", secondOuter, "@switch:main:0:1")
	}

	innerBranch, ok := plan.SwitchBranchFlow(firstOuter, 0, 0)
	if !ok {
		t.Fatalf("missing inner branch flow")
	}
	if innerBranch != "@switch:@switch:main:0:0:0:0" {
		t.Fatalf("inner branch flow = %q, want %q", innerBranch, "@switch:@switch:main:0:0:0:0")
	}

	if _, ok := plan.Lookup(innerBranch); !ok {
		t.Fatalf("inner branch flow %q not registered", innerBranch)
	}
}
