package crush

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/iw2rmb/amata/internal/executor/agent"
	"github.com/iw2rmb/amata/internal/testutil"
)

// TestProvider is the canonical matrix covering args/env/cwd/stdin,
// streaming capture, structured-output parse, and failure paths.
func TestProvider(t *testing.T) {
	t.Parallel()

	trueVal := true

	tests := []struct {
		name      string
		request   agent.Request
		runOutput string
		runErr    error
		// expectations
		wantErrCode        string
		wantFlags          []string
		wantModelArg       string
		wantCWD            string
		wantEnvContains    string
		wantStdinContains  string
		wantTranscript     string
		wantStructuredMode string
		wantValueApproved  *bool
		wantStdoutFile     bool // verify StdoutWriter receives output
	}{
		{
			name: "passes args env cwd stdin",
			request: agent.Request{
				Prompt: "Implement item",
				Model:  "sonnet-5",
				CWD:    "/repo",
				Env:    map[string]string{"CRUSH_TEST": "1"},
			},
			runOutput:         "result output\n",
			wantFlags:         []string{"--yolo", "--quiet"},
			wantModelArg:      "sonnet-5",
			wantCWD:           "/repo",
			wantEnvContains:   "CRUSH_TEST=1",
			wantStdinContains: "Implement item",
			wantTranscript:    "result output\n",
		},
		{
			name: "structured output uses prompt fallback and parses json",
			request: agent.Request{
				Prompt: "Review the diff",
				Model:  "sonnet-5",
				CWD:    "/repo",
				Structured: &agent.StructuredOutput{
					JSON: `{"type":"object","properties":{"approved":{"type":"boolean"}},"required":["approved"]}`,
				},
			},
			runOutput:          "```json\n{\"approved\":true}\n```\n",
			wantStdinContains:  "Return only JSON that matches this schema.",
			wantStructuredMode: "prompt_fallback",
			wantValueApproved:  &trueVal,
		},
		{
			name: "streaming stdout writer receives output while running",
			request: agent.Request{
				Prompt: "Stream test",
				Model:  "sonnet-5",
				CWD:    "/repo",
			},
			runOutput:      "streamed output\n",
			wantTranscript: "streamed output\n",
			wantStdoutFile: true,
		},
		{
			name: "reasoning rejected with unsupported option",
			request: agent.Request{
				Prompt:    "Implement item",
				Model:     "sonnet-5",
				Reasoning: "high",
				CWD:       "/repo",
			},
			wantErrCode: "unsupported_option",
		},
		{
			name: "run failure surfaces agent_failed",
			request: agent.Request{
				Prompt: "Implement item",
				Model:  "sonnet-5",
				CWD:    "/repo",
			},
			runErr:      errors.New("exit status 1"),
			wantErrCode: "agent_failed",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var capturedArgs []string
			var capturedDir string
			var capturedEnv []string
			var capturedStdin []byte

			var stdoutFile *os.File
			if tc.wantStdoutFile {
				f, err := os.CreateTemp(t.TempDir(), "stdout-*")
				if err != nil {
					t.Fatalf("create temp stdout file: %v", err)
				}
				defer f.Close()
				stdoutFile = f
				tc.request.StdoutWriter = f
			}

			prov := provider{
				run: func(_ context.Context, args []string, dir string, env []string, stdin []byte, stdout, _ io.Writer) error {
					capturedArgs = args
					capturedDir = dir
					capturedEnv = env
					capturedStdin = stdin
					if tc.runOutput != "" {
						if _, err := stdout.Write([]byte(tc.runOutput)); err != nil {
							t.Errorf("write run output: %v", err)
						}
					}
					return tc.runErr
				},
			}

			// Skip runner call check for reasoning rejection.
			if tc.request.Reasoning != "" {
				prov.run = func(_ context.Context, _ []string, _ string, _ []string, _ []byte, _, _ io.Writer) error {
					t.Fatal("runner must not be called when reasoning is rejected")
					return nil
				}
			}

			response, execErr := prov.Execute(context.Background(), tc.request)

			if tc.wantErrCode != "" {
				if execErr == nil {
					t.Fatalf("expected error code %q, got nil", tc.wantErrCode)
				}
				if execErr.Code != tc.wantErrCode {
					t.Fatalf("error code = %q, want %q", execErr.Code, tc.wantErrCode)
				}
				return
			}
			if execErr != nil {
				t.Fatalf("unexpected error = %#v", execErr)
			}

			if tc.wantCWD != "" && capturedDir != tc.wantCWD {
				t.Fatalf("cwd = %q, want %q", capturedDir, tc.wantCWD)
			}
			for _, flag := range tc.wantFlags {
				if !slices.Contains(capturedArgs, flag) {
					t.Fatalf("args = %#v, missing flag %q", capturedArgs, flag)
				}
			}
			if tc.wantModelArg != "" && !containsArgPair(capturedArgs, "--model", tc.wantModelArg) {
				t.Fatalf("args = %#v, want --model %s", capturedArgs, tc.wantModelArg)
			}
			if tc.wantEnvContains != "" && !containsEnv(capturedEnv, tc.wantEnvContains) {
				t.Fatalf("env = %#v, want %q", capturedEnv, tc.wantEnvContains)
			}
			if tc.wantStdinContains != "" && !strings.Contains(string(capturedStdin), tc.wantStdinContains) {
				t.Fatalf("stdin = %q, want to contain %q", string(capturedStdin), tc.wantStdinContains)
			}
			if tc.wantTranscript != "" && string(response.Transcript) != tc.wantTranscript {
				t.Fatalf("transcript = %q, want %q", string(response.Transcript), tc.wantTranscript)
			}
			if tc.wantStructuredMode != "" {
				if response.Metadata["structuredOutputMode"] != tc.wantStructuredMode {
					t.Fatalf("structuredOutputMode = %#v, want %q", response.Metadata["structuredOutputMode"], tc.wantStructuredMode)
				}
			}
			if tc.wantValueApproved != nil {
				value, ok := response.Value.(map[string]any)
				if !ok {
					t.Fatalf("response value = %#v, want map", response.Value)
				}
				if value["approved"] != *tc.wantValueApproved {
					t.Fatalf("response value[approved] = %#v, want %v", value["approved"], *tc.wantValueApproved)
				}
			}
			if tc.wantStdoutFile && stdoutFile != nil {
				data, err := os.ReadFile(stdoutFile.Name())
				if err != nil {
					t.Fatalf("read stdout file: %v", err)
				}
				if string(data) != tc.runOutput {
					t.Fatalf("stdout file = %q, want %q", string(data), tc.runOutput)
				}
			}
		})
	}
}

// TestProviderStreamsStdoutWhileRunning verifies that chunks written by the
// runner reach the StdoutWriter before Execute returns.
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
		run: func(_ context.Context, _ []string, _ string, _ []string, _ []byte, stdout, _ io.Writer) error {
			if _, err := stdout.Write([]byte("chunk1\n")); err != nil {
				t.Errorf("write chunk1: %v", err)
			}
			close(firstChunkWritten)
			<-continueWriting
			if _, err := stdout.Write([]byte("chunk2\n")); err != nil {
				t.Errorf("write chunk2: %v", err)
			}
			return nil
		},
	}

	type execResult struct {
		response agent.Response
		err      *agent.Error
	}
	resultCh := make(chan execResult, 1)
	go func() {
		resp, execErr := prov.Execute(context.Background(), agent.Request{
			Prompt:       "test",
			Model:        "sonnet-5",
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
}

var containsArgPair = testutil.ContainsArgPair

var containsEnv = testutil.ContainsEnv
