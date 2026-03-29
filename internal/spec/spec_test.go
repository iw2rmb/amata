package spec_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iw2rmb/amata/internal/spec"
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
        body:
          expr: '"commit body"'
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
      - crush: do the thing
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
      - for_each: [one, two]
        as: item
        steps:
          - expr: $.item
      - git.commit: "feat: done"
        body: "details"
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

	if got := mainSteps[4].Type; got != "crush" {
		t.Fatalf("step 4 type = %q, want crush", got)
	}
	if got := mainSteps[4].Fields["prompt"]; got != "do the thing" {
		t.Fatalf("step 4 prompt = %#v, want \"do the thing\"", got)
	}

	if got := mainSteps[5].Type; got != "switch" {
		t.Fatalf("step 5 type = %q, want switch", got)
	}
	cases, ok := mainSteps[5].Fields["cases"].([]any)
	if !ok || len(cases) != 2 {
		t.Fatalf("step 5 cases = %#v, want 2 switch cases", mainSteps[5].Fields["cases"])
	}
	firstCase, ok := cases[0].(map[string]any)
	if !ok {
		t.Fatalf("step 5 case 0 = %#v, want map", cases[0])
	}
	firstWhen, ok := firstCase["when"].(map[string]any)
	if !ok {
		t.Fatalf("step 5 case 0 when = %#v, want expr map", firstCase["when"])
	}
	if got := firstWhen["expr"]; got != `$.prev.value["hasItem"]` {
		t.Fatalf("step 5 case 0 when.expr = %#v, want hasItem expression", got)
	}
	secondCase, ok := cases[1].(map[string]any)
	if !ok {
		t.Fatalf("step 5 case 1 = %#v, want map", cases[1])
	}
	secondWhen, ok := secondCase["when"].(map[string]any)
	if !ok {
		t.Fatalf("step 5 case 1 when = %#v, want expr map", secondCase["when"])
	}
	if got := secondWhen["expr"]; got != `not ctx.prev.value["hasItem"]` {
		t.Fatalf("step 5 case 1 when.expr = %#v, want negated hasItem expression", got)
	}

	if got := mainSteps[7].Type; got != "for_each" {
		t.Fatalf("step 7 type = %q, want for_each", got)
	}
	if got := mainSteps[7].Fields["as"]; got != "item" {
		t.Fatalf("step 7 as = %#v, want item", got)
	}
	if items, ok := mainSteps[7].Fields["items"].([]any); !ok || len(items) != 2 {
		t.Fatalf("step 7 items = %#v, want 2 items", mainSteps[7].Fields["items"])
	}

	if got := mainSteps[8].Type; got != "git.commit" {
		t.Fatalf("step 8 type = %q, want git.commit", got)
	}
	if got := mainSteps[8].Fields["message"]; got != "feat: done" {
		t.Fatalf("step 8 message = %#v, want feat: done", got)
	}
	if got := mainSteps[8].Fields["body"]; got != "details" {
		t.Fatalf("step 8 body = %#v, want details", got)
	}
}

func TestLoadResolvesIncludeTags(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	specPath := filepath.Join(tempDir, "workflow.yaml")
	flowsDir := filepath.Join(tempDir, "flows")
	if err := os.MkdirAll(flowsDir, 0o755); err != nil {
		t.Fatalf("mkdir flows: %v", err)
	}

	root := `
version: amata/v1
name: sample
entry: main
flows:
  post:
    steps:
      - expr: '"post"'
  main: !include ./flows/implementation-loop.yaml#/flows/main
  review: !include ./flows/item-review-loop.yaml#/flows/review
  helper: !include ./flows/common.yaml#/flows/helper
`
	implFlow := `
flows:
  main:
    steps:
      - call: review
      - call: post
`
	reviewFlow := `
flows:
  review:
    steps:
      - call: helper
`
	commonFlow := `
flows:
  helper:
    steps:
      - expr: '"helper"'
`

	if err := os.WriteFile(specPath, []byte(root), 0o644); err != nil {
		t.Fatalf("write root spec: %v", err)
	}
	if err := os.WriteFile(filepath.Join(flowsDir, "implementation-loop.yaml"), []byte(implFlow), 0o644); err != nil {
		t.Fatalf("write implementation flow: %v", err)
	}
	if err := os.WriteFile(filepath.Join(flowsDir, "item-review-loop.yaml"), []byte(reviewFlow), 0o644); err != nil {
		t.Fatalf("write review flow: %v", err)
	}
	if err := os.WriteFile(filepath.Join(flowsDir, "common.yaml"), []byte(commonFlow), 0o644); err != nil {
		t.Fatalf("write common flow: %v", err)
	}

	loaded, err := spec.Load(specPath)
	if err != nil {
		t.Fatalf("load spec: %v", err)
	}

	if _, ok := loaded.Spec.Flows["main"]; !ok {
		t.Fatalf("flows[main] missing")
	}
	if _, ok := loaded.Spec.Flows["review"]; !ok {
		t.Fatalf("flows[review] missing")
	}
	if _, ok := loaded.Spec.Flows["helper"]; !ok {
		t.Fatalf("flows[helper] missing")
	}
	if _, ok := loaded.Spec.Flows["post"]; !ok {
		t.Fatalf("flows[post] missing")
	}
}

func TestLoadRejectsIncludeErrors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name            string
		files           map[string]string
		wantErrContains string
	}{
		{
			name: "include cycles",
			files: map[string]string{
				"workflow.yaml": `
version: amata/v1
name: sample
entry: main
flows:
  main: !include ./a.yaml#/flows/from_a
`,
				"a.yaml": `
flows:
  from_a: !include ./b.yaml#/flows/from_b
`,
				"b.yaml": `
flows:
  from_b: !include ./a.yaml#/flows/from_a
`,
			},
			wantErrContains: "spec include cycle detected",
		},
		{
			name: "invalid include fragment",
			files: map[string]string{
				"workflow.yaml": `
version: amata/v1
name: sample
entry: main
flows:
  main: !include ./flows/main.yaml#flows/main
`,
				"flows/main.yaml": `
flows:
  main:
    steps:
      - expr: '"ok"'
`,
			},
			wantErrContains: "!include fragment must start with /",
		},
		{
			name: "duplicate flow keys after includes",
			files: map[string]string{
				"workflow.yaml": `
version: amata/v1
name: sample
entry: main
flows:
  duplicate: !include ./flows/a.yaml#/flows/duplicate
  duplicate: !include ./flows/b.yaml#/flows/duplicate
  main:
    steps:
      - call: duplicate
`,
				"flows/a.yaml": `
flows:
  duplicate:
    steps:
      - expr: '"a"'
`,
				"flows/b.yaml": `
flows:
  duplicate:
    steps:
      - expr: '"b"'
`,
			},
			wantErrContains: "mapping key",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tempDir := t.TempDir()
			for name, content := range tc.files {
				path := filepath.Join(tempDir, name)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatalf("mkdir for %s: %v", name, err)
				}
				if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
					t.Fatalf("write %s: %v", name, err)
				}
			}

			_, err := spec.Load(filepath.Join(tempDir, "workflow.yaml"))
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tc.wantErrContains) {
				t.Fatalf("error = %q, want %q", err, tc.wantErrContains)
			}
		})
	}
}
