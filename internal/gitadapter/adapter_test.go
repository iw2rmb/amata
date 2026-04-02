package gitadapter

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/iw2rmb/amata/internal/testutil"
)

func TestInspectSnapshotCases(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		setup     func(t *testing.T) string // returns inspect path
		wantRepo  bool
		wantDiff  bool
		wantFiles []string
		checkRoot func(t *testing.T, root string)
	}{
		{
			name: "includes untracked files in single snapshot",
			setup: func(t *testing.T) string {
				t.Helper()
				repoDir := initRepository(t)
				writeFile(t, filepath.Join(repoDir, "tracked.txt"), "updated\n")
				writeFile(t, filepath.Join(repoDir, "notes", "todo.txt"), "draft\n")
				writeFile(t, filepath.Join(repoDir, "staged.txt"), "staged\n")
				runGit(t, repoDir, "add", "staged.txt")
				return filepath.Join(repoDir, "notes")
			},
			wantRepo:  true,
			wantDiff:  true,
			wantFiles: []string{"notes/todo.txt", "staged.txt", "tracked.txt"},
		},
		{
			name: "outside repository returns typed empty result",
			setup: func(t *testing.T) string {
				t.Helper()
				return t.TempDir()
			},
			wantRepo:  false,
			wantDiff:  false,
			wantFiles: nil,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			inspectPath := tc.setup(t)
			client := New()
			snapshot, err := client.Inspect(context.Background(), inspectPath)
			if err != nil {
				t.Fatalf("inspect: %v", err)
			}

			if snapshot.IsRepo != tc.wantRepo {
				t.Fatalf("snapshot.IsRepo = %v, want %v", snapshot.IsRepo, tc.wantRepo)
			}
			if snapshot.HasDiff != tc.wantDiff {
				t.Fatalf("snapshot.HasDiff = %v, want %v", snapshot.HasDiff, tc.wantDiff)
			}
			if tc.wantFiles == nil {
				if len(snapshot.Files) != 0 {
					t.Fatalf("snapshot.Files = %#v, want empty", snapshot.Files)
				}
			} else if !reflect.DeepEqual(snapshot.Files, tc.wantFiles) {
				t.Fatalf("snapshot.Files = %#v, want %#v", snapshot.Files, tc.wantFiles)
			}
		})
	}
}

func TestCommitExcludesAbsolutePrefixesAndKeepsExcludedStagedChanges(t *testing.T) {
	t.Parallel()

	repoDir := initRepository(t)
	stateDir := filepath.Join(repoDir, ".amata")

	writeFile(t, filepath.Join(repoDir, "engine.txt"), "engine change\n")
	writeFile(t, filepath.Join(stateDir, "runs", "current.json"), "{\"run\":1}\n")
	runGit(t, repoDir, "add", filepath.Join(".amata", "runs", "current.json"))

	client := New()
	snapshot, err := client.Inspect(context.Background(), repoDir)
	if err != nil {
		t.Fatalf("inspect repository: %v", err)
	}

	result, err := client.Commit(context.Background(), snapshot, CommitOptions{
		Message:      "engine: commit tracked changes",
		ExcludePaths: []string{stateDir},
	})
	if err != nil {
		t.Fatalf("commit repository changes: %v", err)
	}

	wantPaths := []string{"engine.txt"}
	if !reflect.DeepEqual(result.Paths, wantPaths) {
		t.Fatalf("result.Paths = %#v, want %#v", result.Paths, wantPaths)
	}
	if !result.Committed {
		t.Fatalf("result.Committed = false, want true")
	}
	if result.Commit == "" {
		t.Fatalf("result.Commit = empty, want sha")
	}
	if result.Metadata == nil {
		t.Fatalf("result.Metadata = nil, want commit stats")
	}
	if result.Metadata.ShortCommit == "" {
		t.Fatalf("result.Metadata.ShortCommit = empty, want short sha")
	}
	if result.Metadata.Insertions != 1 || result.Metadata.Deletions != 1 {
		t.Fatalf("result.Metadata totals = +%d -%d, want +1 -1", result.Metadata.Insertions, result.Metadata.Deletions)
	}
	wantStats := []CommitFileStat{{Path: "engine.txt", Insertions: 1, Deletions: 1}}
	if !reflect.DeepEqual(result.Metadata.FileStats, wantStats) {
		t.Fatalf("result.Metadata.FileStats = %#v, want %#v", result.Metadata.FileStats, wantStats)
	}

	if got := runGit(t, repoDir, "show", result.Commit+":engine.txt"); got != "engine change\n" {
		t.Fatalf("HEAD engine.txt = %q, want committed content", got)
	}
	headFiles := strings.Fields(runGit(t, repoDir, "ls-tree", "--name-only", "-r", result.Commit))
	if contains(headFiles, ".amata/runs/current.json") {
		t.Fatalf("HEAD files = %#v, want excluded state file to remain absent", headFiles)
	}

	staged := strings.Fields(runGit(t, repoDir, "diff", "--cached", "--name-only"))
	wantStaged := []string{".amata/runs/current.json"}
	if !reflect.DeepEqual(staged, wantStaged) {
		t.Fatalf("staged paths = %#v, want %#v", staged, wantStaged)
	}
}

func TestCommitIncludesBodyInDescription(t *testing.T) {
	t.Parallel()

	repoDir := initRepository(t)
	writeFile(t, filepath.Join(repoDir, "engine.txt"), "engine change\n")

	client := New()
	snapshot, err := client.Inspect(context.Background(), repoDir)
	if err != nil {
		t.Fatalf("inspect repository: %v", err)
	}

	result, err := client.Commit(context.Background(), snapshot, CommitOptions{
		Message: "engine: commit tracked changes",
		Body:    "line one\n\nline two",
	})
	if err != nil {
		t.Fatalf("commit repository changes: %v", err)
	}
	if !result.Committed {
		t.Fatalf("result.Committed = false, want true")
	}

	got := strings.TrimRight(runGit(t, repoDir, "show", "-s", "--format=%B", result.Commit), "\n")
	want := "engine: commit tracked changes\n\nline one\n\nline two"
	if got != want {
		t.Fatalf("commit message body = %q, want %q", got, want)
	}
}

func TestCommitReturnsOriginalCommitWhenPostCommitHookAddsAnotherCommit(t *testing.T) {
	t.Parallel()

	repoDir := initRepository(t)
	headBefore := strings.TrimSpace(runGit(t, repoDir, "rev-parse", "HEAD"))

	hooksDir := filepath.Join(repoDir, ".githooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatalf("create hooks dir: %v", err)
	}
	hookPath := filepath.Join(hooksDir, "post-commit")
	hook := "#!/bin/sh\n" +
		"if [ -f .post-commit-done ]; then\n" +
		"  exit 0\n" +
		"fi\n" +
		"echo done > .post-commit-done\n" +
		"git add .post-commit-done\n" +
		"git commit --quiet -m \"hook: follow-up\"\n"
	if err := os.WriteFile(hookPath, []byte(hook), 0o755); err != nil {
		t.Fatalf("write post-commit hook: %v", err)
	}
	runGit(t, repoDir, "config", "core.hooksPath", ".githooks")

	writeFile(t, filepath.Join(repoDir, "engine.txt"), "engine change\n")

	client := New()
	snapshot, err := client.Inspect(context.Background(), repoDir)
	if err != nil {
		t.Fatalf("inspect repository: %v", err)
	}

	result, err := client.Commit(context.Background(), snapshot, CommitOptions{
		Message: "engine: commit tracked changes",
	})
	if err != nil {
		t.Fatalf("commit repository changes: %v", err)
	}
	if !result.Committed {
		t.Fatalf("result.Committed = false, want true")
	}

	headAfter := strings.TrimSpace(runGit(t, repoDir, "rev-parse", "HEAD"))
	if headAfter == headBefore {
		t.Fatalf("HEAD after commit = %q, want new commit", headAfter)
	}
	if result.Commit == headAfter {
		t.Fatalf("result.Commit = %q, want original commit instead of final HEAD", result.Commit)
	}

	originalRange := strings.Fields(runGit(t, repoDir, "rev-list", "--reverse", "--ancestry-path", headBefore+".."+headAfter))
	if len(originalRange) < 2 {
		t.Fatalf("rev-list range = %#v, want original commit + hook commit", originalRange)
	}
	if got, want := result.Commit, originalRange[0]; got != want {
		t.Fatalf("result.Commit = %q, want %q", got, want)
	}

	subject := strings.TrimSpace(runGit(t, repoDir, "show", "-s", "--format=%s", result.Commit))
	if got, want := subject, "engine: commit tracked changes"; got != want {
		t.Fatalf("original commit subject = %q, want %q", got, want)
	}
	if result.Metadata == nil {
		t.Fatalf("result.Metadata = nil, want metadata")
	}
	if result.Metadata.ShortCommit == "" {
		t.Fatalf("result.Metadata.ShortCommit = empty, want short sha")
	}
}

func TestCommitHandlesMissingIncludedPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		setup         func(t *testing.T, repoDir string)
		files         []string
		wantCommitted bool
		wantPaths     []string
		verify        func(t *testing.T, repoDir string, result CommitResult)
	}{
		{
			name: "commits remaining stageable paths",
			setup: func(t *testing.T, repoDir string) {
				t.Helper()
				writeFile(t, filepath.Join(repoDir, "engine.txt"), "engine change\n")
			},
			files:         []string{"engine.txt", "internal/workflow/contracts/mods_spec.go"},
			wantCommitted: true,
			wantPaths:     []string{"engine.txt"},
			verify: func(t *testing.T, repoDir string, _ CommitResult) {
				t.Helper()
				if got := runGit(t, repoDir, "show", "HEAD:engine.txt"); got != "engine change\n" {
					t.Fatalf("HEAD engine.txt = %q, want committed content", got)
				}
			},
		},
		{
			name:          "returns no-op when only missing paths remain",
			files:         []string{"internal/workflow/contracts/mods_spec.go"},
			wantCommitted: false,
			wantPaths:     []string{},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repoDir := initRepository(t)
			if tt.setup != nil {
				tt.setup(t, repoDir)
			}

			client := New()
			result, err := client.Commit(context.Background(), Snapshot{
				IsRepo: true,
				Root:   repoDir,
				Files:  tt.files,
			}, CommitOptions{
				Message: "engine: commit tracked changes",
			})
			if err != nil {
				t.Fatalf("commit repository changes: %v", err)
			}

			if result.Committed != tt.wantCommitted {
				t.Fatalf("result.Committed = %v, want %v", result.Committed, tt.wantCommitted)
			}
			if !reflect.DeepEqual(result.Paths, tt.wantPaths) {
				t.Fatalf("result.Paths = %#v, want %#v", result.Paths, tt.wantPaths)
			}

			if tt.wantCommitted {
				if result.Commit == "" {
					t.Fatalf("result.Commit = empty, want sha")
				}
			} else if result.Commit != "" {
				t.Fatalf("result.Commit = %q, want empty", result.Commit)
			}

			if tt.verify != nil {
				tt.verify(t, repoDir, result)
			}
		})
	}
}

func TestCommitStagesTrackedDeletionWhenPathMissingFromWorktree(t *testing.T) {
	t.Parallel()

	repoDir := initRepository(t)
	if err := os.Remove(filepath.Join(repoDir, "tracked.txt")); err != nil {
		t.Fatalf("remove tracked file: %v", err)
	}

	client := New()
	snapshot, err := client.Inspect(context.Background(), repoDir)
	if err != nil {
		t.Fatalf("inspect repository: %v", err)
	}

	result, err := client.Commit(context.Background(), snapshot, CommitOptions{
		Message: "engine: remove tracked file",
	})
	if err != nil {
		t.Fatalf("commit repository changes: %v", err)
	}
	if !result.Committed {
		t.Fatalf("result.Committed = false, want true")
	}

	wantPaths := []string{"tracked.txt"}
	if !reflect.DeepEqual(result.Paths, wantPaths) {
		t.Fatalf("result.Paths = %#v, want %#v", result.Paths, wantPaths)
	}

	headFiles := strings.Fields(runGit(t, repoDir, "ls-tree", "--name-only", "-r", "HEAD"))
	if contains(headFiles, "tracked.txt") {
		t.Fatalf("HEAD files = %#v, want tracked.txt to be removed", headFiles)
	}
}

func TestFilterPathsUsesDirectoryPrefixSemantics(t *testing.T) {
	t.Parallel()

	filtered, err := filterPaths(
		[]string{"dir/file.txt", "dirname/file.txt", "dir.txt"},
		"/repo",
		[]string{"./dir"},
	)
	if err != nil {
		t.Fatalf("filter paths: %v", err)
	}

	want := []string{"dirname/file.txt", "dir.txt"}
	if !reflect.DeepEqual(filtered, want) {
		t.Fatalf("filtered = %#v, want %#v", filtered, want)
	}
}

func TestParseCommitFileStatsHandlesTextAndBinaryEntries(t *testing.T) {
	t.Parallel()

	stats, err := parseCommitFileStats([]byte("3\t1\tengine.txt\n-\t-\tassets/logo.png\n"))
	if err != nil {
		t.Fatalf("parse commit file stats: %v", err)
	}

	want := []CommitFileStat{
		{Path: "engine.txt", Insertions: 3, Deletions: 1},
		{Path: "assets/logo.png", Insertions: 0, Deletions: 0},
	}
	if !reflect.DeepEqual(stats, want) {
		t.Fatalf("stats = %#v, want %#v", stats, want)
	}
}

func initRepository(t *testing.T) string {
	t.Helper()

	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "config", "user.name", "Test User")
	runGit(t, repoDir, "config", "user.email", "test@example.com")
	if err := os.MkdirAll(filepath.Join(repoDir, ".githooks"), 0o755); err != nil {
		t.Fatalf("create hooks dir: %v", err)
	}
	runGit(t, repoDir, "config", "core.hooksPath", ".githooks")

	writeFile(t, filepath.Join(repoDir, "tracked.txt"), "base\n")
	writeFile(t, filepath.Join(repoDir, "engine.txt"), "base\n")
	runGit(t, repoDir, "add", "tracked.txt", "engine.txt")
	runGit(t, repoDir, "commit", "-m", "init")

	return repoDir
}

var writeFile = testutil.WriteFile

var runGit = testutil.RunGit

func contains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
