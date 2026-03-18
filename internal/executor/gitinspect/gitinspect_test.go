package gitinspect_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/iw2rmb/amata/internal/executor"
	"github.com/iw2rmb/amata/internal/executor/gitinspect"
	exprruntime "github.com/iw2rmb/amata/internal/expr"
	"github.com/iw2rmb/amata/internal/gitadapter"
	"github.com/iw2rmb/amata/internal/spec"
	"github.com/iw2rmb/amata/internal/state"
	"github.com/iw2rmb/amata/internal/workspace"
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

func TestExecutorReturnsTypedSnapshotOutsideRepository(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	result := gitinspect.New().Execute(context.Background(), executor.StepContext{
		Workspace: workspace.Config{Root: rootDir},
		StepIndex: 1,
		Step: spec.Step{
			Type: "git.inspect",
			Fields: map[string]any{
				"cwd": filepath.Join(rootDir, "missing-repo"),
			},
		},
		Runtime: exprruntime.NewRuntime(map[string]any{}),
	})

	if result.Status != state.StepStatusSucceeded {
		t.Fatalf("result.Status = %q, want %q", result.Status, state.StepStatusSucceeded)
	}

	want := map[string]any{
		"isRepo":  false,
		"hasDiff": false,
		"files":   []string{},
	}
	if !reflect.DeepEqual(result.Value, want) {
		t.Fatalf("result.Value = %#v, want %#v", result.Value, want)
	}
}

func TestExecutorIncludesUntrackedFilesInTypedSnapshot(t *testing.T) {
	t.Parallel()

	repoDir := initInspectRepository(t)
	writeInspectFile(t, filepath.Join(repoDir, "tracked.txt"), "updated\n")
	writeInspectFile(t, filepath.Join(repoDir, "notes", "todo.txt"), "draft\n")
	writeInspectFile(t, filepath.Join(repoDir, "staged.txt"), "staged\n")
	runInspectGit(t, repoDir, "add", "staged.txt")

	result := gitinspect.New().Execute(context.Background(), executor.StepContext{
		Workspace: workspace.Config{Root: repoDir},
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
		"files":   []string{"notes/todo.txt", "staged.txt", "tracked.txt"},
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

func initInspectRepository(t *testing.T) string {
	t.Helper()

	repoDir := t.TempDir()
	runInspectGit(t, repoDir, "init")
	runInspectGit(t, repoDir, "config", "user.name", "Test User")
	runInspectGit(t, repoDir, "config", "user.email", "test@example.com")
	writeInspectFile(t, filepath.Join(repoDir, "tracked.txt"), "base\n")
	runInspectGit(t, repoDir, "add", "tracked.txt")
	runInspectGit(t, repoDir, "commit", "-m", "init")
	return repoDir
}

func writeInspectFile(t *testing.T, path string, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create parent directory for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func runInspectGit(t *testing.T, repoDir string, args ...string) string {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = repoDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, string(output))
	}
	return string(output)
}
