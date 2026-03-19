package runtime_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	runtimelib "github.com/iw2rmb/amata/internal/runtime"
	"github.com/iw2rmb/amata/internal/state"
)

// TestCrushProcessInvocationWiring proves that the crush builtin executor
// locates the crush binary on PATH, passes the correct CLI flags, and
// delivers the rendered prompt on stdin.  A fake crush binary records its
// invocation so the test can assert exact arg and stdin content without
// relying on the real crush CLI.
func TestCrushProcessInvocationWiring(t *testing.T) {
	repoDir := t.TempDir()
	invocationsLog := filepath.Join(repoDir, "crush-invocations.log")
	t.Setenv("CRUSH_INVOCATIONS_LOG", invocationsLog)

	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, "crush"), fakeCrushScript)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	specPath := filepath.Join(repoDir, "workflow.yaml")
	specBody := `version: amata/v1
name: crush-wiring
entry: main
defaults:
  executors:
    crush:
      model: claude-sonnet-4-5
flows:
  main:
    steps:
      - id: invoke-crush
        crush: implement the feature
`
	if err := os.WriteFile(specPath, []byte(specBody), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	if err := runtimelib.RunCLI([]string{
		"run",
		specPath,
		"--workspace", repoDir,
		"--run-id", "run-crush-wiring",
	}, nil, nil); err != nil {
		t.Fatalf("run cli: %v", err)
	}

	store := state.NewStore(filepath.Join(repoDir, ".amata", "runs", "run-crush-wiring"))
	snapshot, err := store.LoadSnapshot()
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if snapshot.Status != state.RunStatusSucceeded {
		t.Fatalf("snapshot status = %q, want succeeded", snapshot.Status)
	}

	data, err := os.ReadFile(invocationsLog)
	if err != nil {
		t.Fatalf("read invocations log: %v", err)
	}
	invocations := string(data)

	for _, want := range []string{"run", "--yolo", "--quiet", "--model", "claude-sonnet-4-5"} {
		if !strings.Contains(invocations, want) {
			t.Fatalf("invocations = %q, missing expected arg %q", invocations, want)
		}
	}
	if !strings.Contains(invocations, "implement the feature") {
		t.Fatalf("invocations = %q, want prompt on stdin", invocations)
	}
}

const fakeCrushScript = `#!/usr/bin/env python3
import os, sys

prompt = sys.stdin.read()
log_path = os.environ.get('CRUSH_INVOCATIONS_LOG', '')
if log_path:
    with open(log_path, 'a') as f:
        f.write(' '.join(sys.argv[1:]) + '\n')
        f.write('stdin:' + prompt + '\n')

sys.stdout.write('crush completed\n')
`
