package codex

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"auto/internal/executor"
	"auto/internal/executor/agent"
)

type runner interface {
	Run(context.Context, command) (commandResult, error)
}

type command struct {
	args  []string
	dir   string
	env   []string
	stdin []byte
}

type commandResult struct {
	stdout []byte
	stderr []byte
}

type provider struct {
	runner runner
}

type execRunner struct{}

func New() executor.Executor {
	return agent.New(provider{runner: execRunner{}})
}

func (provider) Name() string {
	return "codex"
}

func (p provider) Execute(ctx context.Context, request agent.Request) (agent.Response, *agent.Error) {
	outputPath := filepath.Join(request.ArtifactDir, "last-message.txt")

	args := []string{
		"exec",
		"--dangerously-bypass-approvals-and-sandbox",
		"--color", "never",
		"-C", request.CWD,
		"--model", request.Model,
	}
	if request.Reasoning != "" {
		args = append(args, "-c", fmt.Sprintf("model_reasoning_effort=%q", request.Reasoning))
	}
	if request.Structured != nil {
		args = append(args, "--output-schema", request.Structured.SchemaPath)
	}
	args = append(args, "-o", outputPath, "-")

	spec := command{
		args:  args,
		dir:   request.CWD,
		env:   agent.CommandEnv(request.Env),
		stdin: []byte(request.Prompt),
	}

	result, runErr := p.runner.Run(ctx, spec)

	transcript, readErr := os.ReadFile(outputPath)

	response := agent.Response{
		Prompt:     request.Prompt,
		Transcript: transcript,
		Stdout:     result.stdout,
		Stderr:     result.stderr,
		Metadata: map[string]any{
			"command": executor.CommandWithBinary("codex", args),
		},
	}
	if request.Structured != nil {
		response.Metadata["structuredOutputMode"] = "provider_schema"
	}

	if runErr != nil {
		return response, &agent.Error{
			Code:    "agent_failed",
			Message: fmt.Sprintf("codex exec failed: %v", runErr),
		}
	}

	if readErr != nil {
		if os.IsNotExist(readErr) {
			return invalidProviderOutput(response, "codex did not produce a final message")
		}
		return invalidProviderOutput(response, fmt.Sprintf("read last message: %v", readErr))
	}
	if len(transcript) == 0 {
		return invalidProviderOutput(response, "codex did not produce a final message")
	}
	if request.Structured != nil {
		value, err := agent.ParseStructuredOutput(transcript)
		if err != nil {
			return invalidProviderOutput(response, fmt.Sprintf("codex output is invalid: %v", err))
		}
		response.Value = value
		response.HasValue = true
	}

	return response, nil
}

func (execRunner) Run(ctx context.Context, spec command) (commandResult, error) {
	cmd := exec.CommandContext(ctx, "codex", spec.args...)
	cmd.Dir = spec.dir
	cmd.Env = spec.env
	cmd.Stdin = bytes.NewReader(spec.stdin)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	return commandResult{
		stdout: stdout.Bytes(),
		stderr: stderr.Bytes(),
	}, err
}

func invalidProviderOutput(response agent.Response, message string) (agent.Response, *agent.Error) {
	return response, &agent.Error{
		Code:    "invalid_provider_output",
		Message: message,
	}
}
