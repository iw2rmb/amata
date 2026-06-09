package claude

import (
	"context"
	"encoding/json"
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

func TestProviderReportsMissingStructuredOutputWithoutRetry(t *testing.T) {
	t.Parallel()

	var calls []command
	provider := provider{
		runner: fakeRunner(func(_ context.Context, spec command) (commandResult, error) {
			calls = append(calls, spec)
			return commandResult{
				stdout:    []byte(`{"type":"result","stop_reason":"end_turn","session_id":"sess-123","usage":{"input_tokens":1},"result":"not-structured"}`),
				sessionID: "sess-123",
			}, nil
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
	if execErr == nil {
		t.Fatalf("expected execute error")
	}
	if execErr.Code != "invalid_provider_output" {
		t.Fatalf("error code = %q, want invalid_provider_output", execErr.Code)
	}
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	if response.Metadata["continuation_session_id"] != "sess-123" {
		t.Fatalf("continuation_session_id = %#v, want sess-123", response.Metadata["continuation_session_id"])
	}
}

func TestProviderUsesResumeSessionFromRequest(t *testing.T) {
	t.Parallel()

	var captured command
	provider := provider{
		runner: fakeRunner(func(_ context.Context, spec command) (commandResult, error) {
			captured = spec
			return commandResult{
				stdout:              []byte(`{"type":"result","stop_reason":"end_turn","session_id":"sess-next","usage":{"input_tokens":1},"structured_output":{"approved":true}}`),
				sessionID:           "sess-next",
				structuredOutput:    map[string]any{"approved": true},
				hasStructuredOutput: true,
			}, nil
		}),
		structuredOutputSupported: true,
	}

	response, execErr := provider.Execute(context.Background(), agent.Request{
		Prompt:                "Fix JSON",
		Model:                 "sonnet",
		CWD:                   "/repo",
		ContinuationSessionID: "sess-prev",
		Structured: &agent.StructuredOutput{
			JSON: `{"type":"object","properties":{"approved":{"type":"boolean"}},"required":["approved"]}`,
		},
	})
	if execErr != nil {
		t.Fatalf("execute error = %#v", execErr)
	}
	if !containsArgPair(captured.args, "--resume", "sess-prev") {
		t.Fatalf("args = %#v, want --resume sess-prev", captured.args)
	}
	if string(captured.stdin) != "Fix JSON" {
		t.Fatalf("stdin = %q, want continuation prompt", string(captured.stdin))
	}
	if response.Metadata["continuation_session_id"] != "sess-next" {
		t.Fatalf("continuation_session_id = %#v, want sess-next", response.Metadata["continuation_session_id"])
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

func TestProviderNormalizesProviderErrorIntoStderrAndDetails(t *testing.T) {
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

	rawLine := `{"type":"error","message":"{\"error\":{\"message\":\"The encrypted content could not be verified.\",\"type\":\"invalid_request_error\",\"param\":null,\"code\":\"invalid_encrypted_content\"}}"}`
	prov := provider{
		runner: fakeRunner(func(_ context.Context, spec command) (commandResult, error) {
			if _, err := spec.stdoutWriter.Write([]byte(rawLine + "\n")); err != nil {
				return commandResult{}, err
			}
			return commandResult{stdout: []byte(rawLine + "\n")}, errors.New("exit status 1")
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

	providerError, ok := execErr.Details["provider_error"].(map[string]any)
	if !ok {
		t.Fatalf("provider_error = %#v, want map", execErr.Details["provider_error"])
	}
	if got := providerError["message"]; got != "The encrypted content could not be verified." {
		t.Fatalf("provider_error.message = %#v", got)
	}
	if got := providerError["type"]; got != "invalid_request_error" {
		t.Fatalf("provider_error.type = %#v", got)
	}
	if got := providerError["code"]; got != "invalid_encrypted_content" {
		t.Fatalf("provider_error.code = %#v", got)
	}

	testutil.AssertFileContents(t, stdoutPath, rawLine+"\n")

	stderrData, readErr := os.ReadFile(stderrPath)
	if readErr != nil {
		t.Fatalf("read stderr: %v", readErr)
	}
	var event map[string]any
	if err := json.Unmarshal(stderrData, &event); err != nil {
		t.Fatalf("decode normalized stderr event: %v", err)
	}
	if got, _ := event["type"].(string); got != "error" {
		t.Fatalf("stderr type = %q, want error", got)
	}
	if _, hasMessage := event["message"]; hasMessage {
		t.Fatalf("stderr event should not include raw message field: %#v", event)
	}
	errorPayload, ok := event["error"].(map[string]any)
	if !ok {
		t.Fatalf("stderr error payload = %#v, want map", event["error"])
	}
	if got := errorPayload["code"]; got != "invalid_encrypted_content" {
		t.Fatalf("stderr error.code = %#v", got)
	}
}

func TestResolveRunOutcome(t *testing.T) {
	t.Parallel()

	errStdoutClosed := errors.New("read |0: file already closed")
	errStderrClosed := errors.New("read |0: file already closed")
	errWait := errors.New("signal: killed")

	testCases := []struct {
		name       string
		setup      func(t *testing.T) (context.Context, context.Context)
		spec       command
		result     commandResult
		stdoutErr  error
		stderrErr  error
		waitErr    error
		wantErr    error
		wantResult commandResult
	}{
		{
			name: "internal cancel with structured output ignores pipe errors",
			setup: func(t *testing.T) (context.Context, context.Context) {
				t.Helper()

				ctx := context.Background()
				runCtx, cancel := context.WithCancel(ctx)
				cancel()
				return ctx, runCtx
			},
			spec: command{
				stopOnStructuredOutput: true,
			},
			result: commandResult{
				hasStructuredOutput: true,
				structuredOutput:    map[string]any{"approved": true},
			},
			stdoutErr: errStdoutClosed,
			stderrErr: errStderrClosed,
			waitErr:   errWait,
			wantErr:   nil,
			wantResult: commandResult{
				hasStructuredOutput: true,
				structuredOutput:    map[string]any{"approved": true},
			},
		},
		{
			name: "caller cancel does not convert pipe error to success",
			setup: func(t *testing.T) (context.Context, context.Context) {
				t.Helper()

				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				runCtx, runCancel := context.WithCancel(ctx)
				runCancel()
				return ctx, runCtx
			},
			spec: command{
				stopOnStructuredOutput: true,
			},
			result: commandResult{
				hasStructuredOutput: true,
			},
			stdoutErr: errStdoutClosed,
			wantErr:   errStdoutClosed,
		},
		{
			name: "missing structured output keeps stdout error path",
			setup: func(t *testing.T) (context.Context, context.Context) {
				t.Helper()

				ctx := context.Background()
				runCtx, cancel := context.WithCancel(ctx)
				cancel()
				return ctx, runCtx
			},
			spec: command{
				stopOnStructuredOutput: true,
			},
			result: commandResult{
				hasStructuredOutput: false,
			},
			stdoutErr: errStdoutClosed,
			wantErr:   errStdoutClosed,
		},
		{
			name: "non-cancel path preserves stderr over wait precedence",
			setup: func(t *testing.T) (context.Context, context.Context) {
				t.Helper()

				ctx := context.Background()
				runCtx := context.Background()
				return ctx, runCtx
			},
			spec: command{
				stopOnStructuredOutput: false,
			},
			result: commandResult{
				stdout: []byte("ok"),
			},
			stderrErr: errStderrClosed,
			waitErr:   errWait,
			wantErr:   errStderrClosed,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, runCtx := tc.setup(t)
			gotResult, gotErr := resolveRunOutcome(ctx, runCtx, tc.spec, tc.result, tc.stdoutErr, tc.stderrErr, tc.waitErr)
			if !errors.Is(gotErr, tc.wantErr) {
				t.Fatalf("error = %v, want %v", gotErr, tc.wantErr)
			}
			if gotResult.hasStructuredOutput != tc.wantResult.hasStructuredOutput {
				t.Fatalf("hasStructuredOutput = %v, want %v", gotResult.hasStructuredOutput, tc.wantResult.hasStructuredOutput)
			}
			if tc.wantResult.structuredOutput != nil && gotResult.structuredOutput == nil {
				t.Fatalf("structuredOutput = nil, want non-nil")
			}
		})
	}
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
