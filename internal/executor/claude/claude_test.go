package claude

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iw2rmb/amata/internal/executor/agent"
	"github.com/iw2rmb/amata/internal/testutil"
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
					if testCase.supported {
						return commandResult{
							stdout:              []byte(`{"type":"result","structured_output":{"approved":true}}`),
							structuredOutput:    map[string]any{"approved": true},
							hasStructuredOutput: true,
						}, nil
					}
					return commandResult{
						stdout: []byte("```json\n{\"approved\":true}\n```\n"),
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
			if !containsArg(captured.args, "--include-partial-messages") {
				t.Fatalf("args = %#v, want --include-partial-messages", captured.args)
			}
			if !containsArg(captured.args, "--verbose") {
				t.Fatalf("args = %#v, want --verbose", captured.args)
			}
			if !containsArgPair(captured.args, "--output-format", "stream-json") {
				t.Fatalf("args = %#v, want --output-format stream-json", captured.args)
			}
			if !containsArgPair(captured.args, "--effort", "medium") {
				t.Fatalf("args = %#v, want --effort medium", captured.args)
			}

			hasSchemaFlag := containsArgPair(captured.args, "--json-schema", `{"type":"object","properties":{"approved":{"type":"boolean"}},"required":["approved"]}`)
			if hasSchemaFlag != testCase.wantFlag {
				t.Fatalf("schema flag = %v, want %v (args=%#v)", hasSchemaFlag, testCase.wantFlag, captured.args)
			}
			if captured.stopOnStructuredOutput != testCase.supported {
				t.Fatalf("stopOnStructuredOutput = %v, want %v", captured.stopOnStructuredOutput, testCase.supported)
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

func TestProviderUsesStructuredOutputCapturedByRunner(t *testing.T) {
	t.Parallel()

	provider := provider{
		runner: fakeRunner(func(_ context.Context, spec command) (commandResult, error) {
			return commandResult{
				stdout:              []byte(`{"type":"result","stop_reason":"end_turn","session_id":"abc","usage":{"input_tokens":1},"structured_output":{"approved":true,"notes":"ok"}}`),
				sessionID:           "abc",
				structuredOutput:    map[string]any{"approved": true, "notes": "ok"},
				hasStructuredOutput: true,
			}, nil
		}),
		structuredOutputSupported: true,
	}

	response, execErr := provider.Execute(context.Background(), agent.Request{
		Prompt: "Review the diff",
		Model:  "sonnet",
		CWD:    "/repo",
		Structured: &agent.StructuredOutput{
			JSON: `{"type":"object","properties":{"approved":{"type":"boolean"},"notes":{"type":"string"}},"required":["approved","notes"]}`,
		},
	})
	if execErr != nil {
		t.Fatalf("execute error = %#v", execErr)
	}

	value, ok := response.Value.(map[string]any)
	if !ok {
		t.Fatalf("response value type = %T, want map[string]any", response.Value)
	}
	if value["approved"] != true || value["notes"] != "ok" {
		t.Fatalf("response value = %#v, want runner-captured structured output", response.Value)
	}
}

func TestProviderRetriesWithResumeBeforeFreshForMissingStructuredOutput(t *testing.T) {
	t.Parallel()

	var calls []command
	provider := provider{
		runner: fakeRunner(func(_ context.Context, spec command) (commandResult, error) {
			calls = append(calls, spec)
			switch len(calls) {
			case 1:
				return commandResult{
					stdout:    []byte(`{"type":"result","stop_reason":"end_turn","session_id":"sess-123","usage":{"input_tokens":1},"result":"not-structured"}`),
					sessionID: "sess-123",
				}, nil
			case 2:
				return commandResult{
					stdout:              []byte(`{"type":"result","stop_reason":"end_turn","session_id":"sess-123","usage":{"input_tokens":1},"structured_output":{"approved":true}}`),
					sessionID:           "sess-123",
					structuredOutput:    map[string]any{"approved": true},
					hasStructuredOutput: true,
				}, nil
			default:
				t.Fatalf("unexpected call #%d", len(calls))
				return commandResult{}, nil
			}
		}),
		structuredOutputSupported: true,
	}

	response, execErr := provider.Execute(context.Background(), agent.Request{
		Prompt: "Review the diff",
		Model:  "sonnet",
		CWD:    "/repo",
		Structured: &agent.StructuredOutput{
			JSON: `{"type":"object","properties":{"approved":{"type":"boolean"}},"required":["approved"]}`,
		},
	})
	if execErr != nil {
		t.Fatalf("execute error = %#v", execErr)
	}
	if len(calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(calls))
	}
	if !containsArgPair(calls[1].args, "--resume", "sess-123") {
		t.Fatalf("resume args = %#v, want --resume sess-123", calls[1].args)
	}
	if !strings.Contains(string(calls[1].stdin), providerSchemaRetryInstruction) {
		t.Fatalf("resume prompt = %q, want retry instruction", string(calls[1].stdin))
	}
	if attempts, ok := response.Metadata["structuredOutputAttempts"].(int); !ok || attempts != 2 {
		t.Fatalf("structuredOutputAttempts = %#v, want int(2)", response.Metadata["structuredOutputAttempts"])
	}
	value, ok := response.Value.(map[string]any)
	if !ok || value["approved"] != true {
		t.Fatalf("response value = %#v, want approved=true", response.Value)
	}
}

func TestProviderFallsBackToFreshAfterResumeWhenStructuredOutputStillMissing(t *testing.T) {
	t.Parallel()

	var calls []command
	provider := provider{
		runner: fakeRunner(func(_ context.Context, spec command) (commandResult, error) {
			calls = append(calls, spec)
			switch len(calls) {
			case 1:
				return commandResult{
					stdout:    []byte(`{"type":"result","stop_reason":"end_turn","session_id":"sess-456","usage":{"input_tokens":1},"result":"not-structured"}`),
					sessionID: "sess-456",
				}, nil
			case 2:
				return commandResult{
					stdout:    []byte(`{"type":"result","stop_reason":"end_turn","session_id":"sess-456","usage":{"input_tokens":1},"result":"still-not-structured"}`),
					sessionID: "sess-456",
				}, nil
			case 3:
				return commandResult{
					stdout:              []byte(`{"type":"result","stop_reason":"end_turn","session_id":"fresh-1","usage":{"input_tokens":1},"structured_output":{"approved":true}}`),
					sessionID:           "fresh-1",
					structuredOutput:    map[string]any{"approved": true},
					hasStructuredOutput: true,
				}, nil
			default:
				t.Fatalf("unexpected call #%d", len(calls))
				return commandResult{}, nil
			}
		}),
		structuredOutputSupported: true,
	}

	response, execErr := provider.Execute(context.Background(), agent.Request{
		Prompt: "Review the diff",
		Model:  "sonnet",
		CWD:    "/repo",
		Structured: &agent.StructuredOutput{
			JSON: `{"type":"object","properties":{"approved":{"type":"boolean"}},"required":["approved"]}`,
		},
	})
	if execErr != nil {
		t.Fatalf("execute error = %#v", execErr)
	}
	if len(calls) != 3 {
		t.Fatalf("calls = %d, want 3", len(calls))
	}
	if !containsArgPair(calls[1].args, "--resume", "sess-456") {
		t.Fatalf("resume args = %#v, want --resume sess-456", calls[1].args)
	}
	if containsArg(calls[2].args, "--resume") {
		t.Fatalf("fresh args = %#v, want no --resume", calls[2].args)
	}
	if attempts, ok := response.Metadata["structuredOutputAttempts"].(int); !ok || attempts != 3 {
		t.Fatalf("structuredOutputAttempts = %#v, want int(3)", response.Metadata["structuredOutputAttempts"])
	}
	value, ok := response.Value.(map[string]any)
	if !ok || value["approved"] != true {
		t.Fatalf("response value = %#v, want approved=true", response.Value)
	}
}

func TestProviderReportsAgentFailureBeforeStructuredParse(t *testing.T) {
	t.Parallel()

	provider := provider{
		runner: fakeRunner(func(_ context.Context, spec command) (commandResult, error) {
			return commandResult{
				stdout: []byte("not json"),
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

func TestProviderStreamsStdoutWhileRunning(t *testing.T) {
	t.Parallel()

	artifactDir := t.TempDir()
	stdoutPath := filepath.Join(artifactDir, "stdout.txt")
	stderrPath := filepath.Join(artifactDir, "stderr.txt")
	stdoutFile, err := os.Create(stdoutPath)
	if err != nil {
		t.Fatalf("create stdout file: %v", err)
	}
	defer stdoutFile.Close()
	stderrFile, err := os.Create(stderrPath)
	if err != nil {
		t.Fatalf("create stderr file: %v", err)
	}
	defer stderrFile.Close()

	firstChunkWritten := make(chan struct{})
	continueWriting := make(chan struct{})

	prov := provider{
		runner: fakeRunner(func(_ context.Context, spec command) (commandResult, error) {
			if _, err := spec.stdoutWriter.Write([]byte("chunk1\n")); err != nil {
				t.Errorf("write chunk1: %v", err)
			}
			if _, err := spec.stderrWriter.Write([]byte("err1\n")); err != nil {
				t.Errorf("write err1: %v", err)
			}
			close(firstChunkWritten)
			<-continueWriting
			if _, err := spec.stdoutWriter.Write([]byte("chunk2\n")); err != nil {
				t.Errorf("write chunk2: %v", err)
			}
			if _, err := spec.stderrWriter.Write([]byte("err2\n")); err != nil {
				t.Errorf("write err2: %v", err)
			}
			return commandResult{stdout: []byte("chunk1\nchunk2\n")}, nil
		}),
		structuredOutputSupported: true,
	}

	type execResult struct {
		response agent.Response
		err      *agent.Error
	}
	resultCh := make(chan execResult, 1)
	go func() {
		resp, execErr := prov.Execute(context.Background(), agent.Request{
			Prompt:       "test",
			Model:        "sonnet",
			CWD:          "/repo",
			StdoutWriter: stdoutFile,
			StderrWriter: stderrFile,
		})
		resultCh <- execResult{resp, execErr}
	}()

	<-firstChunkWritten
	data, err := os.ReadFile(stdoutPath)
	if err != nil {
		t.Fatalf("read stdout mid-run: %v", err)
	}
	if string(data) != "chunk1\n" {
		t.Fatalf("mid-run stdout = %q, want chunk1 only", string(data))
	}
	data, err = os.ReadFile(stderrPath)
	if err != nil {
		t.Fatalf("read stderr mid-run: %v", err)
	}
	if string(data) != "err1\n" {
		t.Fatalf("mid-run stderr = %q, want err1 only", string(data))
	}

	close(continueWriting)
	res := <-resultCh
	if res.err != nil {
		t.Fatalf("execute error = %#v", res.err)
	}

	data, err = os.ReadFile(stdoutPath)
	if err != nil {
		t.Fatalf("read stdout after run: %v", err)
	}
	if string(data) != "chunk1\nchunk2\n" {
		t.Fatalf("final stdout = %q, want chunk1+chunk2", string(data))
	}
	if string(res.response.Transcript) != "chunk1\nchunk2\n" {
		t.Fatalf("transcript = %q, want chunk1+chunk2", string(res.response.Transcript))
	}

	testutil.AssertFileContents(t, stderrPath, "err1\nerr2\n")
}

func TestProviderPreservesPartialOutputOnError(t *testing.T) {
	t.Parallel()

	artifactDir := t.TempDir()
	stdoutPath := filepath.Join(artifactDir, "stdout.txt")
	stderrPath := filepath.Join(artifactDir, "stderr.txt")
	stdoutFile, err := os.Create(stdoutPath)
	if err != nil {
		t.Fatalf("create stdout file: %v", err)
	}
	defer stdoutFile.Close()
	stderrFile, err := os.Create(stderrPath)
	if err != nil {
		t.Fatalf("create stderr file: %v", err)
	}
	defer stderrFile.Close()

	prov := provider{
		runner: fakeRunner(func(_ context.Context, spec command) (commandResult, error) {
			if _, err := spec.stdoutWriter.Write([]byte("partial output\n")); err != nil {
				t.Errorf("write stdout: %v", err)
			}
			if _, err := spec.stderrWriter.Write([]byte("error detail\n")); err != nil {
				t.Errorf("write stderr: %v", err)
			}
			return commandResult{stdout: []byte("partial output\n")}, errors.New("exit status 1")
		}),
		structuredOutputSupported: true,
	}

	_, execErr := prov.Execute(context.Background(), agent.Request{
		Prompt:       "test",
		Model:        "sonnet",
		CWD:          "/repo",
		StdoutWriter: stdoutFile,
		StderrWriter: stderrFile,
	})
	if execErr == nil {
		t.Fatalf("expected execute error")
	}
	if execErr.Code != "agent_failed" {
		t.Fatalf("error code = %q, want agent_failed", execErr.Code)
	}

	testutil.AssertFileContents(t, stdoutPath, "partial output\n")
	testutil.AssertFileContents(t, stderrPath, "error detail\n")
}

type fakeRunner func(context.Context, command) (commandResult, error)

func (f fakeRunner) Run(ctx context.Context, spec command) (commandResult, error) {
	return f(ctx, spec)
}

var containsArgPair = testutil.ContainsArgPair

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}
