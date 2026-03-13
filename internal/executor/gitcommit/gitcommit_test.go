package gitcommit_test

import (
	"context"
	"reflect"
	"testing"

	"auto/internal/executor"
	"auto/internal/executor/gitcommit"
	exprruntime "auto/internal/expr"
	"auto/internal/gitadapter"
	"auto/internal/spec"
	"auto/internal/state"
	"auto/internal/workspace"
)

func TestExecutorAddsWorkspaceStateDirToExclusions(t *testing.T) {
	t.Parallel()

	service := &fakeCommitService{
		snapshot: gitadapter.Snapshot{
			IsRepo: true,
			Root:   "/repo",
			Files:  []string{"engine.txt"},
		},
		commitResult: gitadapter.CommitResult{
			Committed: true,
			Commit:    "abc123",
			Paths:     []string{"engine.txt"},
		},
	}

	result := gitcommit.NewWithService(service).Execute(context.Background(), executor.StepContext{
		Workspace: workspace.Config{
			Root:     "/repo",
			StateDir: "/repo/.amata",
		},
		StepIndex: 1,
		Step: spec.Step{
			Type: "git.commit",
			Fields: map[string]any{
				"message": "engine: commit",
				"exclude_paths": []any{
					"logs",
				},
			},
		},
		Runtime: exprruntime.NewRuntime(map[string]any{}),
	})

	if result.Status != state.StepStatusSucceeded {
		t.Fatalf("result.Status = %q, want %q", result.Status, state.StepStatusSucceeded)
	}

	wantValue := map[string]any{
		"committed": true,
		"commit":    "abc123",
		"paths":     []string{"engine.txt"},
	}
	if !reflect.DeepEqual(result.Value, wantValue) {
		t.Fatalf("result.Value = %#v, want %#v", result.Value, wantValue)
	}

	wantOptions := gitadapter.CommitOptions{
		Message:      "engine: commit",
		ExcludePaths: []string{"logs", "/repo/.amata"},
	}
	if !reflect.DeepEqual(service.commitOptions, wantOptions) {
		t.Fatalf("service.commitOptions = %#v, want %#v", service.commitOptions, wantOptions)
	}
}

func TestExecutorFailsOutsideRepository(t *testing.T) {
	t.Parallel()

	service := &fakeCommitService{
		snapshot: gitadapter.Snapshot{IsRepo: false},
	}

	result := gitcommit.NewWithService(service).Execute(context.Background(), executor.StepContext{
		Workspace: workspace.Config{
			Root:     "/repo",
			StateDir: "/repo/.amata",
		},
		StepIndex: 3,
		Step: spec.Step{
			Type: "git.commit",
			Fields: map[string]any{
				"message": "engine: commit",
			},
		},
		Runtime: exprruntime.NewRuntime(map[string]any{}),
	})

	if result.Status != state.StepStatusFailed {
		t.Fatalf("result.Status = %q, want %q", result.Status, state.StepStatusFailed)
	}
	if result.Error == nil || result.Error.Code != "not_git_repo" {
		t.Fatalf("result.Error = %#v, want code not_git_repo", result.Error)
	}
}

type fakeCommitService struct {
	snapshot      gitadapter.Snapshot
	inspectErr    error
	commitResult  gitadapter.CommitResult
	commitErr     error
	commitOptions gitadapter.CommitOptions
}

func (f *fakeCommitService) Inspect(context.Context, string) (gitadapter.Snapshot, error) {
	return f.snapshot, f.inspectErr
}

func (f *fakeCommitService) Commit(_ context.Context, _ gitadapter.Snapshot, options gitadapter.CommitOptions) (gitadapter.CommitResult, error) {
	f.commitOptions = options
	return f.commitResult, f.commitErr
}
