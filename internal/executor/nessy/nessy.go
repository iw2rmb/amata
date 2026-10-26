package nessy

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"os/exec"

	"github.com/iw2rmb/amata/internal/executor"
	"github.com/iw2rmb/amata/internal/executor/agent"
)

type runnerFunc func(ctx context.Context, args []string, dir string, env []string, stdin []byte, stdout, stderr io.Writer) error

type provider struct {
	run runnerFunc
}

func New() executor.Executor {
	return agent.New(provider{run: execRun})
}

func (provider) Name() string {
	return "nessy"
}

func (p provider) Execute(ctx context.Context, request agent.Request) (agent.Response, *agent.Error) {
	if request.Reasoning != "" {
		return agent.Response{}, &agent.Error{
			Code:    "unsupported_option",
			Message: "nessy does not support reasoning; remove reasoning from this step",
		}
	}

	prompt := request.Prompt
	structuredOutputMode := ""
	if request.Structured != nil {
		prompt = agent.StructuredPrompt(prompt, request.Structured.JSON)
		structuredOutputMode = "prompt_fallback"
	}

	sessionID := request.ContinuationSessionID
	args := []string{"--auth-type", "openai", "--chat-recording", "true"}
	if sessionID != "" {
		args = append(args, "--resume", sessionID)
	} else {
		var id [16]byte
		_, _ = rand.Read(id[:])
		id[6] = (id[6] & 0x0f) | 0x40
		id[8] = (id[8] & 0x3f) | 0x80
		sessionID = fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
		args = append(args, "--session-id", sessionID)
	}
	if request.Model != "" {
		args = append(args, "--model", request.Model)
	}

	var transcriptBuf bytes.Buffer
	stdoutWriter := io.Writer(&transcriptBuf)
	if request.StdoutWriter != nil {
		stdoutWriter = io.MultiWriter(&transcriptBuf, request.StdoutWriter)
	}

	// Nessy ignores positional text after "--". Stdin also keeps leading
	// dashes and multiline prompts out of its option parser.
	runErr := p.run(ctx, args, request.CWD, agent.CommandEnv(request.Env), []byte(prompt), stdoutWriter, request.StderrWriter)

	response := agent.Response{
		Prompt:     prompt,
		Transcript: transcriptBuf.Bytes(),
		Metadata: map[string]any{
			"command":                 executor.CommandWithBinary("nessy", args),
			"continuation_session_id": sessionID,
		},
	}
	if structuredOutputMode != "" {
		response.Metadata["structuredOutputMode"] = structuredOutputMode
	}

	if runErr != nil {
		return response, &agent.Error{
			Code:    "agent_failed",
			Message: fmt.Sprintf("nessy failed: %v", runErr),
		}
	}

	if request.Structured != nil {
		value, err := agent.ParseStructuredOutput(transcriptBuf.Bytes())
		if err != nil {
			return response, &agent.Error{
				Code:    "invalid_provider_output",
				Message: fmt.Sprintf("nessy output is invalid: %v", err),
			}
		}
		response.Value = value
		response.HasValue = true
	}

	return response, nil
}

func execRun(ctx context.Context, args []string, dir string, env []string, stdin []byte, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, "nessy", args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}
