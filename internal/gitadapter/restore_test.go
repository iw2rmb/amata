package gitadapter

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/iw2rmb/amata/internal/testutil"
)

// Reset preserves the step-entry index and files while removing failed edits.
func TestRepositoryBaselineRestoresEntryState(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(map[bool]string{false: "unborn", true: "committed"}[committed], func(t *testing.T) {
			root := t.TempDir()
			testutil.InitGitRepo(t, root)
			file := filepath.Join(root, "source")
			testutil.WriteFile(t, file, "committed")
			testutil.RunGit(t, root, "add", "source")
			if committed {
				testutil.RunGit(t, root, "commit", "-m", "baseline")
			}
			testutil.WriteFile(t, file, "staged")
			testutil.RunGit(t, root, "add", "source")
			testutil.WriteFile(t, file, "earlier step")
			testutil.WriteFile(t, filepath.Join(root, "untracked"), "keep")
			testutil.WriteFile(t, filepath.Join(root, ".gitignore"), "ignored\n")
			testutil.WriteFile(t, filepath.Join(root, "ignored"), "staged ignored content")
			testutil.RunGit(t, root, "add", "-f", "ignored")
			stateDir := filepath.Join(root, ".amata")
			testutil.WriteFile(t, filepath.Join(stateDir, "log"), "before")
			baseline, err := CaptureBaseline(context.Background(), root, stateDir)
			if err != nil {
				t.Fatal(err)
			}
			defer baseline.Close()
			for i := 0; i < 2; i++ {
				testutil.WriteFile(t, file, "failed agent")
				testutil.WriteFile(t, filepath.Join(root, "new", "failed"), "remove")
				testutil.WriteFile(t, filepath.Join(root, "ignored"), "failed ignored content")
				testutil.RunGit(t, root, "add", "source", "new")
				if err := os.Remove(filepath.Join(root, "untracked")); err != nil {
					t.Fatal(err)
				}
				testutil.WriteFile(t, filepath.Join(stateDir, "log"), "failed attempt evidence")
				testutil.RunGit(t, root, "add", ".amata")
				if i == 1 {
					testutil.RunGit(t, root, "commit", "-m", "agent commit")
				}
				if err := baseline.Restore(context.Background()); err != nil {
					t.Fatal(err)
				}
				testutil.AssertFileContents(t, file, "earlier step")
				testutil.AssertFileContents(t, filepath.Join(root, "ignored"), "staged ignored content")
				testutil.AssertFileContents(t, filepath.Join(root, "untracked"), "keep")
				testutil.AssertFileContents(t, filepath.Join(stateDir, "log"), "failed attempt evidence")
				if _, err := os.Stat(filepath.Join(root, "new", "failed")); !os.IsNotExist(err) {
					t.Fatalf("failed file remains: %v", err)
				}
				if got := testutil.RunGit(t, root, "show", ":source"); got != "staged" {
					t.Fatalf("index = %q", got)
				}
			}
		})
	}
}
