package gitadapter

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
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

func writeFile(t *testing.T, path string, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create parent directory for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func runGit(t *testing.T, repoDir string, args ...string) string {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = repoDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, string(output))
	}
	return string(output)
}

func contains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
