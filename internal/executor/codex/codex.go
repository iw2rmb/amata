package codex

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

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
	args = append(args, "-o", outputPath, "-")

	runErr := p.run(ctx, args, request.CWD, agent.CommandEnv(request.Env), []byte(request.Prompt), stdoutObserver, request.StderrWriter)
	if closeErr := stdoutObserver.Close(); runErr == nil && closeErr != nil {
		runErr = closeErr
	}
	providerError := stdoutObserver.ProviderErrorDetails()

	transcript, readErr := os.ReadFile(outputPath)

	response := agent.Response{
		Prompt:     request.Prompt,
		Transcript: transcript,
		Metadata: map[string]any{
			"command": executor.CommandWithBinary("codex", args),
		},
	}
	if request.Structured != nil {
		response.Metadata["structuredOutputMode"] = "provider_schema"
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
