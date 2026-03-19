package claude

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"

	"github.com/iw2rmb/amata/internal/executor"
	"github.com/iw2rmb/amata/internal/executor/agent"
)

type runner interface {
	Run(context.Context, command) (commandResult, error)
}

type command struct {
	args         []string
	dir          string
	env          []string
	stdin        []byte
	stdoutWriter io.Writer
	stderrWriter io.Writer
}

type commandResult struct {
	stdout []byte
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
		args:         args,
		dir:          request.CWD,
		env:          agent.CommandEnv(request.Env),
		stdin:        []byte(prompt),
		stdoutWriter: request.StdoutWriter,
		stderrWriter: request.StderrWriter,
	}

	result, runErr := p.runner.Run(ctx, spec)
	response := agent.Response{
		Prompt:     prompt,
		Transcript: result.stdout,
		Metadata: map[string]any{
			"command": executor.CommandWithBinary("claude", args),
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
		if structuredOutputMode == "provider_schema" {
			value = unwrapProviderStructuredOutput(value)
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

	var stdoutBuf bytes.Buffer
	if spec.stdoutWriter != nil {
		cmd.Stdout = io.MultiWriter(&stdoutBuf, spec.stdoutWriter)
	} else {
		cmd.Stdout = &stdoutBuf
	}
	if spec.stderrWriter != nil {
		cmd.Stderr = spec.stderrWriter
	} else {
		cmd.Stderr = io.Discard
	}

	err := cmd.Run()
	return commandResult{stdout: stdoutBuf.Bytes()}, err
}

func unwrapProviderStructuredOutput(value any) any {
	envelope, ok := value.(map[string]any)
	if !ok {
		return value
	}

	inner, ok := envelope["structured_output"]
	if !ok {
		return value
	}

	if !looksLikeClaudeJSONEnvelope(envelope) {
		return value
	}
	return inner
}

func looksLikeClaudeJSONEnvelope(envelope map[string]any) bool {
	if typ, _ := envelope["type"].(string); typ == "result" {
		return true
	}

	_, hasSessionID := envelope["session_id"]
	_, hasStopReason := envelope["stop_reason"]
	_, hasUsage := envelope["usage"]
	return hasSessionID && hasStopReason && hasUsage
}
