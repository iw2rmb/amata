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

func TestLoadRejectsInvalidBuiltInStepSchemas(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		steps        string
		wantFragment string
	}{
		{
			name: "unknown step type",
			steps: `
      - type: mystery
`,
			wantFragment: `unknown step type "mystery"`,
		},
		{
			name: "shell extra field",
			steps: `
      - command: echo hi
        bogus: true
`,
			wantFragment: "bogus",
		},
		{
			name: "shell shorthand rejects object",
			steps: `
      - shell:
          expr: '"echo hi"'
`,
			wantFragment: "step does not declare an executor",
		},
		{
			name: "git commit missing message",
			steps: `
      - type: git.commit
`,
			wantFragment: "message",
		},
		{
			name: "nested switch step invalid",
			steps: `
      - type: switch
        cases:
          - steps:
              - type: call
                nope: true
`,
			wantFragment: "nope",
		},
		{
			name: "for each body empty",
			steps: `
      - type: for_each
        items: []
        steps: []
`,
			wantFragment: "steps",
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tempDir := t.TempDir()
			specPath := filepath.Join(tempDir, "workflow.yaml")
			specBody := `
version: amata/v1
name: sample
entry: main
flows:
  main:
    steps:
` + testCase.steps
			if err := os.WriteFile(specPath, []byte(specBody), 0o644); err != nil {
				t.Fatalf("write spec: %v", err)
			}

			_, err := spec.Load(specPath)
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), `flow "main" step 0`) {
				t.Fatalf("error = %q, want flow/step context", err)
			}
			if !strings.Contains(err.Error(), testCase.wantFragment) {
				t.Fatalf("error = %q, want fragment %q", err, testCase.wantFragment)
			}
		})
	}
}

func TestLoadAcceptsEmbeddedBuiltInStepSchemas(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	specPath := filepath.Join(tempDir, "workflow.yaml")
	specBody := `
version: amata/v1
name: sample
entry: main
flows:
  main:
    steps:
      - id: shell-short
        command:
          - sh
          - -lc
          - echo hi
        files:
          rendered:
            expr: '"artifact.txt"'
      - type: switch
        cases:
          - when: true
            steps:
              - type: call
                flow: next
      - type: for_each
        items:
          - one
        steps:
          - type: git.inspect
            cwd:
              expr: "ctx.workspace.root"
  next:
    steps:
      - type: git.commit
        message:
          expr: '"commit message"'
`

	if err := os.WriteFile(specPath, []byte(specBody), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	loaded, err := spec.Load(specPath)
	if err != nil {
		t.Fatalf("load spec: %v", err)
	}

	if got := len(loaded.Spec.Flows["main"].Steps); got != 3 {
		t.Fatalf("main step count = %d, want 3", got)
	}
}

func TestLoadAcceptsStepAndResponseShorthand(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	schemaPath := filepath.Join(tempDir, "commit.schema.json")
	specPath := filepath.Join(tempDir, "workflow.yaml")

	if err := os.WriteFile(schemaPath, []byte(`{"type":"object","properties":{"ok":{"type":"boolean"}},"required":["ok"],"additionalProperties":false}`), 0o644); err != nil {
		t.Fatalf("write schema: %v", err)
	}

	specBody := `
version: amata/v1
name: sample
entry: main
flows:
  main:
    steps:
      - call: next
      - shell: echo hi
        response: ./commit.schema.json
      - codex: |
          Output ONLY valid JSON.
        response:
          type: object
          properties:
            approved: boolean
          required: [approved]
          additionalProperties: false
      - claude:
          expr: '"prompt"'
      - switch:
          - when: $.prev.value["hasItem"]
            steps:
              - call: next
          - default: not ctx.prev.value["hasItem"]
            steps:
              - expr: $.prev.value
      - type: for_each
        items: [one]
        steps:
          - shell:
              - sh
              - -lc
              - echo hi
  next:
    steps:
      - type: expr
        expr: '"done"'
`

	if err := os.WriteFile(specPath, []byte(specBody), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	loaded, err := spec.Load(specPath)
	if err != nil {
		t.Fatalf("load spec: %v", err)
	}

	mainSteps := loaded.Spec.Flows["main"].Steps
	if got := mainSteps[0].Type; got != "call" {
		t.Fatalf("step 0 type = %q, want call", got)
	}
	if got := mainSteps[0].Fields["flow"]; got != "next" {
		t.Fatalf("step 0 flow = %#v, want next", got)
	}

	if got := mainSteps[1].Type; got != "shell" {
		t.Fatalf("step 1 type = %q, want shell", got)
	}
	responseFields, ok := mainSteps[1].Fields["response"].(map[string]any)
	if !ok {
		t.Fatalf("step 1 response = %#v, want map", mainSteps[1].Fields["response"])
	}
	if got := responseFields["schema"]; got != "./commit.schema.json" {
		t.Fatalf("step 1 response.schema = %#v, want ./commit.schema.json", got)
	}

	if got := mainSteps[2].Type; got != "codex" {
		t.Fatalf("step 2 type = %q, want codex", got)
	}
	if _, ok := mainSteps[2].Fields["prompt"].(string); !ok {
		t.Fatalf("step 2 prompt = %#v, want string", mainSteps[2].Fields["prompt"])
	}
	responseFields, ok = mainSteps[2].Fields["response"].(map[string]any)
	if !ok {
		t.Fatalf("step 2 response = %#v, want map", mainSteps[2].Fields["response"])
	}
	schemaFields, ok := responseFields["schema"].(map[string]any)
	if !ok {
		t.Fatalf("step 2 response.schema = %#v, want schema map", responseFields["schema"])
	}
	if got := schemaFields["type"]; got != "object" {
		t.Fatalf("step 2 response.schema.type = %#v, want object", got)
	}

	if got := mainSteps[3].Type; got != "claude" {
		t.Fatalf("step 3 type = %q, want claude", got)
	}
	promptFields, ok := mainSteps[3].Fields["prompt"].(map[string]any)
	if !ok {
		t.Fatalf("step 3 prompt = %#v, want expr map", mainSteps[3].Fields["prompt"])
	}
	if got := promptFields["expr"]; got != `"prompt"` {
		t.Fatalf("step 3 prompt.expr = %#v, want %q", got, `"prompt"`)
	}

	if got := mainSteps[4].Type; got != "switch" {
		t.Fatalf("step 4 type = %q, want switch", got)
	}
	cases, ok := mainSteps[4].Fields["cases"].([]any)
	if !ok || len(cases) != 2 {
		t.Fatalf("step 4 cases = %#v, want 2 switch cases", mainSteps[4].Fields["cases"])
	}
	firstCase, ok := cases[0].(map[string]any)
	if !ok {
		t.Fatalf("step 4 case 0 = %#v, want map", cases[0])
	}
	firstWhen, ok := firstCase["when"].(map[string]any)
	if !ok {
		t.Fatalf("step 4 case 0 when = %#v, want expr map", firstCase["when"])
	}
	if got := firstWhen["expr"]; got != `$.prev.value["hasItem"]` {
		t.Fatalf("step 4 case 0 when.expr = %#v, want hasItem expression", got)
	}
	secondCase, ok := cases[1].(map[string]any)
	if !ok {
		t.Fatalf("step 4 case 1 = %#v, want map", cases[1])
	}
	secondWhen, ok := secondCase["when"].(map[string]any)
	if !ok {
		t.Fatalf("step 4 case 1 when = %#v, want expr map", secondCase["when"])
	}
	if got := secondWhen["expr"]; got != `not ctx.prev.value["hasItem"]` {
		t.Fatalf("step 4 case 1 when.expr = %#v, want negated hasItem expression", got)
	}
}
