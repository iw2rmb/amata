package runtime_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	runtimelib "github.com/iw2rmb/amata/internal/runtime"
	"github.com/iw2rmb/amata/internal/state"
)

func TestNessyShorthandInvokesProcessWithoutModel(t *testing.T) {
	repoDir := t.TempDir()
	logPath := filepath.Join(repoDir, "invocation.json")
	t.Setenv("NESSY_INVOCATION_LOG", logPath)
	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, "nessy"), `#!/usr/bin/env python3
import json, os, sys
prompt = sys.stdin.read()
if not prompt:
    sys.stderr.write('No input provided via stdin\n')
    sys.exit(1)
with open(os.environ['NESSY_INVOCATION_LOG'], 'w') as f:
    json.dump({'args': sys.argv[1:], 'cwd': os.getcwd(), 'env': os.environ['NESSY_TEST'], 'stdin': prompt}, f)
sys.stdout.write('nessy completed\n')
sys.stderr.write('nessy diagnostic\n')
`)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	specPath := filepath.Join(repoDir, "workflow.yaml")
	specBody := `version: amata/v1
name: nessy-wiring
entry: main
params:
  task: "--fix the 'feature'"
defaults:
  executors:
    nessy:
      env:
        NESSY_TEST: from-defaults
flows:
  main:
    steps:
      - id: invoke-nessy
        nessy: $.params.task
      - assert:
          expr: 'ctx.prev.value == "nessy completed\n"'
`
	if err := os.WriteFile(specPath, []byte(specBody), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runtimelib.RunCLI([]string{"run", specPath, "--workspace", repoDir, "--run-id", "nessy-wiring"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	store := state.NewStore(filepath.Join(repoDir, ".amata", "runs", "nessy-wiring"))
	snapshot, err := store.LoadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != state.RunStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", snapshot.Status)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	var invocation struct {
		Args            []string
		CWD, Env, Stdin string
	}
	if err := json.Unmarshal(data, &invocation); err != nil {
		t.Fatal(err)
	}
	if len(invocation.Args) != 6 || invocation.Args[5] == "" {
		t.Fatalf("session args = %q", invocation.Args)
	}
	wantArgs := []string{"--auth-type", "openai", "--chat-recording", "true", "--session-id", invocation.Args[5]}
	if !reflect.DeepEqual(invocation.Args, wantArgs) || invocation.Env != "from-defaults" || invocation.Stdin != "--fix the 'feature'" {
		t.Fatalf("invocation = %#v", invocation)
	}
	wantCWD, err := filepath.EvalSymlinks(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	if invocation.CWD != wantCWD {
		t.Fatalf("cwd = %q, want %q", invocation.CWD, wantCWD)
	}
	if len(snapshot.Steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(snapshot.Steps))
	}
	stepDir := filepath.Dir(snapshot.Steps[0].Artifacts.Files["prompt"])
	for name, want := range map[string]string{
		"prompt.md":      "--fix the 'feature'",
		"stdout.txt":     "nessy completed\n",
		"stderr.txt":     "nessy diagnostic\n",
		"transcript.txt": "nessy completed\n",
	} {
		data, err := os.ReadFile(filepath.Join(stepDir, name))
		if err != nil || string(data) != want {
			t.Fatalf("artifact %s = %q, error %v, want %q", name, data, err, want)
		}
	}
}
