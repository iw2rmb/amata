package spec_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"auto/internal/spec"
)

func TestLoadParsesSupportedTopLevelFields(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	specPath := filepath.Join(tempDir, "workflow.yaml")
	specBody := `
version: amata/v1
name: sample
entry: main
workspace:
  root: .
  state_dir: state
params:
  key: value
defaults:
  cwd: .
schemas:
  sample:
    type: object
flows:
  main:
    steps:
      - type: shell
        command: echo hi
`

	if err := os.WriteFile(specPath, []byte(specBody), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	loaded, err := spec.Load(specPath)
	if err != nil {
		t.Fatalf("load spec: %v", err)
	}

	if loaded.Spec.Version != spec.Version {
		t.Fatalf("version = %q, want %q", loaded.Spec.Version, spec.Version)
	}
	if loaded.Spec.Name != "sample" {
		t.Fatalf("name = %q, want sample", loaded.Spec.Name)
	}
	if loaded.Spec.Entry != "main" {
		t.Fatalf("entry = %q, want main", loaded.Spec.Entry)
	}
	if loaded.Spec.Workspace.Root != "." {
		t.Fatalf("workspace.root = %q, want .", loaded.Spec.Workspace.Root)
	}
	if loaded.Spec.Workspace.StateDir != "state" {
		t.Fatalf("workspace.state_dir = %q, want state", loaded.Spec.Workspace.StateDir)
	}
	if got := loaded.Spec.Params["key"]; got != "value" {
		t.Fatalf("params[key] = %#v, want value", got)
	}
	if _, ok := loaded.Spec.Defaults["cwd"]; !ok {
		t.Fatalf("defaults[cwd] missing")
	}
	if _, ok := loaded.Spec.Schemas["sample"]; !ok {
		t.Fatalf("schemas[sample] missing")
	}
	if _, ok := loaded.Spec.Flows["main"]; !ok {
		t.Fatalf("flows[main] missing")
	}
}

func TestLoadRejectsInvalidWorkspaceShape(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		specBody string
	}{
		{
			name: "workspace scalar",
			specBody: `
version: amata/v1
name: sample
entry: main
workspace: invalid
flows:
  main: {}
`,
		},
		{
			name: "workspace root non string",
			specBody: `
version: amata/v1
name: sample
entry: main
workspace:
  root:
    nested: true
flows:
  main: {}
`,
		},
		{
			name: "workspace state dir non string",
			specBody: `
version: amata/v1
name: sample
entry: main
workspace:
  state_dir:
    nested: true
flows:
  main: {}
`,
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tempDir := t.TempDir()
			specPath := filepath.Join(tempDir, "workflow.yaml")
			if err := os.WriteFile(specPath, []byte(testCase.specBody), 0o644); err != nil {
				t.Fatalf("write spec: %v", err)
			}

			_, err := spec.Load(specPath)
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), "workspace") {
				t.Fatalf("error = %q, want workspace failure", err)
			}
		})
	}
}
