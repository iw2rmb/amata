package runtime

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	executorapi "github.com/iw2rmb/amata/internal/executor"
	"github.com/iw2rmb/amata/internal/spec"
	"github.com/iw2rmb/amata/internal/state"
	"github.com/iw2rmb/amata/internal/workspace"
)

func TestRunnerResponseFromPublishesValidatedValue(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		response map[string]any
		result   state.StepResult
		want     string
	}{
		{
			name: "executor value",
			response: map[string]any{
				"schema": map[string]any{"$ref": "#/schemas/selected_value"},
			},
			result: state.StepResult{
				Status: state.StepStatusSucceeded,
				Value: map[string]any{
					"selected": "value",
				},
				Artifacts: executorapi.EmptyArtifacts(),
			},
			want: "value",
		},
		{
			name: "stdout artifact",
			response: map[string]any{
				"from":   "stdout",
				"schema": map[string]any{"$ref": "#/schemas/selected_value"},
			},
			result: state.StepResult{
				Status: state.StepStatusSucceeded,
				Value:  map[string]any{"selected": "ignored"},
				Artifacts: state.Artifacts{
					Stdout: "__stdout__",
					Files:  map[string]string{},
				},
			},
			want: "stdout",
		},
		{
			name: "stderr artifact",
			response: map[string]any{
				"from":   "stderr",
				"schema": map[string]any{"$ref": "#/schemas/selected_value"},
			},
			result: state.StepResult{
				Status: state.StepStatusSucceeded,
				Value:  map[string]any{"selected": "ignored"},
				Artifacts: state.Artifacts{
					Stderr: "__stderr__",
					Files:  map[string]string{},
				},
			},
			want: "stderr",
		},
		{
			name: "named artifact",
			response: map[string]any{
				"from":   "artifact:report",
				"schema": map[string]any{"$ref": "#/schemas/selected_value"},
			},
			result: state.StepResult{
				Status: state.StepStatusSucceeded,
				Value:  map[string]any{"selected": "ignored"},
				Artifacts: state.Artifacts{
					Files: map[string]string{
						"report": "__report__",
					},
				},
			},
			want: "report",
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			config := testConfig(t, spec.Document{
				Version: spec.Version,
				Name:    "sample",
				Entry:   "main",
				Schemas: map[string]any{
					"selected_value": map[string]any{
						"type":                 "object",
						"required":             []any{"selected"},
						"additionalProperties": false,
						"properties": map[string]any{
							"selected": "string",
						},
					},
				},
				Flows: map[string]spec.Flow{
					"main": {
						Steps: []spec.Step{
							{
								ID:   "resolve",
								Type: "fake",
								Fields: map[string]any{
									"response": testCase.response,
								},
							},
							{
								ID:   "consume",
								Type: "expr",
								Fields: map[string]any{
									"expr": `$.prev.value["selected"]`,
								},
							},
						},
					},
				},
			})

			testResult := cloneStepResult(testCase.result)
			switch testCase.name {
			case "stdout artifact":
				testResult.Artifacts.Stdout = writeArtifactFixture(t, config.RunDir, "stdout.json", `{"selected":"stdout"}`)
			case "stderr artifact":
				testResult.Artifacts.Stderr = writeArtifactFixture(t, config.RunDir, "stderr.json", `{"selected":"stderr"}`)
			case "named artifact":
				testResult.Artifacts.Files["report"] = writeArtifactFixture(t, config.RunDir, "report.json", `{"selected":"report"}`)
			}

			if err := PersistRunSpec(config); err != nil {
				t.Fatalf("persist run spec: %v", err)
			}

			registry := builtinRegistry()
			mustRegister(registry, "fake", func() executorapi.Executor {
				return &fakeExecutor{
					calls: new([]string),
					execute: func(executorapi.StepContext) state.StepResult {
						return cloneStepResult(testResult)
					},
				}
			})

			snapshot, err := NewRunner(registry).Run(context.Background(), config)
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if got := snapshot.Steps[0].Value.(map[string]any)["selected"]; got != testCase.want {
				t.Fatalf("resolved step value = %#v, want %q", got, testCase.want)
			}
			if got := snapshot.Steps[1].Value; got != testCase.want {
				t.Fatalf("downstream value = %#v, want %q", got, testCase.want)
			}
		})
	}
}

func TestRunnerResponseFromStdoutLinesPublishesList(t *testing.T) {
	t.Parallel()

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Schemas: map[string]any{
			"line_list": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "string",
				},
			},
		},
		Flows: map[string]spec.Flow{
			"main": {
				Steps: []spec.Step{
					{
						ID:   "resolve",
						Type: "fake",
						Fields: map[string]any{
							"response": map[string]any{
								"from":   "stdout_lines",
								"schema": map[string]any{"$ref": "#/schemas/line_list"},
							},
						},
					},
					{
						ID:   "consume",
						Type: "expr",
						Fields: map[string]any{
							"expr": `$.prev.value[1]`,
						},
					},
				},
			},
		},
	})

	mustPersist(t, config)

	registry := builtinRegistry()
	mustRegister(registry, "fake", func() executorapi.Executor {
		return &fakeExecutor{
			calls: new([]string),
			execute: func(executorapi.StepContext) state.StepResult {
				return state.StepResult{
					Status: state.StepStatusSucceeded,
					Artifacts: state.Artifacts{
						Stdout: writeArtifactFixture(t, config.RunDir, "stdout-lines.txt", "alpha\nbeta\n"),
						Files:  map[string]string{},
					},
				}
			},
		}
	})

	snapshot, err := NewRunner(registry).Run(context.Background(), config)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	lines := snapshot.Steps[0].Value.([]any)
	if !reflect.DeepEqual(lines, []any{"alpha", "beta"}) {
		t.Fatalf("lines = %#v, want %#v", lines, []any{"alpha", "beta"})
	}
	if got := snapshot.Steps[1].Value; got != "beta" {
		t.Fatalf("downstream value = %#v, want beta", got)
	}
}

func TestRunnerNormalizesPreviousTypedSlicesForExpressions(t *testing.T) {
	t.Parallel()

	config := testConfig(t, sampleDoc(map[string]spec.Flow{
		"main": {
			Steps: []spec.Step{
				{ID: "typed-slice", Type: "fake"},
				{ID: "read-path", Type: "fake"},
			},
		},
	}))

	mustPersist(t, config)

	calls := []string{}
	registry := NewRegistry()
	if err := registry.Register("fake", func() executorapi.Executor {
		return &fakeExecutor{
			calls: &calls,
			execute: func(ctx executorapi.StepContext) state.StepResult {
				if ctx.Step.ID == "read-path" {
					value, err := ctx.Runtime.Resolve(map[string]any{
						"expr": `ctx.prev.value["paths"][0]`,
					})
					if err != nil {
						return state.StepResult{
							Status: state.StepStatusFailed,
							Error: &state.Failure{
								Code:    "resolve_failed",
								Message: err.Error(),
							},
						}
					}
					return state.StepResult{
						Status: state.StepStatusSucceeded,
						Value:  value,
					}
				}

				return state.StepResult{
					Status: state.StepStatusSucceeded,
					Value: map[string]any{
						"paths": []string{"alpha", "beta"},
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
	if got := snapshot.Steps[1].Value; got != "alpha" {
		t.Fatalf("expr value = %#v, want %q", got, "alpha")
	}
}

func TestRunnerResponseSchemaErrors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		schemas  map[string]any
		response map[string]any
		result   state.StepResult
		wantCode string
	}{
		{
			name: "schema mismatch",
			schemas: map[string]any{
				"selected_value": map[string]any{
					"type":                 "object",
					"required":             []any{"selected"},
					"additionalProperties": false,
					"properties":           map[string]any{"selected": "string"},
				},
			},
			response: map[string]any{"schema": map[string]any{"$ref": "#/schemas/selected_value"}},
			result: state.StepResult{
				Status:    state.StepStatusSucceeded,
				Value:     map[string]any{"selected": true},
				Artifacts: executorapi.EmptyArtifacts(),
			},
			wantCode: "response_schema_mismatch",
		},
		{
			name:     "invalid schema ref",
			response: map[string]any{"schema": map[string]any{"$ref": "#/schemas/missing"}},
			result: state.StepResult{
				Status:    state.StepStatusSucceeded,
				Value:     map[string]any{"selected": "value"},
				Artifacts: executorapi.EmptyArtifacts(),
			},
			wantCode: "invalid_response_schema",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			config := testConfig(t, spec.Document{
				Version: spec.Version,
				Name:    "sample",
				Entry:   "main",
				Schemas: tc.schemas,
				Flows: map[string]spec.Flow{
					"main": {
						Steps: []spec.Step{
							{ID: "resolve", Type: "fake", Fields: map[string]any{"response": tc.response}},
							{ID: "after", Type: "fake"},
						},
					},
				},
			})
			mustPersist(t, config)

			calls := []string{}
			registry := builtinRegistry()
			mustRegister(registry, "fake", func() executorapi.Executor {
				return &fakeExecutor{
					calls:   &calls,
					results: map[string]state.StepResult{"resolve": tc.result, "after": {Status: state.StepStatusSucceeded}},
				}
			})

			snapshot, err := NewRunner(registry).Run(context.Background(), config)
			assertRunFailed(t, err, tc.wantCode)
			if got, want := calls, []string{"resolve"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("calls = %#v, want %#v", got, want)
			}
			if snapshot.Steps[0].Status != state.StepStatusFailed {
				t.Fatalf("resolve status = %q, want failed", snapshot.Steps[0].Status)
			}
		})
	}
}

func TestRunnerCodexResponseSchemaHandlesThinkingBySource(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		response     map[string]any
		result       state.StepResult
		wantSelected string
		wantThinking bool
	}{
		{
			name: "value source includes thinking",
			response: map[string]any{
				"schema": map[string]any{"$ref": "#/schemas/selected_value"},
			},
			result: state.StepResult{
				Status: state.StepStatusSucceeded,
				Value: map[string]any{
					"selected":  "value",
					"$thinking": "reasoning notes",
				},
				Artifacts: executorapi.EmptyArtifacts(),
			},
			wantSelected: "value",
			wantThinking: true,
		},
		{
			name: "stdout source does not require thinking",
			response: map[string]any{
				"from":   "stdout",
				"schema": map[string]any{"$ref": "#/schemas/selected_value"},
			},
			result: state.StepResult{
				Status: state.StepStatusSucceeded,
				Value:  map[string]any{"selected": "ignored"},
				Artifacts: state.Artifacts{
					Files: map[string]string{},
				},
			},
			wantSelected: "stdout",
			wantThinking: false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			config := testConfig(t, spec.Document{
				Version: spec.Version,
				Name:    "sample",
				Entry:   "main",
				Schemas: map[string]any{
					"selected_value": map[string]any{
						"type":                 "object",
						"required":             []any{"selected"},
						"additionalProperties": false,
						"properties": map[string]any{
							"selected": "string",
						},
					},
				},
				Flows: map[string]spec.Flow{
					"main": {
						Steps: []spec.Step{
							{
								ID:   "resolve",
								Type: "codex",
								Fields: map[string]any{
									"response": tc.response,
								},
							},
							{
								ID:   "after",
								Type: "fake",
							},
						},
					},
				},
			})
			mustPersist(t, config)

			testResult := cloneStepResult(tc.result)
			if tc.name == "stdout source does not require thinking" {
				testResult.Artifacts.Stdout = writeArtifactFixture(t, config.RunDir, "stdout.json", `{"selected":"stdout"}`)
			}

			calls := []string{}
			registry := NewRegistry()
			mustRegister(registry, "codex", func() executorapi.Executor {
				return &fakeExecutor{
					calls: &calls,
					results: map[string]state.StepResult{
						"resolve": testResult,
					},
				}
			})
			mustRegister(registry, "fake", func() executorapi.Executor {
				return &fakeExecutor{
					calls: &calls,
					results: map[string]state.StepResult{
						"after": {Status: state.StepStatusSucceeded},
					},
				}
			})

			snapshot, err := NewRunner(registry).Run(context.Background(), config)
			if err != nil {
				t.Fatalf("run: %v", err)
			}

			resolveValue, ok := snapshot.Steps[0].Value.(map[string]any)
			if !ok {
				t.Fatalf("resolve value = %#v, want map", snapshot.Steps[0].Value)
			}
			if got := resolveValue["selected"]; got != tc.wantSelected {
				t.Fatalf("resolve selected = %#v, want %q", got, tc.wantSelected)
			}
			_, hasThinking := resolveValue["$thinking"]
			if hasThinking != tc.wantThinking {
				t.Fatalf("resolve thinking present = %v, want %v", hasThinking, tc.wantThinking)
			}
			if got, want := calls, []string{"resolve", "after"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("calls = %#v, want %#v", got, want)
			}
		})
	}
}

func TestRunnerExpectDoesNotOverrideExecutionFailure(t *testing.T) {
	t.Parallel()

	config := testConfig(t, sampleDoc(map[string]spec.Flow{
		"main": {
			Steps: []spec.Step{
				{
					ID: "shell-failure",
					Fields: map[string]any{
						"command": "exit 2",
						"expect": map[string]any{
							"expr": "False",
						},
					},
				},
			},
		},
	}))

	mustPersist(t, config)

	_, err := NewRunner(nil).Run(context.Background(), config)
	assertRunFailed(t, err, "shell_failed")
}

func TestRunnerExpectFailureStopsRecursiveLoopOnCurrentStep(t *testing.T) {
	t.Parallel()

	config := testConfig(t, sampleDoc(map[string]spec.Flow{
		"main": {
			Steps: []spec.Step{
				{
					ID: "seed",
					Fields: map[string]any{
						"expr": map[string]any{
							"n":   2,
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
						"expr": "unreachable",
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
											"expect": map[string]any{
												"expr": `ctx.value["n"] > 0`,
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
	assertRunFailed(t, err, "expectation_failed")
	if snapshot.Status != state.RunStatusFailed {
		t.Fatalf("snapshot status = %q, want failed", snapshot.Status)
	}
	if got := snapshot.Steps[len(snapshot.Steps)-1].ID; got != "decrement" {
		t.Fatalf("failed step id = %q, want decrement", got)
	}
	if got := snapshot.Steps[len(snapshot.Steps)-1].Status; got != state.StepStatusFailed {
		t.Fatalf("failed step status = %q, want failed", got)
	}
	if got := intValue(t, snapshot.Steps[len(snapshot.Steps)-1].Value.(map[string]any)["n"]); got != 0 {
		t.Fatalf("failed step value n = %d, want 0", got)
	}
	for _, step := range snapshot.Steps {
		if step.ID == "recurse" || step.ID == "after" {
			t.Fatalf("unexpected step executed after failed expect: %q", step.ID)
		}
	}
}

func TestRunnerExposesSpecPathAndDirInRuntimeContext(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	specDir := filepath.Join(baseDir, "bundle")
	workspaceRoot := filepath.Join(baseDir, "repo")
	stateDir := filepath.Join(workspaceRoot, ".amata")
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		t.Fatalf("mkdir spec dir: %v", err)
	}
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("mkdir workspace root: %v", err)
	}

	helperPath := filepath.Join(specDir, "helper.txt")
	if err := os.WriteFile(helperPath, []byte("bundle helper"), 0o644); err != nil {
		t.Fatalf("write helper file: %v", err)
	}

	config := Config{
		RunID:    "run-001",
		RunDir:   filepath.Join(stateDir, "runs", "run-001"),
		SpecPath: filepath.Join(specDir, "workflow.yaml"),
		Workspace: workspace.Config{
			Root:     workspaceRoot,
			StateDir: stateDir,
		},
		Spec: spec.Document{
			Version: spec.Version,
			Name:    "sample",
			Entry:   "main",
			Flows: map[string]spec.Flow{
				"main": {
					Steps: []spec.Step{
						{
							ID: "spec-dir",
							Fields: map[string]any{
								"expr": "$.spec.dir",
							},
						},
						{
							ID: "spec-path",
							Fields: map[string]any{
								"expr": "$.spec.path",
							},
						},
						{
							ID: "read-helper",
							Fields: map[string]any{
								"command": []any{
									"sh",
									"-lc",
									"cat '{{ ctx.spec.dir }}/helper.txt'",
								},
							},
						},
					},
				},
			},
		},
	}

	mustPersist(t, config)

	snapshot, err := NewRunner(nil).Run(context.Background(), config)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := snapshot.Steps[0].Value; got != specDir {
		t.Fatalf("spec dir = %#v, want %q", got, specDir)
	}
	if got := snapshot.Steps[1].Value; got != config.SpecPath {
		t.Fatalf("spec path = %#v, want %q", got, config.SpecPath)
	}
	if got := strings.TrimSpace(readFile(t, snapshot.Steps[2].Artifacts.Stdout)); got != "bundle helper" {
		t.Fatalf("helper stdout = %q, want bundle helper", got)
	}
}
