package spec

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const roleSpec = `version: amata/v1
name: role-test
entry: main
defaults:
  agents:
    roles:
      coder:
        - codex:
            - model: gpt-5.3-codex
            - model: gpt-6-astra
        - nessy
        - claude:
            model: opus-5
        - nessy: glm53
flows:
  main:
    steps:
      - coder: fix it
`

// Every shorthand form preserves candidate order through persisted YAML.
func TestAgentRoleFormsPreserveOrder(t *testing.T) {
	doc, err := Decode([]byte(roleSpec))
	if err != nil {
		t.Fatal(err)
	}
	step := doc.Flows["main"].Steps[0]
	if step.Type != "coder" || step.Fields["prompt"] != "fix it" {
		t.Fatalf("step = %#v", step)
	}
	roles, err := ParseAgentRoles(doc.Defaults)
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := roles.Candidates(step)
	if err != nil {
		t.Fatal(err)
	}
	wantTypes := []string{"codex", "codex", "nessy", "claude", "nessy"}
	wantModels := []any{"gpt-5.3-codex", "gpt-6-astra", nil, "opus-5", "glm53"}
	if len(candidates) != len(wantTypes) {
		t.Fatalf("candidates = %#v", candidates)
	}
	for i, candidate := range candidates {
		if candidate.Type != wantTypes[i] || candidate.Fields["model"] != wantModels[i] || candidate.Fields["prompt"] != "fix it" {
			t.Fatalf("candidate %d = %#v", i, candidate)
		}
	}
	data, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var restored Document
	if err := yaml.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.Flows["main"].Steps[0].Type != "coder" {
		t.Fatalf("persisted role lost: %s", data)
	}
}

// Nested roles are normalized before ordinary switch and loop validation.
func TestAgentRolesInNestedSteps(t *testing.T) {
	body := strings.Replace(roleSpec, "      - coder: fix it", `      - for_each: [one]
        steps:
          - switch:
              - when: true
                steps:
                  - coder: fix it
                    reasoning: high`, 1)
	doc, err := Decode([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	nested, err := decodeNestedSteps(doc.Flows["main"].Steps[0].Fields["steps"])
	if err != nil {
		t.Fatal(err)
	}
	branches, err := decodeSwitchBranches(nested[0])
	if err != nil {
		t.Fatal(err)
	}
	if branches[0][0].Type != "coder" {
		t.Fatalf("nested role = %#v", branches)
	}
}

// Invalid roles and candidate settings fail validation before execution.
func TestAgentRoleValidation(t *testing.T) {
	cases := []struct{ name, old, replacement, want string }{
		{"empty chain", "        - codex:\n            - model: gpt-5.3-codex\n            - model: gpt-6-astra\n        - nessy\n        - claude:\n            model: opus-5\n        - nessy: glm53", "        []", "non-empty array"},
		{"reserved name", "coder:", "shell:", "conflicts"},
		{"unknown agent", "- nessy: glm53", "- unknown: glm53", "unknown agent"},
		{"invalid supported field", "model: opus-5", "model: 42", "model"},
		{"ambiguous shorthand", "- coder: fix it", "- coder: fix it\n        prompt: another", "ambiguous"},
		{"unknown role", "- coder: fix it", "- unknown: fix it", "does not declare"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Decode([]byte(strings.Replace(roleSpec, tc.old, tc.replacement, 1)))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

// A shared field reaches only candidates whose executor schema accepts it.
func TestAgentRoleIgnoresUnsupportedFieldsPerCandidate(t *testing.T) {
	body := strings.Replace(roleSpec, "        - nessy\n", "        - nessy:\n            reasoning: high\n            unknown_setting: candidate-value\n", 1)
	body = strings.Replace(body, "      - coder: fix it", "      - coder: fix it\n        reasoning: high\n        unknown_setting: step-value", 1)
	doc, err := Decode([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	roles, err := ParseAgentRoles(doc.Defaults)
	if err != nil {
		t.Fatal(err)
	}
	step := doc.Flows["main"].Steps[0]
	candidates, err := roles.Candidates(step)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range candidates {
		if candidate.Fields["prompt"] != "fix it" {
			t.Fatalf("lost prompt: %#v", candidate)
		}
		if _, ok := candidate.Fields["unknown_setting"]; ok {
			t.Fatalf("unknown field reached %s", candidate.Type)
		}
		reasoning, hasReasoning := candidate.Fields["reasoning"]
		if candidate.Type == "nessy" {
			if hasReasoning {
				t.Fatalf("reasoning reached nessy: %#v", candidate)
			}
		} else if reasoning != "high" {
			t.Fatalf("reasoning lost for %s", candidate.Type)
		}
	}
	if step.Fields["reasoning"] != "high" || step.Fields["unknown_setting"] != "step-value" {
		t.Fatal("role invocation was mutated")
	}
}

// Direct executors remain strict even when the document also declares roles.
func TestDirectExecutorStillRejectsUnsupportedFields(t *testing.T) {
	body := strings.Replace(roleSpec, "      - coder: fix it", "      - nessy: fix it\n        reasoning: high", 1)
	if _, err := Decode([]byte(body)); err == nil || !strings.Contains(err.Error(), "reasoning") {
		t.Fatalf("error = %v, want unsupported reasoning", err)
	}
}
