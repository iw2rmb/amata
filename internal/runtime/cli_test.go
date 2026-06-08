package runtime_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iw2rmb/amata/internal/progress"
	"github.com/iw2rmb/amata/internal/runtime"
	"github.com/iw2rmb/amata/internal/state"

	"gopkg.in/yaml.v3"
)

func TestRunCLIInterruptHelperProcess(t *testing.T) {
	if os.Getenv("AMATA_RUNCLI_HELPER") != "1" {
		return
	}

	if err := os.Chdir(os.Getenv("AMATA_RUNCLI_CWD")); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	args := []string{
		"run",
		os.Getenv("AMATA_RUNCLI_SPEC"),
		"--run-id",
		os.Getenv("AMATA_RUNCLI_RUN_ID"),
	}
	if err := runtime.RunCLI(args, nil, nil); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func TestRunCLIErrorCases(t *testing.T) {
	testCases := []struct {
		name            string
		args            []string
		wantErrContains string
	}{
		{
			name:            "no args returns usage",
			args:            nil,
			wantErrContains: "usage:",
		},
		{
			name:            "unknown flag",
			args:            []string{"run", "--bogus"},
			wantErrContains: `unknown flag "--bogus"`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := runtime.RunCLI(tc.args, nil, nil)
			if err == nil {
				t.Fatalf("run cli succeeded, want error")
			}
			if !strings.Contains(err.Error(), tc.wantErrContains) {
				t.Fatalf("run cli error = %q, want %q", err, tc.wantErrContains)
			}
		})
	}
}

func TestRunCLIDefaultWorkspaceUsesCWDAndPersistsLaunchSettings(t *testing.T) {
	specDir := t.TempDir()
	repoRoot := filepath.Join(specDir, "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("mkdir repo root: %v", err)
	}
	chdirForTest(t, specDir)

	specPath := filepath.Join(specDir, "workflow.yaml")
	specBody := `
version: amata/v1
name: sample
entry: main
workspace:
  root: repo
  state_dir: state
flows:
  main: {}
`
	if err := os.WriteFile(specPath, []byte(specBody), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runtime.RunCLI([]string{"run", specPath, "--run-id", "run-001"}, &stdout, &stderr); err != nil {
		t.Fatalf("run cli: %v", err)
	}

	persistedPath := filepath.Join(specDir, "state", "runs", "run-001", "spec.yaml")
	persisted := loadPersistedRunSpec(t, persistedPath)

	if persisted.Launch.RunID != "run-001" {
		t.Fatalf("launch.run_id = %q, want run-001", persisted.Launch.RunID)
	}
	if persisted.Launch.Command != "run" {
		t.Fatalf("launch.command = %q, want run", persisted.Launch.Command)
	}
	if canonicalPath(t, persisted.Spec.Workspace.Root) != canonicalPath(t, specDir) {
		t.Fatalf("spec.workspace.root = %q, want %q", persisted.Spec.Workspace.Root, specDir)
	}
	expectedStateDir := filepath.Join(specDir, "state")
	if canonicalPath(t, persisted.Spec.Workspace.StateDir) != canonicalPath(t, expectedStateDir) {
		t.Fatalf("spec.workspace.state_dir = %q, want %q", persisted.Spec.Workspace.StateDir, expectedStateDir)
	}
	if canonicalPath(t, persisted.Launch.Workspace.Root) != canonicalPath(t, specDir) {
		t.Fatalf("launch.workspace.root = %q, want %q", persisted.Launch.Workspace.Root, specDir)
	}
	if canonicalPath(t, persisted.Launch.Workspace.StateDir) != canonicalPath(t, expectedStateDir) {
		t.Fatalf("launch.workspace.state_dir = %q, want %q", persisted.Launch.Workspace.StateDir, expectedStateDir)
	}
	if got := stdout.String(); got != "run-001\n" {
		t.Fatalf("stdout = %q, want run id", got)
	}
}

func TestRunCLIThreadsProgressSinkIntoRunner(t *testing.T) {
	specDir := t.TempDir()
	chdirForTest(t, specDir)
	specPath := filepath.Join(specDir, "workflow.yaml")
	specBody := `
version: amata/v1
name: sample
entry: main
flows:
  main:
    steps:
      - id: step-1
        expr: 1
`
	if err := os.WriteFile(specPath, []byte(specBody), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	var stdout bytes.Buffer
	var events []progress.Event
	sink := progress.SinkFunc(func(event progress.Event) {
		events = append(events, event)
	})

	if err := runtime.RunCLI(
		[]string{"run", specPath, "--run-id", "run-001"},
		&stdout,
		nil,
		runtime.WithProgressSink(sink),
	); err != nil {
		t.Fatalf("run cli: %v", err)
	}

	if got := stdout.String(); got != "run-001\n" {
		t.Fatalf("stdout = %q, want run id", got)
	}
	if got, want := len(events), 4; got != want {
		t.Fatalf("event count = %d, want %d", got, want)
	}
	if events[0].Kind != progress.EventRunStarted {
		t.Fatalf("first event kind = %q, want run_started", events[0].Kind)
	}
	if events[1].Kind != progress.EventStepStarted || events[1].Step == nil || events[1].Step.ID != "step-1" {
		t.Fatalf("step start event = %#v, want step-1 start", events[1])
	}
	if events[2].Kind != progress.EventStepFinished || events[2].Step == nil || events[2].Step.ID != "step-1" {
		t.Fatalf("step finish event = %#v, want step-1 finish", events[2])
	}
	if events[3].Kind != progress.EventRunFinished || events[3].Status != progress.RunStatusSucceeded {
		t.Fatalf("final event = %#v, want succeeded run finish", events[3])
	}
}

func TestRunCLIPlainFallbackWritesProgressToStderr(t *testing.T) {
	specDir := t.TempDir()
	chdirForTest(t, specDir)
	specPath := filepath.Join(specDir, "workflow.yaml")
	specBody := `
version: amata/v1
name: sample
entry: main
flows:
  main:
    steps:
      - assert: true
`
	if err := os.WriteFile(specPath, []byte(specBody), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runtime.RunCLI([]string{"run", specPath, "--run-id", "run-plain"}, &stdout, &stderr); err != nil {
		t.Fatalf("run cli: %v", err)
	}

	if got := stdout.String(); got != "run-plain\n" {
		t.Fatalf("stdout = %q, want run id only", got)
	}

	output := stderr.String()
	for _, want := range []string{"assert true", "⏺", "⏺"} {
		if !strings.Contains(output, want) {
			t.Fatalf("stderr = %q, want %q", output, want)
		}
	}
}

func TestRunCLIExplicitProgressSinkSuppressesDefaultStderrRenderer(t *testing.T) {
	specDir := t.TempDir()
	chdirForTest(t, specDir)
	specPath := filepath.Join(specDir, "workflow.yaml")
	specBody := `
version: amata/v1
name: sample
entry: main
flows:
  main:
    steps:
      - assert: true
`
	if err := os.WriteFile(specPath, []byte(specBody), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var events []progress.Event
	sink := progress.SinkFunc(func(event progress.Event) {
		events = append(events, event)
	})

	if err := runtime.RunCLI(
		[]string{"run", specPath, "--run-id", "run-sink"},
		&stdout,
		&stderr,
		runtime.WithProgressSink(sink),
	); err != nil {
		t.Fatalf("run cli: %v", err)
	}

	if got := stdout.String(); got != "run-sink\n" {
		t.Fatalf("stdout = %q, want run id only", got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty because explicit sink replaces default renderer", stderr.String())
	}
	if len(events) == 0 {
		t.Fatalf("events = %d, want progress callbacks", len(events))
	}
}

func TestRunCLIOutJSONLWritesProgressEventsToStdout(t *testing.T) {
	specDir := t.TempDir()
	chdirForTest(t, specDir)
	specPath := filepath.Join(specDir, "workflow.yaml")
	specBody := `
version: amata/v1
name: sample
entry: main
flows:
  main:
    steps:
      - id: step-1
        expr: 1
`
	if err := os.WriteFile(specPath, []byte(specBody), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runtime.RunCLI(
		[]string{"run", specPath, "--run-id", "run-jsonl", "--out", "jsonl"},
		&stdout,
		&stderr,
	); err != nil {
		t.Fatalf("run cli: %v", err)
	}

	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty for --out jsonl success path", stderr.String())
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("jsonl line count = %d, want 4", len(lines))
	}

	kinds := make([]progress.EventKind, 0, len(lines))
	for index, line := range lines {
		var event progress.Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("decode jsonl event %q: %v", line, err)
		}
		kinds = append(kinds, event.Kind)

		var payload map[string]any
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			t.Fatalf("decode raw jsonl event %q: %v", line, err)
		}
		snapshot, ok := payload["snapshot"].(map[string]any)
		if !ok {
			t.Fatalf("event %d snapshot = %#v, want object", index, payload["snapshot"])
		}
		if event.Kind == progress.EventStepStarted || event.Kind == progress.EventStepFinished || event.Kind == progress.EventRunFinished {
			if _, exists := snapshot["active"]; exists {
				t.Fatalf("event %d (%s) snapshot.active = %#v, want omitted", index, event.Kind, snapshot["active"])
			}
			if _, exists := snapshot["steps"]; exists {
				t.Fatalf("event %d (%s) snapshot.steps = %#v, want omitted", index, event.Kind, snapshot["steps"])
			}
		}
	}

	wantKinds := []progress.EventKind{
		progress.EventRunStarted,
		progress.EventStepStarted,
		progress.EventStepFinished,
		progress.EventRunFinished,
	}
	if fmt.Sprintf("%v", kinds) != fmt.Sprintf("%v", wantKinds) {
		t.Fatalf("event kinds = %v, want %v", kinds, wantKinds)
	}
}

func TestRunCLIOutRejectsUnknownMode(t *testing.T) {
	specDir := t.TempDir()
	chdirForTest(t, specDir)
	specPath := filepath.Join(specDir, "workflow.yaml")
	specBody := `
version: amata/v1
name: sample
entry: main
flows:
  main: {}
`
	if err := os.WriteFile(specPath, []byte(specBody), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	err := runtime.RunCLI([]string{"run", specPath, "--out", "bogus"}, nil, nil)
	if err == nil {
		t.Fatalf("run cli succeeded, want invalid --out error")
	}
	if !strings.Contains(err.Error(), "--out must be one of: auto, jsonl") {
		t.Fatalf("run cli error = %q, want invalid --out message", err)
	}
}

func TestRunCLIWorkspaceNormalizationCases(t *testing.T) {
	testCases := []struct {
		name             string
		specDir          string
		workspaceRoot    string
		stateDir         string
		overrideRoot     string
		runID            string // defaults to "run-001"
		expectedRoot     func(base string) string
		expectedStateDir func(base string) string
		expectedSpecPath func(base string) string
	}{
		{
			name:          "default workspace ignores spec root but keeps state dir",
			specDir:       "specs",
			workspaceRoot: "../repo",
			stateDir:      "state",
			expectedRoot: func(base string) string {
				return base
			},
			expectedStateDir: func(base string) string {
				return filepath.Join(base, "state")
			},
			expectedSpecPath: func(base string) string {
				return filepath.Join(base, "state", "runs", "run-001", "spec.yaml")
			},
		},
		{
			name:    "defaults to cwd and default state dir",
			specDir: "workflow",
			expectedRoot: func(base string) string {
				return base
			},
			expectedStateDir: func(base string) string {
				return filepath.Join(base, ".amata")
			},
			expectedSpecPath: func(base string) string {
				return filepath.Join(base, ".amata", "runs", "run-001", "spec.yaml")
			},
		},
		{
			name:          "cli override resets root but keeps state relative to override",
			specDir:       "specs",
			workspaceRoot: "ignored",
			stateDir:      ".state",
			overrideRoot:  "override-root",
			expectedRoot: func(base string) string {
				return filepath.Join(base, "override-root")
			},
			expectedStateDir: func(base string) string {
				return filepath.Join(base, "override-root", ".state")
			},
			expectedSpecPath: func(base string) string {
				return filepath.Join(base, "override-root", ".state", "runs", "run-001", "spec.yaml")
			},
		},
		{
			name:          "cli override with custom run id normalizes all paths",
			specDir:       "specs",
			workspaceRoot: "ignored",
			stateDir:      ".state",
			overrideRoot:  "workspace-root",
			runID:         "custom-run",
			expectedRoot: func(base string) string {
				return filepath.Join(base, "workspace-root")
			},
			expectedStateDir: func(base string) string {
				return filepath.Join(base, "workspace-root", ".state")
			},
			expectedSpecPath: func(base string) string {
				return filepath.Join(base, "workspace-root", ".state", "runs", "custom-run", "spec.yaml")
			},
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			runID := testCase.runID
			if runID == "" {
				runID = "run-001"
			}

			originalWD, err := os.Getwd()
			if err != nil {
				t.Fatalf("getwd: %v", err)
			}

			cwd := t.TempDir()
			if err := os.Chdir(cwd); err != nil {
				t.Fatalf("chdir: %v", err)
			}
			t.Cleanup(func() {
				if chdirErr := os.Chdir(originalWD); chdirErr != nil {
					t.Fatalf("restore cwd: %v", chdirErr)
				}
			})

			specDir := filepath.Join(cwd, testCase.specDir)
			if err := os.MkdirAll(specDir, 0o755); err != nil {
				t.Fatalf("mkdir spec dir: %v", err)
			}
			specPath := filepath.Join(specDir, "workflow.yaml")
			specBody := `
version: amata/v1
name: sample
entry: main
flows:
  main: {}
`
			if testCase.workspaceRoot != "" || testCase.stateDir != "" {
				specBody = fmt.Sprintf(`
version: amata/v1
name: sample
entry: main
workspace:
  root: %q
  state_dir: %q
flows:
  main: {}
`, testCase.workspaceRoot, testCase.stateDir)
			}
			if err := os.WriteFile(specPath, []byte(specBody), 0o644); err != nil {
				t.Fatalf("write spec: %v", err)
			}

			args := []string{"run", specPath, "--run-id", runID}
			if testCase.overrideRoot != "" {
				if err := os.MkdirAll(filepath.Join(cwd, testCase.overrideRoot), 0o755); err != nil {
					t.Fatalf("mkdir override root: %v", err)
				}
				args = append(args, "--workspace", testCase.overrideRoot)
			}

			var stdout bytes.Buffer
			if err := runtime.RunCLI(args, &stdout, nil); err != nil {
				t.Fatalf("run cli: %v", err)
			}

			persisted := loadPersistedRunSpec(t, testCase.expectedSpecPath(cwd))
			if canonicalPath(t, persisted.Spec.Workspace.Root) != canonicalPath(t, testCase.expectedRoot(cwd)) {
				t.Fatalf("spec.workspace.root = %q, want %q", persisted.Spec.Workspace.Root, testCase.expectedRoot(cwd))
			}
			if canonicalPath(t, persisted.Spec.Workspace.StateDir) != canonicalPath(t, testCase.expectedStateDir(cwd)) {
				t.Fatalf("spec.workspace.state_dir = %q, want %q", persisted.Spec.Workspace.StateDir, testCase.expectedStateDir(cwd))
			}
			if canonicalPath(t, persisted.Launch.Workspace.Root) != canonicalPath(t, testCase.expectedRoot(cwd)) {
				t.Fatalf("launch.workspace.root = %q, want %q", persisted.Launch.Workspace.Root, testCase.expectedRoot(cwd))
			}
			if canonicalPath(t, persisted.Launch.Workspace.StateDir) != canonicalPath(t, testCase.expectedStateDir(cwd)) {
				t.Fatalf("launch.workspace.state_dir = %q, want %q", persisted.Launch.Workspace.StateDir, testCase.expectedStateDir(cwd))
			}
			if canonicalPath(t, persisted.Launch.RunDir) != canonicalPath(t, filepath.Join(testCase.expectedStateDir(cwd), "runs", runID)) {
				t.Fatalf("launch.run_dir = %q, want %q", persisted.Launch.RunDir, filepath.Join(testCase.expectedStateDir(cwd), "runs", runID))
			}
			if persisted.Launch.RunID != runID {
				t.Fatalf("launch.run_id = %q, want %q", persisted.Launch.RunID, runID)
			}
			if got := stdout.String(); got != runID+"\n" {
				t.Fatalf("stdout = %q, want run id", got)
			}
		})
	}
}

func TestRunCLISetOverridesPersistedParams(t *testing.T) {
	specDir := t.TempDir()
	repoRoot := filepath.Join(specDir, "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("mkdir repo root: %v", err)
	}
	chdirForTest(t, repoRoot)

	specPath := filepath.Join(specDir, "workflow.yaml")
	specBody := `
version: amata/v1
name: sample
entry: main
workspace:
  root: repo
params:
  roadmap_file: roadmap/default.md
  dry_run: false
flows:
  main: {}
`
	if err := os.WriteFile(specPath, []byte(specBody), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	if err := runtime.RunCLI([]string{
		"run",
		specPath,
		"--run-id", "run-params",
		"--set", "roadmap_file=roadmap/custom.md",
		"--set", "dry_run=true",
	}, nil, nil); err != nil {
		t.Fatalf("run cli: %v", err)
	}

	persisted := loadPersistedRunSpec(t, filepath.Join(repoRoot, ".amata", "runs", "run-params", "spec.yaml"))
	if got := persisted.Spec.Params["roadmap_file"]; got != "roadmap/custom.md" {
		t.Fatalf("spec.params.roadmap_file = %#v, want %q", got, "roadmap/custom.md")
	}
	if got := persisted.Spec.Params["dry_run"]; got != true {
		t.Fatalf("spec.params.dry_run = %#v, want true", got)
	}
}

func TestRunCLISetRejectsUndeclaredParam(t *testing.T) {
	specDir := t.TempDir()
	specPath := filepath.Join(specDir, "workflow.yaml")
	specBody := `
version: amata/v1
name: sample
entry: main
params:
  roadmap_file: roadmap/index.md
flows:
  main: {}
`
	if err := os.WriteFile(specPath, []byte(specBody), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	err := runtime.RunCLI([]string{"run", specPath, "--set", "unknown=value"}, nil, nil)
	if err == nil {
		t.Fatalf("run cli succeeded, want undeclared param error")
	}
	if !strings.Contains(err.Error(), `param "unknown" is not declared in spec.params`) {
		t.Fatalf("run cli error = %q, want undeclared param message", err)
	}
}

func TestValidateCLICases(t *testing.T) {
	testCases := []struct {
		name            string
		specBody        string
		args            []string
		wantErr         bool
		wantErrContains string
	}{
		{
			name: "succeeds for valid spec",
			specBody: `
version: amata/v1
name: sample
entry: main
flows:
  main: {}
`,
		},
		{
			name: "fails for invalid spec",
			specBody: `
version: amata/v1
name: sample
flows:
  main: {}
`,
			wantErr:         true,
			wantErrContains: "entry is required",
		},
		{
			name:            "requires exactly one spec path",
			args:            []string{"validate"},
			wantErr:         true,
			wantErrContains: "validate requires exactly one spec path",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			args := tc.args
			if args == nil {
				specDir := t.TempDir()
				specPath := filepath.Join(specDir, "workflow.yaml")
				if err := os.WriteFile(specPath, []byte(tc.specBody), 0o644); err != nil {
					t.Fatalf("write spec: %v", err)
				}
				args = []string{"validate", specPath}
			}

			err := runtime.RunCLI(args, nil, nil)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("validate cli succeeded, want error")
				}
				if !strings.Contains(err.Error(), tc.wantErrContains) {
					t.Fatalf("validate cli error = %q, want %q", err, tc.wantErrContains)
				}
			} else {
				if err != nil {
					t.Fatalf("validate cli: %v", err)
				}
			}
		})
	}
}

func TestResumeCLIRestartsFromStepAfterInterruptedBoundary(t *testing.T) {
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	cwd := t.TempDir()
	if err := os.Chdir(cwd); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		if chdirErr := os.Chdir(originalWD); chdirErr != nil {
			t.Fatalf("restore cwd: %v", chdirErr)
		}
	})

	repoRoot := filepath.Join(cwd, "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("mkdir repo root: %v", err)
	}

	specPath := filepath.Join(cwd, "workflow.yaml")
	specBody := `
version: amata/v1
name: sample
entry: main
workspace:
  root: repo
  state_dir: .amata
flows:
  main:
    steps:
      - id: step-1
        command: printf 'step-1\n' >> first-count.txt
      - id: step-2
        command: while [ ! -f proceed.txt ]; do sleep 0.1; done; printf 'step-2\n' > second.txt
      - id: step-3
        command: printf 'step-3\n' > third.txt
`
	if err := os.WriteFile(specPath, []byte(specBody), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestRunCLIInterruptHelperProcess$")
	cmd.Env = append(os.Environ(),
		"AMATA_RUNCLI_HELPER=1",
		"AMATA_RUNCLI_CWD="+repoRoot,
		"AMATA_RUNCLI_SPEC="+specPath,
		"AMATA_RUNCLI_RUN_ID=run-001",
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper process: %v", err)
	}

	runDir := filepath.Join(repoRoot, ".amata", "runs", "run-001")
	store := state.NewStore(runDir)
	waitForInterruptBoundary(t, store.EventsPath(), filepath.Join(repoRoot, "first-count.txt"))

	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill helper process: %v", err)
	}
	_ = cmd.Wait()

	if _, err := os.Stat(filepath.Join(repoRoot, "second.txt")); !os.IsNotExist(err) {
		t.Fatalf("second step completed before interruption, err = %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "proceed.txt"), []byte("go"), 0o644); err != nil {
		t.Fatalf("write proceed marker: %v", err)
	}

	if err := runtime.RunCLI([]string{"resume", "run-001"}, nil, nil); err != nil {
		t.Fatalf("resume cli: %v", err)
	}

	if got := strings.TrimSpace(readTextFile(t, filepath.Join(repoRoot, "first-count.txt"))); got != "step-1" {
		t.Fatalf("first-count.txt = %q, want single step-1 record", got)
	}
	if got := strings.TrimSpace(readTextFile(t, filepath.Join(repoRoot, "second.txt"))); got != "step-2" {
		t.Fatalf("second.txt = %q, want step-2", got)
	}
	if got := strings.TrimSpace(readTextFile(t, filepath.Join(repoRoot, "third.txt"))); got != "step-3" {
		t.Fatalf("third.txt = %q, want step-3", got)
	}

	snapshot, err := store.LoadSnapshot()
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if len(snapshot.Steps) != 3 {
		t.Fatalf("step count = %d, want 3", len(snapshot.Steps))
	}
}

func waitForInterruptBoundary(t *testing.T, eventsPath string, firstCountPath string) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		eventsData, err := os.ReadFile(eventsPath)
		if err == nil && strings.Count(strings.TrimSpace(string(eventsData)), "\n")+1 >= 2 {
			if got := strings.TrimSpace(readTextFile(t, firstCountPath)); got == "step-1" {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for first completed step to persist")
}

func chdirForTest(t *testing.T, dir string) {
	t.Helper()

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() {
		if chdirErr := os.Chdir(originalWD); chdirErr != nil {
			t.Fatalf("restore cwd: %v", chdirErr)
		}
	})
}

func loadPersistedRunSpec(t *testing.T, path string) runtime.PersistedRunSpec {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read persisted spec: %v", err)
	}

	var persisted runtime.PersistedRunSpec
	if err := yaml.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("unmarshal persisted spec: %v", err)
	}

	return persisted
}

func readTextFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return string(data)
}

func canonicalPath(t *testing.T, path string) string {
	t.Helper()

	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved
	}
	if !os.IsNotExist(err) {
		t.Fatalf("eval symlinks for %s: %v", path, err)
	}
	return filepath.Clean(path)
}
