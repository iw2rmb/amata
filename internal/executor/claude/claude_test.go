package claude

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/iw2rmb/amata/internal/executor/agent"
)

func TestProviderStructuredOutputModes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name               string
		supported          bool
		wantFlag           bool
		wantPromptFragment string
	}{
		{
			name:      "provider schema flag",
			supported: true,
			wantFlag:  true,
		},
		{
			name:               "prompt fallback",
			supported:          false,
			wantFlag:           false,
			wantPromptFragment: "Return only JSON that matches this schema.",
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var captured command
			provider := provider{
				runner: fakeRunner(func(_ context.Context, spec command) (commandResult, error) {
					captured = spec
					return commandResult{
						stdout: []byte("```json\n{\"approved\":true}\n```\n"),
						stderr: []byte(""),
					}, nil
				}),
				structuredOutputSupported: testCase.supported,
			}

			response, execErr := provider.Execute(context.Background(), agent.Request{
				Prompt:    "Review the diff",
				Model:     "sonnet",
				Reasoning: "medium",
				CWD:       "/repo",
				Structured: &agent.StructuredOutput{
					JSON: `{"type":"object","properties":{"approved":{"type":"boolean"}},"required":["approved"]}`,
				},
			})
			if execErr != nil {
				t.Fatalf("execute error = %#v", execErr)
			}

			if captured.dir != "/repo" {
				t.Fatalf("dir = %q, want /repo", captured.dir)
			}
			if !containsArgPair(captured.args, "--model", "sonnet") {
				t.Fatalf("args = %#v, want --model sonnet", captured.args)
			}
			if !containsArgPair(captured.args, "--effort", "medium") {
				t.Fatalf("args = %#v, want --effort medium", captured.args)
			}

			hasJSONOutputFormat := containsArgPair(captured.args, "--output-format", "json")
			if hasJSONOutputFormat != testCase.wantFlag {
				t.Fatalf("json output format = %v, want %v (args=%#v)", hasJSONOutputFormat, testCase.wantFlag, captured.args)
			}
			if !testCase.wantFlag && !containsArgPair(captured.args, "--output-format", "text") {
				t.Fatalf("args = %#v, want --output-format text in fallback mode", captured.args)
			}

			hasSchemaFlag := containsArgPair(captured.args, "--json-schema", `{"type":"object","properties":{"approved":{"type":"boolean"}},"required":["approved"]}`)
			if hasSchemaFlag != testCase.wantFlag {
				t.Fatalf("schema flag = %v, want %v (args=%#v)", hasSchemaFlag, testCase.wantFlag, captured.args)
			}

			prompt := string(captured.stdin)
			if testCase.wantPromptFragment != "" && !strings.Contains(prompt, testCase.wantPromptFragment) {
				t.Fatalf("prompt = %q, want fragment %q", prompt, testCase.wantPromptFragment)
			}
			if testCase.wantPromptFragment == "" && prompt != "Review the diff" {
				t.Fatalf("prompt = %q, want original prompt", prompt)
			}
			if response.Prompt != prompt {
				t.Fatalf("response prompt = %q, want %q", response.Prompt, prompt)
			}

			value, ok := response.Value.(map[string]any)
			if !ok || value["approved"] != true {
				t.Fatalf("response value = %#v, want parsed structured JSON", response.Value)
			}
		})
	}
}

func TestProviderReportsAgentFailureBeforeStructuredParse(t *testing.T) {
	t.Parallel()

	provider := provider{
		runner: fakeRunner(func(_ context.Context, spec command) (commandResult, error) {
			return commandResult{
				stdout: []byte("not json"),
				stderr: []byte("failure"),
			}, errors.New("exit status 1")
		}),
		structuredOutputSupported: true,
	}

	_, execErr := provider.Execute(context.Background(), agent.Request{
		Prompt: "Review the diff",
		Model:  "sonnet",
		CWD:    "/repo",
		Structured: &agent.StructuredOutput{
			JSON: `{"type":"object","properties":{"approved":{"type":"boolean"}},"required":["approved"]}`,
		},
	})
	if execErr == nil {
		t.Fatalf("expected execute error")
	}
	if execErr.Code != "agent_failed" {
		t.Fatalf("error code = %q, want agent_failed", execErr.Code)
	}
}

type fakeRunner func(context.Context, command) (commandResult, error)

func (f fakeRunner) Run(ctx context.Context, spec command) (commandResult, error) {
	return f(ctx, spec)
}

func containsArgPair(args []string, name string, value string) bool {
	for index := 0; index+1 < len(args); index++ {
		if args[index] == name && args[index+1] == value {
			return true
		}
	}
	return false
}
