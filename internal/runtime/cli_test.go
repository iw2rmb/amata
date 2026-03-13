package runtime_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"auto/internal/runtime"
	"gopkg.in/yaml.v3"
)

func TestRunCLINormalizesWorkspaceFromSpecAndPersistsLaunchSettings(t *testing.T) {
	specDir := t.TempDir()
	repoRoot := filepath.Join(specDir, "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("mkdir repo root: %v", err)
	}

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

	persistedPath := filepath.Join(repoRoot, "state", "runs", "run-001", "spec.yaml")
	persisted := loadPersistedRunSpec(t, persistedPath)

	if persisted.Launch.RunID != "run-001" {
		t.Fatalf("launch.run_id = %q, want run-001", persisted.Launch.RunID)
	}
	if persisted.Launch.Command != "run" {
		t.Fatalf("launch.command = %q, want run", persisted.Launch.Command)
	}
	if persisted.Spec.Workspace.Root != repoRoot {
		t.Fatalf("spec.workspace.root = %q, want %q", persisted.Spec.Workspace.Root, repoRoot)
	}
	expectedStateDir := filepath.Join(repoRoot, "state")
	if persisted.Spec.Workspace.StateDir != expectedStateDir {
		t.Fatalf("spec.workspace.state_dir = %q, want %q", persisted.Spec.Workspace.StateDir, expectedStateDir)
	}
	if persisted.Launch.Workspace.Root != repoRoot {
		t.Fatalf("launch.workspace.root = %q, want %q", persisted.Launch.Workspace.Root, repoRoot)
	}
	if persisted.Launch.Workspace.StateDir != expectedStateDir {
		t.Fatalf("launch.workspace.state_dir = %q, want %q", persisted.Launch.Workspace.StateDir, expectedStateDir)
	}
	if got := stdout.String(); got != "run-001\n" {
		t.Fatalf("stdout = %q, want run id", got)
	}
}

func TestRunCLIWorkspaceAndRunIDOverrideNormalizedSettings(t *testing.T) {
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

	specDir := filepath.Join(cwd, "specs")
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		t.Fatalf("mkdir spec dir: %v", err)
	}
	specPath := filepath.Join(specDir, "workflow.yaml")
	specBody := `
version: amata/v1
name: sample
entry: main
workspace:
  root: ignored
  state_dir: .state
flows:
  main: {}
`
	if err := os.WriteFile(specPath, []byte(specBody), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	overrideRoot := filepath.Join("workspace-root")
	normalizedCWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd after chdir: %v", err)
	}
	expectedRoot := filepath.Join(normalizedCWD, overrideRoot)
	if err := os.MkdirAll(expectedRoot, 0o755); err != nil {
		t.Fatalf("mkdir override root: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runtime.RunCLI([]string{"run", specPath, "--workspace", overrideRoot, "--run-id", "custom-run"}, &stdout, &stderr); err != nil {
		t.Fatalf("run cli: %v", err)
	}

	persistedPath := filepath.Join(expectedRoot, ".state", "runs", "custom-run", "spec.yaml")
	persisted := loadPersistedRunSpec(t, persistedPath)

	if persisted.Spec.Workspace.Root != expectedRoot {
		t.Fatalf("spec.workspace.root = %q, want %q", persisted.Spec.Workspace.Root, expectedRoot)
	}
	expectedStateDir := filepath.Join(expectedRoot, ".state")
	if persisted.Spec.Workspace.StateDir != expectedStateDir {
		t.Fatalf("spec.workspace.state_dir = %q, want %q", persisted.Spec.Workspace.StateDir, expectedStateDir)
	}
	if persisted.Launch.RunID != "custom-run" {
		t.Fatalf("launch.run_id = %q, want custom-run", persisted.Launch.RunID)
	}
	if persisted.Launch.RunDir != filepath.Join(expectedStateDir, "runs", "custom-run") {
		t.Fatalf("launch.run_dir = %q, want normalized run dir", persisted.Launch.RunDir)
	}
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
