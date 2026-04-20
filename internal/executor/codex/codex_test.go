package codex

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/iw2rmb/amata/internal/executor/agent"
	"github.com/iw2rmb/amata/internal/testutil"
)

func TestProviderPassesSettingsAndParsesStructuredOutput(t *testing.T) {
	t.Parallel()

	artifactDir := t.TempDir()

	var capturedArgs []string
	var capturedDir string
	var capturedEnv []string
	var capturedStdin []byte

	prov := provider{
		run: func(_ context.Context, args []string, dir string, env []string, stdin []byte, _, _ io.Writer) error {
			capturedArgs = args
			capturedDir = dir
			capturedEnv = env
			capturedStdin = stdin
			return os.WriteFile(filepath.Join(artifactDir, "last-message.txt"), []byte("{\"approved\":true}\n"), 0o644)
		},
	}

	response, execErr := prov.Execute(context.Background(), agent.Request{
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

	if capturedDir != "/repo" {
		t.Fatalf("dir = %q, want /repo", capturedDir)
	}
	if string(capturedStdin) != "Implement item" {
		t.Fatalf("stdin = %q, want prompt", string(capturedStdin))
	}
	if !containsArgPair(capturedArgs, "--model", "gpt-5.4") {
		t.Fatalf("args = %#v, want --model gpt-5.4", capturedArgs)
	}
	if !slices.Contains(capturedArgs, "--json") {
		t.Fatalf("args = %#v, want --json", capturedArgs)
	}
	if !containsArgPair(capturedArgs, "--output-schema", filepath.Join(artifactDir, "response-schema.json")) {
		t.Fatalf("args = %#v, want structured schema flag", capturedArgs)
	}
	if !containsArgPair(capturedArgs, "-o", filepath.Join(artifactDir, "last-message.txt")) {
		t.Fatalf("args = %#v, want last message output flag", capturedArgs)
	}
	if !slices.Contains(capturedArgs, `model_reasoning_effort="high"`) {
		t.Fatalf("args = %#v, want reasoning setting", capturedArgs)
	}
	if !containsEnv(capturedEnv, "CODEX_TEST=1") {
		t.Fatalf("env = %#v, want CODEX_TEST override", capturedEnv)
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

func TestProviderUsesResumeCommandForContinuationSession(t *testing.T) {
	t.Parallel()

	artifactDir := t.TempDir()

	var capturedArgs []string
	var capturedStdin []byte

	prov := provider{
		run: func(_ context.Context, args []string, _ string, _ []string, stdin []byte, _, _ io.Writer) error {
			capturedArgs = args
			capturedStdin = stdin
			return os.WriteFile(filepath.Join(artifactDir, "last-message.txt"), []byte("{\"approved\":true}\n"), 0o644)
		},
	}

	response, execErr := prov.Execute(context.Background(), agent.Request{
		Prompt:                "continue",
		Model:                 "gpt-5.4",
		CWD:                   "/repo",
		ArtifactDir:           artifactDir,
		ContinuationSessionID: "session-abc",
		Structured: &agent.StructuredOutput{
			SchemaPath: filepath.Join(artifactDir, "response-schema.json"),
		},
	})
	if execErr != nil {
		t.Fatalf("execute error = %#v", execErr)
	}

	if !containsArgPair(capturedArgs, "--model", "gpt-5.4") {
		t.Fatalf("args = %#v, want --model gpt-5.4", capturedArgs)
	}
	if !containsArgPair(capturedArgs, "--output-schema", filepath.Join(artifactDir, "response-schema.json")) {
		t.Fatalf("args = %#v, want structured schema flag", capturedArgs)
	}
	if !containsArgPair(capturedArgs, "-o", filepath.Join(artifactDir, "last-message.txt")) {
		t.Fatalf("args = %#v, want last message output flag", capturedArgs)
	}
	wantResumeSuffix := []string{"resume", "session-abc", "continue"}
	if len(capturedArgs) < len(wantResumeSuffix) || !slices.Equal(capturedArgs[len(capturedArgs)-len(wantResumeSuffix):], wantResumeSuffix) {
		t.Fatalf("args suffix = %#v, want %#v", capturedArgs, wantResumeSuffix)
	}
	if len(capturedStdin) != 0 {
		t.Fatalf("stdin = %q, want empty stdin for resume mode", string(capturedStdin))
	}
	if response.Prompt != "continue" {
		t.Fatalf("prompt = %q, want continuation prompt", response.Prompt)
	}
}

func TestProviderReportsAgentFailureBeforeTranscriptValidation(t *testing.T) {
	t.Parallel()

	artifactDir := t.TempDir()
	prov := provider{
		run: func(_ context.Context, _ []string, _ string, _ []string, _ []byte, _, _ io.Writer) error {
			return errors.New("exit status 1")
		},
	}

	_, execErr := prov.Execute(context.Background(), agent.Request{
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
		run: func(_ context.Context, args []string, _ string, _ []string, _ []byte, stdout, _ io.Writer) error {
			if _, err := stdout.Write([]byte("chunk1\n")); err != nil {
				t.Errorf("write chunk1: %v", err)
			}
			close(firstChunkWritten)
			<-continueWriting
			if _, err := stdout.Write([]byte("chunk2\n")); err != nil {
				t.Errorf("write chunk2: %v", err)
			}
			var outputPath string
			for i, arg := range args {
				if arg == "-o" && i+1 < len(args) {
					outputPath = args[i+1]
				}
			}
			return os.WriteFile(outputPath, []byte("result\n"), 0o644)
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
			Model:        "gpt-5.4",
			CWD:          "/repo",
			ArtifactDir:  artifactDir,
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
}

func TestProviderPreservesPartialOutputOnFailure(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		cancel     bool
		runErr     string
		wantStdout string
		wantStderr string
	}{
		{
			name:       "cancellation",
			cancel:     true,
			runErr:     "signal: killed",
			wantStdout: "partial stdout\n",
			wantStderr: "partial stderr\n",
		},
		{
			name:       "non-zero exit",
			cancel:     false,
			runErr:     "exit status 1",
			wantStdout: "partial output\n",
			wantStderr: "error detail\n",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
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

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			prov := provider{
				run: func(_ context.Context, _ []string, _ string, _ []string, _ []byte, stdout, stderr io.Writer) error {
					if _, err := stdout.Write([]byte(tc.wantStdout)); err != nil {
						t.Errorf("write stdout: %v", err)
					}
					if _, err := stderr.Write([]byte(tc.wantStderr)); err != nil {
						t.Errorf("write stderr: %v", err)
					}
					if tc.cancel {
						cancel()
					}
					return errors.New(tc.runErr)
				},
			}

			_, execErr := prov.Execute(ctx, agent.Request{
				Prompt:       "test",
				Model:        "gpt-5.4",
				CWD:          "/repo",
				ArtifactDir:  artifactDir,
				StdoutWriter: stdoutFile,
				StderrWriter: stderrFile,
			})
			if execErr == nil {
				t.Fatalf("expected execute error")
			}
			if execErr.Code != "agent_failed" {
				t.Fatalf("error code = %q, want agent_failed", execErr.Code)
			}

			testutil.AssertFileContents(t, stdoutPath, tc.wantStdout)
			testutil.AssertFileContents(t, stderrPath, tc.wantStderr)
		})
	}
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
		run: func(_ context.Context, _ []string, _ string, _ []string, _ []byte, stdout, _ io.Writer) error {
			if _, err := stdout.Write([]byte(rawLine + "\n")); err != nil {
				return err
			}
			return errors.New("exit status 1")
		},
	}

	_, execErr := prov.Execute(context.Background(), agent.Request{
		Prompt:       "test",
		Model:        "gpt-5.4",
		CWD:          "/repo",
		ArtifactDir:  artifactDir,
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

func TestProviderAttachesContinuationSessionIDToProviderError(t *testing.T) {
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
		run: func(_ context.Context, _ []string, _ string, _ []string, _ []byte, stdout, _ io.Writer) error {
			if _, err := stdout.Write([]byte(`{"type":"thread.started","thread_id":"sess-thread-1"}` + "\n")); err != nil {
				return err
			}
			if _, err := stdout.Write([]byte(`{"type":"error","message":"exceeded retry limit, last status: 429 Too Many Requests, request id: req_123"}` + "\n")); err != nil {
				return err
			}
			return errors.New("exit status 1")
		},
	}

	_, execErr := prov.Execute(context.Background(), agent.Request{
		Prompt:       "test",
		Model:        "gpt-5.4",
		CWD:          "/repo",
		ArtifactDir:  artifactDir,
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
	if got := providerError["session_id"]; got != "sess-thread-1" {
		t.Fatalf("provider_error.session_id = %#v, want sess-thread-1", got)
	}
}

var containsArgPair = testutil.ContainsArgPair

var containsEnv = testutil.ContainsEnv
