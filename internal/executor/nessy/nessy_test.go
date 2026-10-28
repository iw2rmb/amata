package nessy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"slices"
	"testing"

	"github.com/iw2rmb/amata/internal/executor/agent"
)

func TestProviderInvokesNessyAndCapturesOutput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		model      string
		session    string
		structured bool
		output     string
		runErr     error
		wantCode   string
	}{
		{name: "configured default model", output: "done\n"},
		{name: "continued session", session: "previous-session", output: "done\n"},
		{name: "explicit model", model: "test-model", output: "done\n"},
		{name: "structured JSON", structured: true, output: "{\"approved\":true}\n"},
		{name: "invalid structured output", structured: true, output: "not JSON", wantCode: "invalid_provider_output"},
		{name: "process failure preserves output", output: "partial\n", runErr: errors.New("exit status 1"), wantCode: "agent_failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			request := agent.Request{
				Prompt:                "--a prompt with spaces, 'quotes', and\nnewlines",
				ContinuationSessionID: tt.session,
				Model:                 tt.model, CWD: "/workspace",
				Env:          map[string]string{"NESSY_TEST": "value"},
				StdoutWriter: &stdout, StderrWriter: &stderr,
			}
			wantPrompt := request.Prompt
			if tt.structured {
				request.Structured = &agent.StructuredOutput{JSON: `{"type":"object","properties":{"approved":{"type":"boolean"}}}`}
				wantPrompt = agent.StructuredPrompt(wantPrompt, request.Structured.JSON)
			}
			wantArgs := []string{"--auth-type", "openai", "--chat-recording", "true", "--approval-mode", "auto-edit"}
			if tt.model != "" {
				wantArgs = append(wantArgs, "--model", tt.model)
			}

			called := false
			p := provider{run: func(_ context.Context, args []string, dir string, env []string, stdin []byte, out, errOut io.Writer) error {
				called = true
				sessionFlag := "--session-id"
				if tt.session != "" {
					sessionFlag = "--resume"
				}
				if len(args) < 8 || args[6] != sessionFlag || args[7] == "" || (tt.session != "" && args[7] != tt.session) {
					t.Fatalf("session args = %q", args)
				}
				wantArgs = append(append(append([]string{}, wantArgs[:6]...), sessionFlag, args[7]), wantArgs[6:]...)
				if string(stdin) != wantPrompt {
					t.Fatalf("stdin = %q, want %q", stdin, wantPrompt)
				}
				if !reflect.DeepEqual(args, wantArgs) || dir != request.CWD || !slices.Contains(env, "NESSY_TEST=value") {
					t.Fatalf("invocation: args=%q dir=%q env override present=%t", args, dir, slices.Contains(env, "NESSY_TEST=value"))
				}
				_, _ = io.WriteString(out, tt.output)
				_, _ = io.WriteString(errOut, "diagnostic\n")
				return tt.runErr
			}}
			response, err := p.Execute(context.Background(), request)
			if !called {
				t.Fatal("Nessy was not invoked")
			}
			if (err == nil) != (tt.wantCode == "") || (err != nil && err.Code != tt.wantCode) {
				t.Fatalf("error = %#v, want code %q", err, tt.wantCode)
			}
			if string(response.Transcript) != tt.output || stdout.String() != tt.output || stderr.String() != "diagnostic\n" {
				t.Fatalf("output was not captured: response=%#v stdout=%q stderr=%q", response, stdout.String(), stderr.String())
			}
			if response.Metadata["continuation_session_id"] == "" {
				t.Fatal("missing continuation session")
			}
			if response.Prompt != wantPrompt || !reflect.DeepEqual(response.Metadata["command"], append([]string{"nessy"}, wantArgs...)) {
				t.Fatalf("invocation metadata = %#v", response)
			}
			if tt.structured && tt.wantCode == "" {
				if !response.HasValue || !reflect.DeepEqual(response.Value, map[string]any{"approved": true}) || response.Metadata["structuredOutputMode"] != "prompt_fallback" {
					t.Fatalf("structured response = %#v", response)
				}
			}
		})
	}
}

func TestProviderRejectsReasoningBeforeInvocation(t *testing.T) {
	t.Parallel()
	p := provider{run: func(context.Context, []string, string, []string, []byte, io.Writer, io.Writer) error {
		t.Fatal("unsupported reasoning must not invoke Nessy")
		return nil
	}}
	_, err := p.Execute(context.Background(), agent.Request{Prompt: "task", Reasoning: "high"})
	if err == nil || err.Code != "unsupported_option" {
		t.Fatalf("error = %#v, want unsupported_option", err)
	}
}
