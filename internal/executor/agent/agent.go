package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"auto/internal/executor"
	"auto/internal/jsonutil"
	"auto/internal/state"
)

type Provider interface {
	Name() string
	Execute(context.Context, Request) (Response, *Error)
}

type Request struct {
	Prompt      string
	Model       string
	Reasoning   string
	CWD         string
	Env         map[string]string
	ArtifactDir string
	Structured  *StructuredOutput
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
		return executor.Failed(requestErr.Code, fmt.Sprintf("step %d: %s", stepCtx.StepIndex, requestErr.Message))
	}

	response, execErr := e.provider.Execute(ctx, request)

	artifacts, artifactErr := captureArtifacts(stepDir, e.provider.Name(), request, response)
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
		}
	}

	return result
}

func captureArtifacts(stepDir string, providerName string, request Request, response Response) (state.Artifacts, error) {
	artifacts := executor.EmptyArtifacts()

	stdoutPath := filepath.Join(stepDir, "stdout.txt")
	if err := os.WriteFile(stdoutPath, response.Stdout, 0o644); err != nil {
		return artifacts, err
	}
	artifacts.Stdout = stdoutPath

	stderrPath := filepath.Join(stepDir, "stderr.txt")
	if err := os.WriteFile(stderrPath, response.Stderr, 0o644); err != nil {
		return artifacts, err
	}
	artifacts.Stderr = stderrPath

	files := map[string]string{}

	promptText := request.Prompt
	if response.Prompt != "" {
		promptText = response.Prompt
	}
	promptPath := filepath.Join(stepDir, "prompt.txt")
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
