package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/iw2rmb/amata/internal/executor"
	"github.com/iw2rmb/amata/internal/executor/agent"
)

// RunnerFunc executes the codex CLI. The default implementation uses os/exec.
// Tests may supply a fake via NewWithRunner.
type RunnerFunc func(ctx context.Context, args []string, dir string, env []string, stdin []byte, stdout, stderr io.Writer) error

type provider struct {
	run RunnerFunc
}

func New() executor.Executor {
	return NewWithRunner(execRun)
}

// NewWithRunner returns an executor that uses run to invoke the codex CLI.
// Intended for tests that need to inject a controllable fake.
func NewWithRunner(run RunnerFunc) executor.Executor {
	return agent.New(provider{run: run})
}

func (provider) Name() string {
	return "codex"
}

func (p provider) Execute(ctx context.Context, request agent.Request) (agent.Response, *agent.Error) {
	outputPath := filepath.Join(request.ArtifactDir, "last-message.txt")
	stdoutObserver := agent.NewProviderErrorObserver(request.StdoutWriter, request.StderrWriter)
	var observedStdout bytes.Buffer
	stdoutWriter := io.MultiWriter(stdoutObserver, &observedStdout)

	args := []string{
		"exec",
		"--dangerously-bypass-approvals-and-sandbox",
		"--color", "never",
		"-C", request.CWD,
		"--model", request.Model,
		"--json",
	}
	if request.Reasoning != "" {
		args = append(args, "-c", fmt.Sprintf("model_reasoning_effort=%q", request.Reasoning))
	}
	if request.Structured != nil {
		args = append(args, "--output-schema", request.Structured.SchemaPath)
	}
	args = append(args, "-o", outputPath)

	prompt := request.Prompt
	stdin := []byte(prompt)
	if request.ContinuationSessionID != "" {
		args = append(args, "resume", request.ContinuationSessionID, prompt)
		stdin = nil
	} else {
		args = append(args, "-")
	}

	runErr := p.run(ctx, args, request.CWD, agent.CommandEnv(request.Env), stdin, stdoutWriter, request.StderrWriter)
	if closeErr := stdoutObserver.Close(); runErr == nil && closeErr != nil {
		runErr = closeErr
	}
	sessionID := continuationSessionID(observedStdout.Bytes())
	providerError := withContinuationSessionID(stdoutObserver.ProviderErrorDetails(), sessionID)

	transcript, readErr := os.ReadFile(outputPath)

	response := agent.Response{
		Prompt:     prompt,
		Transcript: transcript,
		Metadata: map[string]any{
			"command": executor.CommandWithBinary("codex", args),
		},
	}
	if request.Structured != nil {
		response.Metadata["structuredOutputMode"] = "provider_schema"
	}
	if sessionID != "" {
		response.Metadata["continuation_session_id"] = sessionID
	}

	if runErr != nil {
		return response, attachProviderError(&agent.Error{
			Code:    "agent_failed",
			Message: fmt.Sprintf("codex exec failed: %v", runErr),
		}, providerError)
	}

	if readErr != nil {
		if os.IsNotExist(readErr) {
			return invalidProviderOutput(response, "codex did not produce a final message", providerError)
		}
		return invalidProviderOutput(response, fmt.Sprintf("read last message: %v", readErr), providerError)
	}
	if len(transcript) == 0 {
		return invalidProviderOutput(response, "codex did not produce a final message", providerError)
	}
	if request.Structured != nil {
		value, err := agent.ParseStructuredOutput(transcript)
		if err != nil {
			return invalidProviderOutput(response, fmt.Sprintf("codex output is invalid: %v", err), providerError)
		}
		response.Value = value
		response.HasValue = true
	}

	return response, nil
}

func execRun(ctx context.Context, args []string, dir string, env []string, stdin []byte, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, "codex", args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

func invalidProviderOutput(response agent.Response, message string, providerError map[string]any) (agent.Response, *agent.Error) {
	return response, attachProviderError(&agent.Error{
		Code:    "invalid_provider_output",
		Message: message,
	}, providerError)
}

func attachProviderError(err *agent.Error, providerError map[string]any) *agent.Error {
	if err == nil || len(providerError) == 0 {
		return err
	}
	details := map[string]any{}
	for key, value := range err.Details {
		details[key] = value
	}
	details["provider_error"] = providerError
	err.Details = details
	return err
}

func withContinuationSessionID(providerError map[string]any, sessionID string) map[string]any {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return providerError
	}

	details := map[string]any{}
	for key, value := range providerError {
		details[key] = value
	}
	details["session_id"] = sessionID
	return details
}

func continuationSessionID(stdout []byte) string {
	if len(stdout) == 0 {
		return ""
	}

	scanner := bufio.NewScanner(bytes.NewReader(stdout))
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	sessionID := ""
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		if id := continuationSessionIDFromMap(event); id != "" {
			sessionID = id
		}
	}
	return sessionID
}

func continuationSessionIDFromMap(value map[string]any) string {
	for _, key := range []string{"session_id", "thread_id"} {
		if id, ok := value[key].(string); ok && strings.TrimSpace(id) != "" {
			return strings.TrimSpace(id)
		}
	}
	for _, nestedKey := range []string{"payload", "item"} {
		nested, ok := value[nestedKey].(map[string]any)
		if !ok || len(nested) == 0 {
			continue
		}
		if id := continuationSessionIDFromMap(nested); id != "" {
			return id
		}
	}
	return ""
}
