package claude

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	args                   []string
	dir                    string
	env                    []string
	stdin                  []byte
	stdoutWriter           io.Writer
	stderrWriter           io.Writer
	stopOnStructuredOutput bool
}

type commandResult struct {
	stdout              []byte
	sessionID           string
	structuredOutput    any
	hasStructuredOutput bool
}

type provider struct {
	runner                    runner
	structuredOutputSupported bool
}

type execRunner struct{}

const providerSchemaRetryInstruction = "\n\nReturn only strict JSON that matches the requested response schema. Do not include prose outside JSON."

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
		"--verbose",
		"--output-format", "stream-json",
		"--include-partial-messages",
		"--permission-mode", "bypassPermissions",
		"--model", request.Model,
	}
	if request.Reasoning != "" {
		args = append(args, "--effort", request.Reasoning)
	}

	structuredOutputMode := ""
	if request.Structured != nil {
		if p.structuredOutputSupported {
			args = append(args, "--json-schema", request.Structured.JSON)
			structuredOutputMode = "provider_schema"
		} else {
			prompt = agent.StructuredPrompt(prompt, request.Structured.JSON)
			structuredOutputMode = "prompt_fallback"
		}
	}

	runCommand := func(runArgs []string, runPrompt string) (commandResult, error) {
		return p.runner.Run(ctx, command{
			args:                   append([]string(nil), runArgs...),
			dir:                    request.CWD,
			env:                    agent.CommandEnv(request.Env),
			stdin:                  []byte(runPrompt),
			stdoutWriter:           request.StdoutWriter,
			stderrWriter:           request.StderrWriter,
			stopOnStructuredOutput: structuredOutputMode == "provider_schema",
		})
	}

	result, runErr := runCommand(args, prompt)
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

	if structuredOutputMode == "provider_schema" {
		if result.hasStructuredOutput {
			response.Value = result.structuredOutput
			response.HasValue = true
			return response, nil
		}

		retryPrompt := request.Prompt + providerSchemaRetryInstruction
		attempts := 1
		response.Metadata["structuredOutputRetryOrder"] = "resume_then_fresh"

		if result.sessionID != "" {
			resumeArgs := append(append([]string(nil), args...), "--resume", result.sessionID)
			retryResult, retryErr := runCommand(resumeArgs, retryPrompt)
			attempts++
			response.Prompt = retryPrompt
			response.Transcript = retryResult.stdout
			response.Metadata["command"] = executor.CommandWithBinary("claude", resumeArgs)
			if retryErr == nil && retryResult.hasStructuredOutput {
				response.Value = retryResult.structuredOutput
				response.HasValue = true
				response.Metadata["structuredOutputAttempts"] = attempts
				return response, nil
			}
		}

		freshResult, freshErr := runCommand(args, retryPrompt)
		attempts++
		response.Prompt = retryPrompt
		response.Transcript = freshResult.stdout
		response.Metadata["command"] = executor.CommandWithBinary("claude", args)
		response.Metadata["structuredOutputAttempts"] = attempts
		if freshErr != nil {
			return response, &agent.Error{
				Code:    "agent_failed",
				Message: fmt.Sprintf("claude -p failed: %v", freshErr),
			}
		}
		if !freshResult.hasStructuredOutput {
			return response, &agent.Error{
				Code:    "invalid_provider_output",
				Message: "claude output is invalid: provider schema response envelope is missing structured_output",
			}
		}

		response.Value = freshResult.structuredOutput
		response.HasValue = true
		return response, nil
	}

	if structuredOutputMode == "prompt_fallback" {
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
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "claude", spec.args...)
	cmd.Dir = spec.dir
	cmd.Env = spec.env
	cmd.Stdin = bytes.NewReader(spec.stdin)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return commandResult{}, err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return commandResult{}, err
	}

	if err := cmd.Start(); err != nil {
		return commandResult{}, err
	}

	type stdoutOutcome struct {
		result commandResult
		err    error
	}
	stdoutCh := make(chan stdoutOutcome, 1)
	go func() {
		reader := bufio.NewReader(stdoutPipe)
		var out commandResult
		var stdoutBuf bytes.Buffer
		for {
			chunk, readErr := reader.ReadBytes('\n')
			if len(chunk) > 0 {
				stdoutBuf.Write(chunk)
				if spec.stdoutWriter != nil {
					if _, err := spec.stdoutWriter.Write(chunk); err != nil {
						stdoutCh <- stdoutOutcome{err: err}
						return
					}
				}

				trimmed := bytes.TrimSpace(chunk)
				if len(trimmed) > 0 && trimmed[0] == '{' {
					var event map[string]any
					if err := json.Unmarshal(trimmed, &event); err == nil {
						if typ, _ := event["type"].(string); typ == "result" {
							if sid, _ := event["session_id"].(string); sid != "" {
								out.sessionID = sid
							}
							if structured, ok := event["structured_output"]; ok && !out.hasStructuredOutput {
								out.structuredOutput = structured
								out.hasStructuredOutput = true
								if spec.stopOnStructuredOutput {
									cancel()
								}
							}
						}
					}
				}
			}
			if readErr != nil {
				if errors.Is(readErr, io.EOF) {
					break
				}
				stdoutCh <- stdoutOutcome{err: readErr}
				return
			}
		}
		out.stdout = stdoutBuf.Bytes()
		stdoutCh <- stdoutOutcome{result: out}
	}()

	stderrCh := make(chan error, 1)
	go func() {
		writer := spec.stderrWriter
		if writer == nil {
			writer = io.Discard
		}
		_, copyErr := io.Copy(writer, stderrPipe)
		stderrCh <- copyErr
	}()

	waitErr := cmd.Wait()
	stdoutOutcomeResult := <-stdoutCh
	stderrErr := <-stderrCh

	result := stdoutOutcomeResult.result
	return resolveRunOutcome(ctx, runCtx, spec, result, stdoutOutcomeResult.err, stderrErr, waitErr)
}

func resolveRunOutcome(
	ctx context.Context,
	runCtx context.Context,
	spec command,
	result commandResult,
	stdoutErr error,
	stderrErr error,
	waitErr error,
) (commandResult, error) {
	if spec.stopOnStructuredOutput &&
		result.hasStructuredOutput &&
		errors.Is(runCtx.Err(), context.Canceled) &&
		ctx.Err() == nil {
		return result, nil
	}

	if stdoutErr != nil {
		return commandResult{}, stdoutErr
	}
	if stderrErr != nil {
		return commandResult{}, stderrErr
	}
	if waitErr != nil {
		return result, waitErr
	}
	return result, nil
}
