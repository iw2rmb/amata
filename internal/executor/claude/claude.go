package claude

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"

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
			args:         append([]string(nil), runArgs...),
			dir:          request.CWD,
			env:          agent.CommandEnv(request.Env),
			stdin:        []byte(runPrompt),
			stdoutWriter: request.StdoutWriter,
			stderrWriter: request.StderrWriter,
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

	if structuredOutputMode != "" {
		value, err := agent.ParseStructuredOutput(result.stdout)
		if err != nil {
			return response, &agent.Error{
				Code:    "invalid_provider_output",
				Message: fmt.Sprintf("claude output is invalid: %v", err),
			}
		}
		if structuredOutputMode == "provider_schema" {
			unwrapped, missingStructuredOutput, sessionID := selectProviderSchemaValue(value, result.stdout)
			if missingStructuredOutput {
				retryPrompt := request.Prompt + providerSchemaRetryInstruction
				attempts := 1
				response.Metadata["structuredOutputRetryOrder"] = "resume_then_fresh"

				if sessionID != "" {
					resumeArgs := append(append([]string(nil), args...), "--resume", sessionID)
					retryResult, retryErr := runCommand(resumeArgs, retryPrompt)
					attempts++
					response.Prompt = retryPrompt
					response.Transcript = retryResult.stdout
					response.Metadata["command"] = executor.CommandWithBinary("claude", resumeArgs)
					if retryErr == nil {
						retryValue, parseErr := agent.ParseStructuredOutput(retryResult.stdout)
						if parseErr == nil {
							resumeUnwrapped, resumeMissingStructuredOutput, _ := selectProviderSchemaValue(retryValue, retryResult.stdout)
							if !resumeMissingStructuredOutput {
								response.Value = resumeUnwrapped
								response.HasValue = true
								response.Metadata["structuredOutputAttempts"] = attempts
								return response, nil
							}
						}
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

				freshValue, parseErr := agent.ParseStructuredOutput(freshResult.stdout)
				if parseErr != nil {
					return response, &agent.Error{
						Code:    "invalid_provider_output",
						Message: fmt.Sprintf("claude output is invalid: %v", parseErr),
					}
				}

				unwrappedFresh, freshMissingStructuredOutput, _ := selectProviderSchemaValue(freshValue, freshResult.stdout)
				if freshMissingStructuredOutput {
					return response, &agent.Error{
						Code:    "invalid_provider_output",
						Message: "claude output is invalid: provider schema response envelope is missing structured_output",
					}
				}
				response.Value = unwrappedFresh
				response.HasValue = true
				return response, nil
			}
			value = unwrapped
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

func unwrapProviderStructuredOutput(value any) (any, bool, string) {
	envelope, ok := value.(map[string]any)
	if !ok {
		return value, false, ""
	}

	if !looksLikeClaudeJSONEnvelope(envelope) {
		return value, false, ""
	}

	sessionID, _ := envelope["session_id"].(string)
	inner, ok := envelope["structured_output"]
	if !ok {
		return value, true, sessionID
	}
	return inner, false, sessionID
}

func selectProviderSchemaValue(value any, transcript []byte) (any, bool, string) {
	unwrapped, missingStructuredOutput, sessionID := unwrapProviderStructuredOutput(value)
	if missingStructuredOutput {
		return unwrapped, true, sessionID
	}

	// stream-json outputs NDJSON events where the first object is usually a
	// system/init event. Prefer the final type=result envelope when present.
	envelope, ok := findResultEnvelopeFromNDJSON(transcript)
	if !ok {
		return unwrapped, false, sessionID
	}
	return unwrapProviderStructuredOutput(envelope)
}

func findResultEnvelopeFromNDJSON(data []byte) (map[string]any, bool) {
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	// Raise the scanner buffer cap for long JSON lines emitted by providers.
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 4*1024*1024)

	var result map[string]any
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var current map[string]any
		if err := json.Unmarshal([]byte(line), &current); err != nil {
			continue
		}
		if typ, _ := current["type"].(string); typ == "result" {
			result = current
		}
	}
	if result == nil {
		return nil, false
	}
	return result, true
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
