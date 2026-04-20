package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/iw2rmb/amata/internal/executor"
	"github.com/iw2rmb/amata/internal/jsonutil"
	"github.com/iw2rmb/amata/internal/state"
)

type Provider interface {
	Name() string
	Execute(context.Context, Request) (Response, *Error)
}

type Request struct {
	Prompt                string
	Model                 string
	Reasoning             string
	CWD                   string
	Env                   map[string]string
	ArtifactDir           string
	Structured            *StructuredOutput
	ContinuationSessionID string
	// StdoutWriter and StderrWriter are wired to pre-created artifact files
	// before the provider executes. Streaming providers write incrementally;
	// buffered providers leave these unused and return bytes in Response.Stdout
	// and Response.Stderr instead.
	StdoutWriter io.Writer
	StderrWriter io.Writer
}

type StructuredOutput struct {
	Document   any
	JSON       string
	SchemaPath string
}

type Response struct {
	Prompt     string
	Value      any
	HasValue   bool
	Transcript []byte
	Stdout     []byte
	Stderr     []byte
	Metadata   map[string]any
}

type Error struct {
	Code    string
	Message string
	Details map[string]any
}

type Executor struct {
	provider Provider
}

func New(provider Provider) executor.Executor {
	return &Executor{provider: provider}
}

func (e *Executor) Execute(ctx context.Context, stepCtx executor.StepContext) state.StepResult {
	if e.provider == nil {
		return executor.Failed("invalid_executor", fmt.Sprintf("step %d: provider is required", stepCtx.StepIndex))
	}

	stepDir := executor.StepArtifactDir(stepCtx.RunDir, stepCtx.StepIndex, stepCtx.Step.ID, stepCtx.ExecutionLabel)
	if err := os.MkdirAll(stepDir, 0o755); err != nil {
		return executor.Failed("artifact_dir_failed", fmt.Sprintf("step %d: create artifact directory: %v", stepCtx.StepIndex, err))
	}

	request, requestErr := loadRequest(stepCtx, e.provider.Name(), stepDir)
	if requestErr != nil {
		result := executor.Failed(requestErr.Code, requestErr.Message)
		if result.Error != nil && len(requestErr.Details) > 0 {
			result.Error.Details = jsonutil.CloneMap(requestErr.Details)
		}
		return result
	}
	if err := initializePromptArtifact(stepDir, request.Prompt); err != nil {
		return executor.Failed("artifact_capture_failed", fmt.Sprintf("step %d: initialize prompt artifact: %v", stepCtx.StepIndex, err))
	}

	capture, captureOpenErr := OpenStreamCapture(stepDir)
	if captureOpenErr != nil {
		return executor.Failed("artifact_capture_failed", fmt.Sprintf("step %d: open stream capture: %v", stepCtx.StepIndex, captureOpenErr))
	}
	request.StdoutWriter = capture.StdoutWriter()
	request.StderrWriter = capture.StderrWriter()

	response, execErr := e.provider.Execute(ctx, request)

	// Normalize context cancellation/deadline to a stable failure code at the
	// executor boundary, regardless of what error the provider surfaced.
	if ctxErr := ctx.Err(); ctxErr != nil {
		msg := "canceled"
		if errors.Is(ctxErr, context.DeadlineExceeded) {
			msg = "deadline exceeded"
		}
		execErr = &Error{Code: "canceled", Message: msg}
	}

	// Normalize provider process crash to a stable code at the executor
	// boundary. Providers (codex, claude) surface agent_failed for abrupt
	// process termination; callers must not depend on that provider-internal
	// code leaking through.
	if execErr != nil && execErr.Code == "agent_failed" {
		execErr = &Error{
			Code:    "provider_crashed",
			Message: execErr.Message,
			Details: jsonutil.CloneMap(execErr.Details),
		}
	}

	writeErr := capture.Write(response.Stdout, response.Stderr)
	closeErr := capture.Close()

	captureErr := writeErr
	if captureErr == nil {
		captureErr = closeErr
	}

	artifacts, artifactErr := captureArtifacts(stepDir, e.provider.Name(), request, response)
	artifacts.Stdout = capture.StdoutPath()
	artifacts.Stderr = capture.StderrPath()

	if captureErr != nil {
		result := executor.Failed("artifact_capture_failed", fmt.Sprintf("step %d: write stream capture: %v", stepCtx.StepIndex, captureErr))
		result.Artifacts = artifacts
		return result
	}

	if artifactErr != nil {
		result := executor.Failed("artifact_capture_failed", fmt.Sprintf("step %d: capture artifacts: %v", stepCtx.StepIndex, artifactErr))
		result.Artifacts = artifacts
		return result
	}

	value := response.Value
	hasValue := response.HasValue
	if !hasValue && request.Structured == nil {
		value = string(response.Transcript)
		hasValue = true
	}

	result := executor.Succeeded(nil)
	result.Artifacts = artifacts
	if hasValue {
		result.Value = jsonutil.CloneValue(value)
	}

	if execErr != nil {
		result.Status = state.StepStatusFailed
		result.Error = &state.Failure{
			Code:    execErr.Code,
			Message: fmt.Sprintf("step %d: %s", stepCtx.StepIndex, execErr.Message),
			Details: jsonutil.CloneMap(execErr.Details),
		}
	}

	return result
}

func captureArtifacts(stepDir string, providerName string, request Request, response Response) (state.Artifacts, error) {
	artifacts := executor.EmptyArtifacts()

	files := map[string]string{}

	promptText := request.Prompt
	if response.Prompt != "" {
		promptText = response.Prompt
	}
	promptPath := filepath.Join(stepDir, "prompt.md")
	if err := os.WriteFile(promptPath, []byte(promptText), 0o644); err != nil {
		return artifacts, err
	}
	files["prompt"] = promptPath

	transcriptPath := filepath.Join(stepDir, "transcript.txt")
	if err := os.WriteFile(transcriptPath, response.Transcript, 0o644); err != nil {
		return artifacts, err
	}
	files["transcript"] = transcriptPath

	metadataPath := filepath.Join(stepDir, "provider-metadata.json")
	metadata, err := metadataDocument(providerName, request, response.Metadata)
	if err != nil {
		return artifacts, err
	}
	if err := os.WriteFile(metadataPath, metadata, 0o644); err != nil {
		return artifacts, err
	}
	files["metadata"] = metadataPath

	artifacts.Files = files
	return artifacts, nil
}

func initializePromptArtifact(stepDir string, prompt string) error {
	promptPath := filepath.Join(stepDir, "prompt.md")
	return os.WriteFile(promptPath, []byte(prompt), 0o644)
}

func metadataDocument(providerName string, request Request, providerMetadata map[string]any) ([]byte, error) {
	metadata := map[string]any{
		"provider":                  providerName,
		"model":                     request.Model,
		"cwd":                       request.CWD,
		"envKeys":                   sortedEnvKeys(request.Env),
		"structuredOutputRequested": request.Structured != nil,
	}
	if request.Reasoning != "" {
		metadata["reasoning"] = request.Reasoning
	}

	keys := jsonutil.SortedKeys(providerMetadata)
	for _, key := range keys {
		metadata[key] = jsonutil.CloneValue(providerMetadata[key])
	}

	return json.Marshal(metadata)
}

func sortedEnvKeys(values map[string]string) []string {
	if len(values) == 0 {
		return []string{}
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
