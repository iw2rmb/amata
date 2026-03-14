package gitcommit_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
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
			Metadata: &gitadapter.CommitMetadata{
				ShortCommit:      "abc123",
				ChangedFileCount: 1,
				Insertions:       5,
				Deletions:        2,
				FileStats: []gitadapter.CommitFileStat{
					{Path: "engine.txt", Insertions: 5, Deletions: 2},
				},
			},
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
		"repoRoot":  "/repo",
		"metadata": map[string]any{
			"shortCommit":      "abc123",
			"changedFileCount": 1,
			"insertions":       5,
			"deletions":        2,
			"files": []any{
				map[string]any{
					"path":       "engine.txt",
					"insertions": 5,
					"deletions":  2,
				},
			},
		},
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

	rootDir := t.TempDir()
	result := gitcommit.New().Execute(context.Background(), executor.StepContext{
		Workspace: workspace.Config{
			Root:     rootDir,
			StateDir: filepath.Join(rootDir, ".amata"),
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

func TestExecutorReturnsTypedNoOpWhenOnlyExcludedStateDirChanged(t *testing.T) {
	t.Parallel()

	repoDir := initCommitRepository(t)
	stateDir := filepath.Join(repoDir, ".amata")
	writeCommitFile(t, filepath.Join(stateDir, "runs", "current.json"), "{\"run\":1}\n")
	runCommitGit(t, repoDir, "add", filepath.Join(".amata", "runs", "current.json"))

	result := gitcommit.New().Execute(context.Background(), executor.StepContext{
		Workspace: workspace.Config{
			Root:     repoDir,
			StateDir: stateDir,
		},
		StepIndex: 1,
		Step: spec.Step{
			Type: "git.commit",
			Fields: map[string]any{
				"message": "engine: commit tracked changes",
			},
		},
		Runtime: exprruntime.NewRuntime(map[string]any{}),
	})

	if result.Status != state.StepStatusSucceeded {
		t.Fatalf("result.Status = %q, want %q", result.Status, state.StepStatusSucceeded)
	}

	want := map[string]any{
		"committed": false,
		"commit":    nil,
		"paths":     []string{},
		"repoRoot":  repoDir,
		"metadata":  nil,
	}
	if !reflect.DeepEqual(result.Value, want) {
		t.Fatalf("result.Value = %#v, want %#v", result.Value, want)
	}

	staged := strings.Fields(runCommitGit(t, repoDir, "diff", "--cached", "--name-only"))
	if got, want := staged, []string{".amata/runs/current.json"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("staged paths = %#v, want %#v", got, want)
	}
}

func TestExecutorCommitsUntrackedFilesAndPreservesExcludedPaths(t *testing.T) {
	t.Parallel()

	repoDir := initCommitRepository(t)
	stateDir := filepath.Join(repoDir, ".amata")
	writeCommitFile(t, filepath.Join(repoDir, "engine.txt"), "engine change\n")
	writeCommitFile(t, filepath.Join(repoDir, "notes", "todo.txt"), "draft\n")
	writeCommitFile(t, filepath.Join(stateDir, "runs", "current.json"), "{\"run\":1}\n")
	writeCommitFile(t, filepath.Join(repoDir, "docs", "review.md"), "leave staged\n")
	runCommitGit(t, repoDir, "add", filepath.Join(".amata", "runs", "current.json"), filepath.Join("docs", "review.md"))

	result := gitcommit.New().Execute(context.Background(), executor.StepContext{
		Workspace: workspace.Config{
			Root:     repoDir,
			StateDir: stateDir,
		},
		StepIndex: 2,
		Step: spec.Step{
			Type: "git.commit",
			Fields: map[string]any{
				"message": "engine: commit tracked changes",
				"exclude_paths": []any{
					"docs",
				},
			},
		},
		Runtime: exprruntime.NewRuntime(map[string]any{}),
	})

	if result.Status != state.StepStatusSucceeded {
		t.Fatalf("result.Status = %q, want %q", result.Status, state.StepStatusSucceeded)
	}

	value, ok := result.Value.(map[string]any)
	if !ok {
		t.Fatalf("result.Value type = %T, want map[string]any", result.Value)
	}
	if value["committed"] != true {
		t.Fatalf("committed = %#v, want true", value["committed"])
	}
	commit, ok := value["commit"].(string)
	if !ok || commit == "" {
		t.Fatalf("commit = %#v, want sha string", value["commit"])
	}

	wantPaths := []string{"engine.txt", "notes/todo.txt"}
	if got := value["paths"]; !reflect.DeepEqual(got, wantPaths) {
		t.Fatalf("paths = %#v, want %#v", got, wantPaths)
	}
	metadata, ok := value["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata = %#v, want map[string]any", value["metadata"])
	}
	if metadata["shortCommit"] == "" {
		t.Fatalf("metadata.shortCommit = %#v, want short sha", metadata["shortCommit"])
	}
	if metadata["changedFileCount"] != 2 {
		t.Fatalf("metadata.changedFileCount = %#v, want 2", metadata["changedFileCount"])
	}
	if metadata["insertions"] != 2 || metadata["deletions"] != 1 {
		t.Fatalf("metadata totals = +%#v -%#v, want +2 -1", metadata["insertions"], metadata["deletions"])
	}
	wantStats := []any{
		map[string]any{"path": "engine.txt", "insertions": 1, "deletions": 1},
		map[string]any{"path": "notes/todo.txt", "insertions": 1, "deletions": 0},
	}
	if got := metadata["files"]; !reflect.DeepEqual(got, wantStats) {
		t.Fatalf("metadata.files = %#v, want %#v", got, wantStats)
	}

	if got := runCommitGit(t, repoDir, "show", "HEAD:engine.txt"); got != "engine change\n" {
		t.Fatalf("HEAD engine.txt = %q, want committed content", got)
	}
	if got := runCommitGit(t, repoDir, "show", "HEAD:notes/todo.txt"); got != "draft\n" {
		t.Fatalf("HEAD notes/todo.txt = %q, want committed content", got)
	}

	headFiles := strings.Fields(runCommitGit(t, repoDir, "ls-tree", "--name-only", "-r", "HEAD"))
	if containsCommitPath(headFiles, ".amata/runs/current.json") {
		t.Fatalf("HEAD files = %#v, want excluded state file to remain absent", headFiles)
	}
	if containsCommitPath(headFiles, "docs/review.md") {
		t.Fatalf("HEAD files = %#v, want excluded docs file to remain absent", headFiles)
	}

	staged := strings.Fields(runCommitGit(t, repoDir, "diff", "--cached", "--name-only"))
	wantStaged := []string{".amata/runs/current.json", "docs/review.md"}
	if !reflect.DeepEqual(staged, wantStaged) {
		t.Fatalf("staged paths = %#v, want %#v", staged, wantStaged)
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

func initCommitRepository(t *testing.T) string {
	t.Helper()

	repoDir := t.TempDir()
	runCommitGit(t, repoDir, "init")
	runCommitGit(t, repoDir, "config", "user.name", "Test User")
	runCommitGit(t, repoDir, "config", "user.email", "test@example.com")

	writeCommitFile(t, filepath.Join(repoDir, "engine.txt"), "base\n")
	runCommitGit(t, repoDir, "add", "engine.txt")
	runCommitGit(t, repoDir, "commit", "-m", "init")
	return repoDir
}

func writeCommitFile(t *testing.T, path string, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create parent directory for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func runCommitGit(t *testing.T, repoDir string, args ...string) string {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = repoDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, string(output))
	}
	return string(output)
}

func containsCommitPath(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
