package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/iw2rmb/amata/internal/spec"
	"github.com/iw2rmb/amata/internal/state"
)

func TestRunnerBuiltinsShellCapturesArtifactsAndNormalizesCWD(t *testing.T) {
	t.Parallel()

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Flows: map[string]spec.Flow{
			"main": {
				Steps: []spec.Step{
					{
						ID: "shell-step",
						Fields: map[string]any{
							"command": "printf 'hello'; printf 'warn' >&2; pwd > report.txt",
							"cwd":     "nested",
							"files": map[string]any{
								"report": "nested/report.txt",
							},
						},
					},
				},
			},
		},
	})

	if err := os.MkdirAll(filepath.Join(config.Workspace.Root, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir nested cwd: %v", err)
	}
	mustPersist(t, config)

	snapshot, err := NewRunner(nil).Run(context.Background(), config)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	result := snapshot.Steps[0]
	if result.Status != state.StepStatusSucceeded {
		t.Fatalf("step status = %q, want succeeded", result.Status)
	}
	if got := result.Value.(map[string]any)["exitCode"].(float64); got != 0 {
		t.Fatalf("exitCode = %#v, want 0", got)
	}
	if got := strings.TrimSpace(readFile(t, result.Artifacts.Stdout)); got != "hello" {
		t.Fatalf("stdout = %q, want hello", got)
	}
	if got := strings.TrimSpace(readFile(t, result.Artifacts.Stderr)); got != "warn" {
		t.Fatalf("stderr = %q, want warn", got)
	}

	reportPath := result.Artifacts.Files["report"]
	if reportPath == "" {
		t.Fatalf("named report artifact missing")
	}
	if got := strings.TrimSpace(readFile(t, reportPath)); got != filepath.Join(config.Workspace.Root, "nested") {
		t.Fatalf("captured cwd = %q, want %q", got, filepath.Join(config.Workspace.Root, "nested"))
	}
}

func TestRunnerBuiltinsShellResolveTemplatedScalars(t *testing.T) {
	t.Parallel()

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Params: map[string]any{
			"filename": "report",
			"content":  "templated",
		},
		Flows: map[string]spec.Flow{
			"main": {
				Steps: []spec.Step{
					{
						ID: "shell-step",
						Fields: map[string]any{
							"command": []any{
								"sh",
								"-lc",
								"printf '{{ ctx.params.content }}' > {{ ctx.params.filename }}.txt",
							},
							"cwd": "{{ ctx.workspace.root }}/nested",
							"files": map[string]any{
								"report": "{{ ctx.workspace.root }}/nested/{{ ctx.params.filename }}.txt",
							},
						},
					},
				},
			},
		},
	})

	if err := os.MkdirAll(filepath.Join(config.Workspace.Root, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir nested cwd: %v", err)
	}
	mustPersist(t, config)

	snapshot, err := NewRunner(nil).Run(context.Background(), config)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	reportPath := snapshot.Steps[0].Artifacts.Files["report"]
	if reportPath == "" {
		t.Fatalf("named report artifact missing")
	}
	if got := strings.TrimSpace(readFile(t, reportPath)); got != "templated" {
		t.Fatalf("captured report = %q, want templated", got)
	}
}

func TestRunnerPersistsRunMetadataAndArtifactsUnderRunDirectory(t *testing.T) {
	t.Parallel()

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Flows: map[string]spec.Flow{
			"main": {
				Steps: []spec.Step{
					{
						ID: "shell-step",
						Fields: map[string]any{
							"command": "printf 'hello'; printf 'warn' >&2; printf 'report' > report.txt",
							"files": map[string]any{
								"report": "report.txt",
							},
						},
					},
				},
			},
		},
	})

	mustPersist(t, config)

	snapshot, err := NewRunner(nil).Run(context.Background(), config)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	expectedPaths := []string{
		filepath.Join(config.RunDir, "spec.yaml"),
		filepath.Join(config.RunDir, "events.ndjson"),
		filepath.Join(config.RunDir, "snapshot.json"),
		snapshot.Steps[0].Artifacts.Stdout,
		snapshot.Steps[0].Artifacts.Stderr,
		snapshot.Steps[0].Artifacts.Files["report"],
	}
	for _, path := range expectedPaths {
		if !strings.HasPrefix(path, config.RunDir+string(os.PathSeparator)) {
			t.Fatalf("path %q does not live under run dir %q", path, config.RunDir)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
	}
}

func TestRunnerBuiltinShellRejectsInvalidFilesBeforeCommandRuns(t *testing.T) {
	t.Parallel()

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Flows: map[string]spec.Flow{
			"main": {
				Steps: []spec.Step{
					{
						ID: "shell-step",
						Fields: map[string]any{
							"command": "touch should-not-exist.txt",
							"files":   []any{"bad"},
						},
					},
				},
			},
		},
	})

	mustPersist(t, config)

	_, err := NewRunner(nil).Run(context.Background(), config)
	assertRunFailed(t, err, "invalid_files")
	if _, err := os.Stat(filepath.Join(config.Workspace.Root, "should-not-exist.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("command side effect err = %v, want not exists", err)
	}
}

func TestRunnerBuiltinShellKeepsStdIOArtifactsWhenNamedFileCaptureFails(t *testing.T) {
	t.Parallel()

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Flows: map[string]spec.Flow{
			"main": {
				Steps: []spec.Step{
					{
						ID: "shell-step",
						Fields: map[string]any{
							"command": "printf 'hello'; printf 'warn' >&2",
							"files": map[string]any{
								"missing": "missing.txt",
							},
						},
					},
				},
			},
		},
	})

	mustPersist(t, config)

	_, err := NewRunner(nil).Run(context.Background(), config)
	assertRunFailed(t, err, "artifact_capture_failed")

	snapshot, err := state.NewStore(config.RunDir).LoadSnapshot()
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	result := snapshot.Steps[0]
	if result.Artifacts.Stdout == "" {
		t.Fatalf("stdout artifact path missing")
	}
	if result.Artifacts.Stderr == "" {
		t.Fatalf("stderr artifact path missing")
	}
	if got := strings.TrimSpace(readFile(t, result.Artifacts.Stdout)); got != "hello" {
		t.Fatalf("stdout = %q, want hello", got)
	}
	if got := strings.TrimSpace(readFile(t, result.Artifacts.Stderr)); got != "warn" {
		t.Fatalf("stderr = %q, want warn", got)
	}
}

func TestRunnerBuiltinsExprAndWhenSkip(t *testing.T) {
	t.Parallel()

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Flows: map[string]spec.Flow{
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
		},
	})

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

func TestRunnerSwitchUsesFirstMatchingCaseAndReturnsStructuredResult(t *testing.T) {
	t.Parallel()

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Flows: map[string]spec.Flow{
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
								"matched": `$.prev.value["matched"]`,
								"case":    `$.prev.value["case"]`,
								"picked":  `$.prev.value["value"]["picked"]`,
								"seed":    `$.prev.value["value"]["seed"]`,
							},
						},
					},
				},
			},
		},
	})

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

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Flows: map[string]spec.Flow{
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
								"matched": `$.prev.value["matched"]`,
								"value":   `$.prev.value["value"]`,
							},
						},
					},
				},
			},
		},
	})

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

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Flows: map[string]spec.Flow{
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
								"count":     `$.prev.value["count"]`,
								"index":     `$.prev.value["index"]`,
								"item":      `$.prev.value["item"]`,
								"bodyItem":  `$.prev.value["value"]["item"]`,
								"bodyAlias": `$.prev.value["value"]["folder"]`,
							},
						},
					},
				},
			},
		},
	})

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

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Flows: map[string]spec.Flow{
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
		},
	})

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

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Flows: map[string]spec.Flow{
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
							"expr": `$.prev.value["value"]`,
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
												"expr": `$.prev.value["value"]`,
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
							"expr": `$.prev.value["value"]`,
						},
					},
				},
			},
		},
	})

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

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Flows: map[string]spec.Flow{
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
		},
	})

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

func TestRunnerCtxPrevChainTraversesSucceededStepsOnly(t *testing.T) {
	t.Parallel()

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Flows: map[string]spec.Flow{
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
		},
	})

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

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Flows: map[string]spec.Flow{
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
		},
	})

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

			config := testConfig(t, spec.Document{
				Version: spec.Version,
				Name:    "sample",
				Entry:   "main",
				Flows: map[string]spec.Flow{
					"main": {Steps: tc.steps},
				},
			})
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

			config := testConfig(t, spec.Document{
				Version: spec.Version,
				Name:    "sample",
				Entry:   "main",
				Flows: map[string]spec.Flow{
					"main": {Steps: tc.steps},
				},
			})
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

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Params: map[string]any{
			"payload": payload,
		},
		Flows: map[string]spec.Flow{
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
		},
	})

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
