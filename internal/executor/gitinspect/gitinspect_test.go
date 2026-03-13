package gitinspect_test

import (
	"context"
	"reflect"
	"testing"

	"auto/internal/executor"
	"auto/internal/executor/gitinspect"
	exprruntime "auto/internal/expr"
	"auto/internal/gitadapter"
	"auto/internal/spec"
	"auto/internal/state"
	"auto/internal/workspace"
)

func TestExecutorReturnsTypedSnapshot(t *testing.T) {
	t.Parallel()

	result := gitinspect.NewWithService(fakeInspectService{
		snapshot: gitadapter.Snapshot{
			IsRepo:  true,
			HasDiff: true,
			Files:   []string{"tracked.txt", "notes/todo.txt"},
		},
	}).Execute(context.Background(), executor.StepContext{
		Workspace: workspace.Config{Root: "/repo"},
		StepIndex: 2,
		Step: spec.Step{
			Type: "git.inspect",
			Fields: map[string]any{
				"cwd": "notes",
			},
		},
		Runtime: exprruntime.NewRuntime(map[string]any{}),
	})

	if result.Status != state.StepStatusSucceeded {
		t.Fatalf("result.Status = %q, want %q", result.Status, state.StepStatusSucceeded)
	}

	want := map[string]any{
		"isRepo":  true,
		"hasDiff": true,
		"files":   []string{"tracked.txt", "notes/todo.txt"},
	}
	if !reflect.DeepEqual(result.Value, want) {
		t.Fatalf("result.Value = %#v, want %#v", result.Value, want)
	}
}

type fakeInspectService struct {
	snapshot gitadapter.Snapshot
	err      error
}

func (f fakeInspectService) Inspect(context.Context, string) (gitadapter.Snapshot, error) {
	return f.snapshot, f.err
}
