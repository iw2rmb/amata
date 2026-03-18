package progress_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/iw2rmb/amata/internal/executor"
	exprruntime "github.com/iw2rmb/amata/internal/expr"
	"github.com/iw2rmb/amata/internal/progress"
	"github.com/iw2rmb/amata/internal/spec"
	"github.com/iw2rmb/amata/internal/state"
	"github.com/iw2rmb/amata/internal/workspace"
)

func TestStepDescriptorShapes(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, time.March, 14, 10, 0, 0, 0, time.UTC)
	finishedAt := startedAt.Add(3*time.Second + 250*time.Millisecond)

	testCases := []struct {
		name              string
		step              func(t *testing.T) progress.Step
		wantStatus        progress.StatusSymbolKind
		wantType          string
		wantPrimary       string
		wantSummary       []string
		wantDetailLines   []string
		wantWrappedPrompt bool
	}{
		{
			name: "call running",
			step: func(t *testing.T) progress.Step {
				t.Helper()

				step, err := progress.StepFromContext(stepContext(spec.Document{}, spec.Step{
					ID:   "call-step",
					Type: "call",
					Fields: map[string]any{
						"flow": "$.params.flow",
					},
				}, map[string]any{"flow": "apply"}))
				if err != nil {
					t.Fatalf("StepFromContext: %v", err)
				}
				step.StartedAt = startedAt
				return step
			},
			wantStatus:      progress.StatusSymbolRunning,
			wantType:        "call",
			wantPrimary:     "apply",
			wantSummary:     []string{"apply"},
			wantDetailLines: []string{},
		},
		{
			name: "codex running",
			step: func(t *testing.T) progress.Step {
				t.Helper()

				step, err := progress.StepFromContext(stepContext(spec.Document{
					Defaults: map[string]any{
						"executors": map[string]any{
							"codex": map[string]any{
								"model": "$.params.model",
							},
						},
					},
				}, spec.Step{
					ID:   "agent",
					Type: "codex",
					Fields: map[string]any{
						"prompt":    "Implement {{ ctx.params.repo }} with enough detail to wrap across multiple descriptor lines cleanly.",
						"reasoning": "$.params.reasoning",
					},
				}, map[string]any{
					"model":     "gpt-5.4",
					"reasoning": "high",
					"repo":      "descriptor-repo",
				}))
				if err != nil {
					t.Fatalf("StepFromContext: %v", err)
				}
				step.StartedAt = startedAt
				return step
			},
			wantStatus:        progress.StatusSymbolRunning,
			wantType:          "codex",
			wantPrimary:       "gpt-5.4:high",
			wantSummary:       []string{"gpt-5.4", "high"},
			wantWrappedPrompt: true,
		},
		{
			name: "claude running",
			step: func(t *testing.T) progress.Step {
				t.Helper()

				step, err := progress.StepFromContext(stepContext(spec.Document{
					Defaults: map[string]any{
						"executors": map[string]any{
							"claude": map[string]any{
								"model": "sonnet",
							},
						},
					},
				}, spec.Step{
					ID:   "review",
					Type: "claude",
					Fields: map[string]any{
						"prompt": "Review the diff and return actionable feedback only.",
					},
				}, nil))
				if err != nil {
					t.Fatalf("StepFromContext: %v", err)
				}
				step.StartedAt = startedAt
				return step
			},
			wantStatus:      progress.StatusSymbolRunning,
			wantType:        "claude",
			wantPrimary:     "sonnet",
			wantSummary:     []string{"sonnet"},
			wantDetailLines: []string{"Review the diff and return actionable feedback only."},
		},
		{
			name: "switch finished",
			step: func(t *testing.T) progress.Step {
				t.Helper()

				step, err := progress.StepFromResultWithContext("main", stepContext(spec.Document{}, spec.Step{
					ID:   "switch-step",
					Type: "switch",
					Fields: map[string]any{
						"cases": []any{
							map[string]any{"when": true, "steps": []any{}},
							map[string]any{"steps": []any{}},
						},
					},
				}, nil), state.StepResult{
					Index:  1,
					ID:     "switch-step",
					Type:   "switch",
					Status: state.StepStatusSucceeded,
					Value: map[string]any{
						"matched": true,
						"case":    1,
					},
				})
				if err != nil {
					t.Fatalf("StepFromResultWithContext: %v", err)
				}
				step.StartedAt = startedAt
				step.FinishedAt = finishedAt
				return step
			},
			wantStatus:      progress.StatusSymbolSucceeded,
			wantType:        "switch",
			wantPrimary:     "case 1",
			wantSummary:     []string{"case 1"},
			wantDetailLines: []string{},
		},
		{
			name: "for_each running",
			step: func(t *testing.T) progress.Step {
				t.Helper()

				step, err := progress.StepFromContext(stepContext(spec.Document{}, spec.Step{
					ID:   "loop-step",
					Type: "for_each",
					Fields: map[string]any{
						"items": []any{"alpha", "beta", "gamma"},
						"steps": []any{},
					},
				}, nil))
				if err != nil {
					t.Fatalf("StepFromContext: %v", err)
				}
				step.StartedAt = startedAt
				return step
			},
			wantStatus:      progress.StatusSymbolRunning,
			wantType:        "for_each",
			wantPrimary:     "3 items",
			wantSummary:     []string{"3 items"},
			wantDetailLines: []string{},
		},
		{
			name: "shell running",
			step: func(t *testing.T) progress.Step {
				t.Helper()

				step, err := progress.StepFromContext(stepContext(spec.Document{}, spec.Step{
					ID:   "shell-step",
					Type: "shell",
					Fields: map[string]any{
						"command": []any{"go", "test", "./internal/progress"},
					},
				}, nil))
				if err != nil {
					t.Fatalf("StepFromContext: %v", err)
				}
				step.StartedAt = startedAt
				return step
			},
			wantStatus:      progress.StatusSymbolRunning,
			wantType:        "shell",
			wantPrimary:     "go test ./internal/progress",
			wantDetailLines: []string{},
		},
		{
			name: "assert failed",
			step: func(t *testing.T) progress.Step {
				t.Helper()

				step, err := progress.StepFromResultWithContext("main", stepContext(spec.Document{}, spec.Step{
					ID:   "assert-step",
					Type: "assert",
					Fields: map[string]any{
						"assert":  "$.params.ok",
						"message": "expected descriptor shape",
					},
				}, map[string]any{"ok": false}), state.StepResult{
					Index:  1,
					ID:     "assert-step",
					Type:   "assert",
					Status: state.StepStatusFailed,
					Error: &state.Failure{
						Code:    "assertion_failed",
						Message: "expected descriptor shape",
					},
				})
				if err != nil {
					t.Fatalf("StepFromResultWithContext: %v", err)
				}
				step.StartedAt = startedAt
				step.FinishedAt = finishedAt
				return step
			},
			wantStatus:      progress.StatusSymbolFailed,
			wantType:        "assert",
			wantPrimary:     "false",
			wantSummary:     []string{"failed"},
			wantDetailLines: []string{"expected descriptor shape"},
		},
		{
			name: "git.inspect finished",
			step: func(t *testing.T) progress.Step {
				t.Helper()

				step, err := progress.StepFromResultWithContext("main", stepContext(spec.Document{}, spec.Step{
					ID:   "inspect",
					Type: "git.inspect",
					Fields: map[string]any{
						"cwd": "repo",
					},
				}, nil), state.StepResult{
					Index:  2,
					ID:     "inspect",
					Type:   "git.inspect",
					Status: state.StepStatusSucceeded,
					Value: map[string]any{
						"isRepo":  true,
						"hasDiff": true,
						"files":   []string{"engine.txt", "notes/todo.txt"},
					},
				})
				if err != nil {
					t.Fatalf("StepFromResultWithContext: %v", err)
				}
				step.StartedAt = startedAt
				step.FinishedAt = finishedAt
				return step
			},
			wantStatus:      progress.StatusSymbolSucceeded,
			wantType:        "git.inspect",
			wantPrimary:     "dirty 2 files",
			wantSummary:     []string{"2 files"},
			wantDetailLines: []string{"/repo/repo", "engine.txt", "notes/todo.txt"},
		},
		{
			name: "git.commit finished",
			step: func(t *testing.T) progress.Step {
				t.Helper()

				step, err := progress.StepFromResultWithContext("main", stepContext(spec.Document{}, spec.Step{
					ID:   "commit",
					Type: "git.commit",
					Fields: map[string]any{
						"message": "engine: persist structured commit summary",
					},
				}, nil), state.StepResult{
					Index:  3,
					ID:     "commit",
					Type:   "git.commit",
					Status: state.StepStatusSucceeded,
					Value: map[string]any{
						"committed": true,
						"commit":    "abc123def456",
						"paths":     []string{"engine.txt", "notes/todo.txt"},
						"metadata": map[string]any{
							"shortCommit":      "abc123d",
							"changedFileCount": 2,
							"insertions":       7,
							"deletions":        3,
							"files": []any{
								map[string]any{"path": "engine.txt", "insertions": 5, "deletions": 2},
								map[string]any{"path": "notes/todo.txt", "insertions": 2, "deletions": 1},
							},
						},
					},
				})
				if err != nil {
					t.Fatalf("StepFromResultWithContext: %v", err)
				}
				step.StartedAt = startedAt
				step.FinishedAt = finishedAt
				return step
			},
			wantStatus:  progress.StatusSymbolSucceeded,
			wantType:    "git.commit",
			wantPrimary: "abc123d files 2 +7 -3",
			wantSummary: []string{"abc123d", "files 2 +7 -3"},
			wantDetailLines: []string{
				"engine: persist structured commit summary",
				"+5 -2 engine.txt",
				"+2 -1 notes/todo.txt",
			},
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			descriptor := progress.BuildStepDescriptor(testCase.step(t), progress.DescriptorOptions{
				Now:         finishedAt,
				DetailWidth: 60,
			})

			if descriptor.StatusSymbolKind != testCase.wantStatus {
				t.Fatalf("StatusSymbolKind = %q, want %q", descriptor.StatusSymbolKind, testCase.wantStatus)
			}
			if descriptor.StepType != testCase.wantType {
				t.Fatalf("StepType = %q, want %q", descriptor.StepType, testCase.wantType)
			}
			if descriptor.PrimaryText != testCase.wantPrimary {
				t.Fatalf("PrimaryText = %q, want %q", descriptor.PrimaryText, testCase.wantPrimary)
			}
			if !reflect.DeepEqual(descriptor.FinalSummaryDetails, testCase.wantSummary) {
				t.Fatalf("FinalSummaryDetails = %#v, want %#v", descriptor.FinalSummaryDetails, testCase.wantSummary)
			}
			if descriptor.Elapsed != 3*time.Second+250*time.Millisecond {
				t.Fatalf("Elapsed = %v, want %v", descriptor.Elapsed, 3*time.Second+250*time.Millisecond)
			}

			if testCase.wantWrappedPrompt {
				if len(descriptor.DetailLines) < 2 {
					t.Fatalf("DetailLines = %#v, want wrapped prompt across multiple lines", descriptor.DetailLines)
				}
				for _, line := range descriptor.DetailLines {
					if len(line) > 60 {
						t.Fatalf("detail line %q exceeds width 60", line)
					}
				}
				if strings.Join(descriptor.DetailLines, " ") != "Implement descriptor-repo with enough detail to wrap across multiple descriptor lines cleanly." {
					t.Fatalf("DetailLines joined = %q", strings.Join(descriptor.DetailLines, " "))
				}
				return
			}

			if !reflect.DeepEqual(descriptor.DetailLines, testCase.wantDetailLines) {
				t.Fatalf("DetailLines = %#v, want %#v", descriptor.DetailLines, testCase.wantDetailLines)
			}
		})
	}
}

func stepContext(document spec.Document, step spec.Step, params map[string]any) executor.StepContext {
	workspaceConfig := workspace.Config{
		Root:     "/repo",
		StateDir: "/repo/.amata",
	}

	return executor.StepContext{
		FlowName:  "main",
		StepIndex: 0,
		Step:      step,
		Spec:      document,
		Workspace: workspaceConfig,
		Runtime: exprruntime.NewRuntime(map[string]any{
			"ctx": map[string]any{
				"workspace": map[string]any{
					"root":      workspaceConfig.Root,
					"state_dir": workspaceConfig.StateDir,
				},
				"params": params,
				"prev":   nil,
			},
		}),
	}
}
