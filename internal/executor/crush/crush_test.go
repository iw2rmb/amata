package crush

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iw2rmb/amata/internal/executor/agent"
)

func TestProviderPassesModelPromptAndRequiredFlags(t *testing.T) {
	t.Parallel()

	var capturedArgs []string
	var capturedDir string
	var capturedEnv []string
	var capturedStdin []byte

	prov := provider{
		run: func(_ context.Context, args []string, dir string, env []string, stdin []byte, stdout, _ io.Writer) error {
			capturedArgs = args
			capturedDir = dir
			capturedEnv = env
			capturedStdin = stdin
			_, err := stdout.Write([]byte("result output\n"))
			return err
		},
	}

	response, execErr := prov.Execute(context.Background(), agent.Request{
		Prompt: "Implement item",
		Model:  "sonnet-5",
		CWD:    "/repo",
		Env:    map[string]string{"CRUSH_TEST": "1"},
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
	if !containsString(capturedArgs, "--yolo") {
		t.Fatalf("args = %#v, want --yolo", capturedArgs)
	}
	if !containsString(capturedArgs, "--quiet") {
		t.Fatalf("args = %#v, want --quiet", capturedArgs)
	}
	if !containsArgPair(capturedArgs, "--model", "sonnet-5") {
		t.Fatalf("args = %#v, want --model sonnet-5", capturedArgs)
	}
	if !containsArgPair(capturedArgs, "run", "--yolo") {
		t.Fatalf("args = %#v, want run as first arg", capturedArgs)
	}
	if !containsEnv(capturedEnv, "CRUSH_TEST=1") {
		t.Fatalf("env = %#v, want CRUSH_TEST override", capturedEnv)
	}

	if response.Prompt != "Implement item" {
		t.Fatalf("prompt = %q, want original prompt", response.Prompt)
	}
	if string(response.Transcript) != "result output\n" {
		t.Fatalf("transcript = %q, want stdout content", string(response.Transcript))
	}
}

func TestProviderStructuredOutputUsesPromptFallback(t *testing.T) {
	t.Parallel()

	var capturedStdin []byte

	prov := provider{
		run: func(_ context.Context, _ []string, _ string, _ []string, stdin []byte, stdout, _ io.Writer) error {
			capturedStdin = stdin
			_, err := stdout.Write([]byte("```json\n{\"approved\":true}\n```\n"))
			return err
		},
	}

	response, execErr := prov.Execute(context.Background(), agent.Request{
		Prompt: "Review the diff",
		Model:  "sonnet-5",
		CWD:    "/repo",
		Structured: &agent.StructuredOutput{
			JSON: `{"type":"object","properties":{"approved":{"type":"boolean"}},"required":["approved"]}`,
		},
	})
	if execErr != nil {
		t.Fatalf("execute error = %#v", execErr)
	}

	if !strings.Contains(string(capturedStdin), "Return only JSON that matches this schema.") {
		t.Fatalf("prompt = %q, want schema fragment in prompt", string(capturedStdin))
	}
	if response.Prompt != string(capturedStdin) {
		t.Fatalf("response.Prompt = %q, want adjusted prompt", response.Prompt)
	}
	if response.Metadata["structuredOutputMode"] != "prompt_fallback" {
		t.Fatalf("structuredOutputMode = %#v, want prompt_fallback", response.Metadata["structuredOutputMode"])
	}

	value, ok := response.Value.(map[string]any)
	if !ok || value["approved"] != true {
		t.Fatalf("response value = %#v, want parsed structured JSON", response.Value)
	}
}

func TestProviderRejectsReasoningWithUnsupportedOptionError(t *testing.T) {
	t.Parallel()

	prov := provider{
		run: func(_ context.Context, _ []string, _ string, _ []string, _ []byte, _, _ io.Writer) error {
			t.Fatal("runner must not be called when reasoning is rejected")
			return nil
		},
	}

	_, execErr := prov.Execute(context.Background(), agent.Request{
		Prompt:    "Implement item",
		Model:     "sonnet-5",
		Reasoning: "high",
		CWD:       "/repo",
	})
	if execErr == nil {
		t.Fatal("expected error for unsupported reasoning")
	}
	if execErr.Code != "unsupported_option" {
		t.Fatalf("error code = %q, want unsupported_option", execErr.Code)
	}
}

func TestProviderReportsAgentFailure(t *testing.T) {
	t.Parallel()

	prov := provider{
		run: func(_ context.Context, _ []string, _ string, _ []string, _ []byte, _, _ io.Writer) error {
			return errors.New("exit status 1")
		},
	}

	_, execErr := prov.Execute(context.Background(), agent.Request{
		Prompt: "Implement item",
		Model:  "sonnet-5",
		CWD:    "/repo",
	})
	if execErr == nil {
		t.Fatal("expected execute error")
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

func containsArgPair(args []string, name string, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == name && args[i+1] == value {
			return true
		}
	}
	return false
}

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

func containsEnv(values []string, want string) bool {
	for _, v := range values {
		if strings.HasPrefix(v, want) {
			return true
		}
	}
	return false
}
