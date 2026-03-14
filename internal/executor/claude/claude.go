package claude

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"

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
	runner                    runner
	structuredOutputSupported bool
}

type execRunner struct{}

func New() executor.Executor {
	return agent.New(provider{
		runner:                    execRunner{},
		structuredOutputSupported: true,
	})
}

func (provider) Name() string {
	return "claude"
}

func (p provider) Execute(ctx context.Context, request agent.Request) (agent.Response, *agent.Error) {
	prompt := request.Prompt
	args := []string{
		"-p",
		"--permission-mode", "bypassPermissions",
		"--model", request.Model,
	}
	if request.Reasoning != "" {
		args = append(args, "--effort", request.Reasoning)
	}

	structuredOutputMode := ""
	useJSONOutputFormat := false
	if request.Structured != nil {
		if p.structuredOutputSupported {
			args = append(args, "--output-format", "json", "--json-schema", request.Structured.JSON)
			structuredOutputMode = "provider_schema"
			useJSONOutputFormat = true
		} else {
			prompt = agent.StructuredPrompt(prompt, request.Structured.JSON)
			structuredOutputMode = "prompt_fallback"
		}
	}
	if !useJSONOutputFormat {
		args = append(args, "--output-format", "text")
	}

	spec := command{
		args:  args,
		dir:   request.CWD,
		env:   agent.CommandEnv(request.Env),
		stdin: []byte(prompt),
	}

	result, runErr := p.runner.Run(ctx, spec)
	response := agent.Response{
		Prompt:     prompt,
		Transcript: result.stdout,
		Stdout:     result.stdout,
		Stderr:     result.stderr,
		Metadata: map[string]any{
			"command": commandWithBinary("claude", args),
		},
	}
	if structuredOutputMode != "" {
		response.Metadata["structuredOutputMode"] = structuredOutputMode
	}

	if runErr != nil {
		return response, &agent.Error{
			Code:    "agent_failed",
			Message: fmt.Sprintf("claude -p failed: %v", runErr),
		}
	}

	if structuredOutputMode != "" {
		value, err := agent.ParseStructuredOutput(result.stdout)
		if err != nil {
			return response, &agent.Error{
				Code:    "invalid_provider_output",
				Message: fmt.Sprintf("claude output is invalid: %v", err),
			}
		}
		response.Value = value
		response.HasValue = true
	}

	return response, nil
}

func (execRunner) Run(ctx context.Context, spec command) (commandResult, error) {
	cmd := exec.CommandContext(ctx, "claude", spec.args...)
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

func commandWithBinary(binary string, args []string) []string {
	command := make([]string, 0, len(args)+1)
	command = append(command, binary)
	command = append(command, args...)
	return command
}
