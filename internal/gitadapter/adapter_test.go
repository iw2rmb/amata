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

func TestInspectIncludesUntrackedFilesInSingleSnapshot(t *testing.T) {
	t.Parallel()

	repoDir := initRepository(t)
	writeFile(t, filepath.Join(repoDir, "tracked.txt"), "updated\n")
	writeFile(t, filepath.Join(repoDir, "notes", "todo.txt"), "draft\n")
	writeFile(t, filepath.Join(repoDir, "staged.txt"), "staged\n")
	runGit(t, repoDir, "add", "staged.txt")

	client := New()
	snapshot, err := client.Inspect(context.Background(), filepath.Join(repoDir, "notes"))
	if err != nil {
		t.Fatalf("inspect repository: %v", err)
	}

	if !snapshot.IsRepo {
		t.Fatalf("snapshot.IsRepo = false, want true")
	}
	if !snapshot.HasDiff {
		t.Fatalf("snapshot.HasDiff = false, want true")
	}
	if snapshot.Root != repoDir {
		t.Fatalf("snapshot.Root = %q, want %q", snapshot.Root, repoDir)
	}

	wantFiles := []string{"notes/todo.txt", "staged.txt", "tracked.txt"}
	if !reflect.DeepEqual(snapshot.Files, wantFiles) {
		t.Fatalf("snapshot.Files = %#v, want %#v", snapshot.Files, wantFiles)
	}
}

func TestInspectOutsideRepositoryReturnsTypedEmptyResult(t *testing.T) {
	t.Parallel()

	client := New()
	snapshot, err := client.Inspect(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("inspect non-repository directory: %v", err)
	}

	if snapshot.IsRepo {
		t.Fatalf("snapshot.IsRepo = true, want false")
	}
	if snapshot.HasDiff {
		t.Fatalf("snapshot.HasDiff = true, want false")
	}
	if len(snapshot.Files) != 0 {
		t.Fatalf("snapshot.Files = %#v, want empty", snapshot.Files)
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
	if result.Metadata.ChangedFileCount != 1 {
		t.Fatalf("result.Metadata.ChangedFileCount = %d, want 1", result.Metadata.ChangedFileCount)
	}
	if result.Metadata.Insertions != 1 || result.Metadata.Deletions != 1 {
		t.Fatalf("result.Metadata totals = +%d -%d, want +1 -1", result.Metadata.Insertions, result.Metadata.Deletions)
	}
	wantStats := []CommitFileStat{{Path: "engine.txt", Insertions: 1, Deletions: 1}}
	if !reflect.DeepEqual(result.Metadata.FileStats, wantStats) {
		t.Fatalf("result.Metadata.FileStats = %#v, want %#v", result.Metadata.FileStats, wantStats)
	}

	if got := runGit(t, repoDir, "show", "HEAD:engine.txt"); got != "engine change\n" {
		t.Fatalf("HEAD engine.txt = %q, want committed content", got)
	}
	headFiles := strings.Fields(runGit(t, repoDir, "ls-tree", "--name-only", "-r", "HEAD"))
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

	got := strings.TrimRight(runGit(t, repoDir, "log", "-1", "--pretty=%B"), "\n")
	want := "engine: commit tracked changes\n\nline one\n\nline two"
	if got != want {
		t.Fatalf("commit message body = %q, want %q", got, want)
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
