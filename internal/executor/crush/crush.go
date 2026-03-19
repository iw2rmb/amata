package crush

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"

	"github.com/iw2rmb/amata/internal/executor"
	"github.com/iw2rmb/amata/internal/executor/agent"
)

// RunnerFunc executes the crush CLI. The default implementation uses os/exec.
// Tests may supply a fake via NewWithRunner.
type RunnerFunc func(ctx context.Context, args []string, dir string, env []string, stdin []byte, stdout, stderr io.Writer) error

type provider struct {
	run RunnerFunc
}

func New() executor.Executor {
	return NewWithRunner(execRun)
}

// NewWithRunner returns an executor that uses run to invoke the crush CLI.
// Intended for tests that need to inject a controllable fake.
func NewWithRunner(run RunnerFunc) executor.Executor {
	return agent.New(provider{run: run})
}

func (provider) Name() string {
	return "crush"
}

func (p provider) Execute(ctx context.Context, request agent.Request) (agent.Response, *agent.Error) {
	if request.Reasoning != "" {
		return agent.Response{}, &agent.Error{
			Code:    "unsupported_option",
			Message: "crush does not support reasoning; remove reasoning from this step",
		}
	}

	prompt := request.Prompt
	structuredOutputMode := ""
	if request.Structured != nil {
		prompt = agent.StructuredPrompt(prompt, request.Structured.JSON)
		structuredOutputMode = "prompt_fallback"
	}

	args := []string{
		"run",
		"--yolo",
		"--quiet",
		"--model", request.Model,
	}

	var transcriptBuf bytes.Buffer
	stdoutWriter := io.Writer(&transcriptBuf)
	if request.StdoutWriter != nil {
		stdoutWriter = io.MultiWriter(&transcriptBuf, request.StdoutWriter)
	}

	runErr := p.run(ctx, args, request.CWD, agent.CommandEnv(request.Env), []byte(prompt), stdoutWriter, request.StderrWriter)

	response := agent.Response{
		Prompt:     prompt,
		Transcript: transcriptBuf.Bytes(),
		Metadata: map[string]any{
			"command": executor.CommandWithBinary("crush", args),
		},
	}
	if structuredOutputMode != "" {
		response.Metadata["structuredOutputMode"] = structuredOutputMode
	}

	if runErr != nil {
		return response, &agent.Error{
			Code:    "agent_failed",
			Message: fmt.Sprintf("crush run failed: %v", runErr),
		}
	}

	if request.Structured != nil {
		value, err := agent.ParseStructuredOutput(transcriptBuf.Bytes())
		if err != nil {
			return response, &agent.Error{
				Code:    "invalid_provider_output",
				Message: fmt.Sprintf("crush output is invalid: %v", err),
			}
		}
		response.Value = value
		response.HasValue = true
	}

	return response, nil
}

func execRun(ctx context.Context, args []string, dir string, env []string, stdin []byte, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, "crush", args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}
