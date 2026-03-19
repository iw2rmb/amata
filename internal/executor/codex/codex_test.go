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
			return commandResult{}, nil
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
			return commandResult{}, errors.New("exit status 1")
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
			close(firstChunkWritten)
			<-continueWriting
			if _, err := spec.stdoutWriter.Write([]byte("chunk2\n")); err != nil {
				t.Errorf("write chunk2: %v", err)
			}
			if err := os.WriteFile(filepath.Join(artifactDir, "last-message.txt"), []byte("result\n"), 0o644); err != nil {
				t.Errorf("write last-message: %v", err)
			}
			return commandResult{}, nil
		}),
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

func TestProviderPreservesPartialOutputOnCancellation(t *testing.T) {
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

	prov := provider{
		runner: fakeRunner(func(_ context.Context, spec command) (commandResult, error) {
			if _, err := spec.stdoutWriter.Write([]byte("partial stdout\n")); err != nil {
				t.Errorf("write stdout: %v", err)
			}
			if _, err := spec.stderrWriter.Write([]byte("partial stderr\n")); err != nil {
				t.Errorf("write stderr: %v", err)
			}
			cancel()
			return commandResult{}, errors.New("signal: killed")
		}),
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
		t.Fatalf("expected execute error on cancellation")
	}
	if execErr.Code != "agent_failed" {
		t.Fatalf("error code = %q, want agent_failed", execErr.Code)
	}

	assertFileContents(t, stdoutPath, "partial stdout\n")
	assertFileContents(t, stderrPath, "partial stderr\n")
}

func TestProviderPreservesPartialOutputOnNonZeroExit(t *testing.T) {
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
			return commandResult{}, errors.New("exit status 1")
		}),
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
		t.Fatalf("expected execute error on non-zero exit")
	}
	if execErr.Code != "agent_failed" {
		t.Fatalf("error code = %q, want agent_failed", execErr.Code)
	}

	assertFileContents(t, stdoutPath, "partial output\n")
	assertFileContents(t, stderrPath, "error detail\n")
}

func assertFileContents(t *testing.T, path string, want string) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(data) != want {
		t.Fatalf("%s = %q, want %q", path, string(data), want)
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
