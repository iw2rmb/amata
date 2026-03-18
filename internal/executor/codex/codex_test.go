package codex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iw2rmb/amata/internal/executor/agent"
)

func TestProviderPassesSettingsAndParsesStructuredOutput(t *testing.T) {
	t.Parallel()

	artifactDir := t.TempDir()
	var captured command

	provider := provider{
		runner: fakeRunner(func(_ context.Context, spec command) (commandResult, error) {
			captured = spec
			if err := os.WriteFile(filepath.Join(artifactDir, "last-message.txt"), []byte("{\"approved\":true}\n"), 0o644); err != nil {
				t.Fatalf("write last message: %v", err)
			}
			return commandResult{
				stdout: []byte("progress\n"),
				stderr: []byte("warnings\n"),
			}, nil
		}),
	}

	response, execErr := provider.Execute(context.Background(), agent.Request{
		Prompt:      "Implement item",
		Model:       "gpt-5.4",
		Reasoning:   "high",
		CWD:         "/repo",
		Env:         map[string]string{"CODEX_TEST": "1"},
		ArtifactDir: artifactDir,
		Structured: &agent.StructuredOutput{
			SchemaPath: filepath.Join(artifactDir, "response-schema.json"),
		},
	})
	if execErr != nil {
		t.Fatalf("execute error = %#v", execErr)
	}

	if captured.dir != "/repo" {
		t.Fatalf("dir = %q, want /repo", captured.dir)
	}
	if string(captured.stdin) != "Implement item" {
		t.Fatalf("stdin = %q, want prompt", string(captured.stdin))
	}
	if !containsArgPair(captured.args, "--model", "gpt-5.4") {
		t.Fatalf("args = %#v, want --model gpt-5.4", captured.args)
	}
	if !containsArgPair(captured.args, "--output-schema", filepath.Join(artifactDir, "response-schema.json")) {
		t.Fatalf("args = %#v, want structured schema flag", captured.args)
	}
	if !containsArgPair(captured.args, "-o", filepath.Join(artifactDir, "last-message.txt")) {
		t.Fatalf("args = %#v, want last message output flag", captured.args)
	}
	if !containsString(captured.args, `model_reasoning_effort="high"`) {
		t.Fatalf("args = %#v, want reasoning setting", captured.args)
	}
	if !containsEnv(captured.env, "CODEX_TEST=1") {
		t.Fatalf("env = %#v, want CODEX_TEST override", captured.env)
	}

	value, ok := response.Value.(map[string]any)
	if !ok || value["approved"] != true {
		t.Fatalf("response value = %#v, want parsed structured JSON", response.Value)
	}
	if string(response.Transcript) != "{\"approved\":true}\n" {
		t.Fatalf("transcript = %q, want last message content", string(response.Transcript))
	}
	if response.Prompt != "Implement item" {
		t.Fatalf("prompt = %q, want original prompt", response.Prompt)
	}
}

func TestProviderReportsAgentFailureBeforeTranscriptValidation(t *testing.T) {
	t.Parallel()

	artifactDir := t.TempDir()
	provider := provider{
		runner: fakeRunner(func(_ context.Context, spec command) (commandResult, error) {
			return commandResult{
				stdout: []byte("progress\n"),
				stderr: []byte("failure\n"),
			}, errors.New("exit status 1")
		}),
	}

	_, execErr := provider.Execute(context.Background(), agent.Request{
		Prompt:      "Implement item",
		Model:       "gpt-5.4",
		CWD:         "/repo",
		ArtifactDir: artifactDir,
		Structured: &agent.StructuredOutput{
			SchemaPath: filepath.Join(artifactDir, "response-schema.json"),
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

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsEnv(values []string, want string) bool {
	for _, value := range values {
		if strings.HasPrefix(value, want) {
			return true
		}
	}
	return false
}
