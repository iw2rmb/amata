package agent_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/iw2rmb/amata/internal/executor"
	"github.com/iw2rmb/amata/internal/executor/agent"
	exprruntime "github.com/iw2rmb/amata/internal/expr"
	"github.com/iw2rmb/amata/internal/runtime/response"
	"github.com/iw2rmb/amata/internal/schema"
	"github.com/iw2rmb/amata/internal/spec"
	"github.com/iw2rmb/amata/internal/state"
	"github.com/iw2rmb/amata/internal/workspace"
)

func TestExecutorResolvesDefaultsTemplatesAndPersistsArtifacts(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	runDir := filepath.Join(rootDir, ".amata", "runs", "run-01")
	workspaceConfig := workspace.Config{
		Root:     rootDir,
		StateDir: filepath.Join(rootDir, ".amata"),
	}
	document := spec.Document{
		Defaults: map[string]any{
			"cwd": "$.workspace.root",
			"env": map[string]any{
				"GLOBAL": "{{ ctx.params.repo }}",
			},
			"executors": map[string]any{
				"codex": map[string]any{
					"model": "$.params.model",
					"env": map[string]any{
						"PROVIDER": "{{ ctx.params.repo }}",
					},
				},
			},
		},
		Schemas: map[string]any{
			"result": map[string]any{
				"type":                 "object",
				"required":             []any{"approved", "summary"},
				"additionalProperties": false,
				"properties": map[string]any{
					"approved": "boolean",
					"summary":  "string",
				},
			},
		},
	}
	step := spec.Step{
		ID:   "shared-agent",
		Type: "codex",
		Fields: map[string]any{
			"prompt":    "Implement {{ ctx.params.repo }}.",
			"reasoning": "$.params.reasoning",
			"env": map[string]any{
				"STEP": "{{ ctx.params.repo }}",
			},
			"response": map[string]any{
				"schema": map[string]any{
					"$ref": "#/schemas/result",
				},
			},
		},
	}

	provider := &fakeProvider{
		name: "codex",
		execute: func(_ context.Context, request agent.Request) (agent.Response, *agent.Error) {
			if request.Prompt != "Implement fixture-repo." {
				t.Fatalf("prompt = %q, want rendered template", request.Prompt)
			}
			if request.Model != "gpt-5.4" {
				t.Fatalf("model = %q, want gpt-5.4", request.Model)
			}
			if request.Reasoning != "high" {
				t.Fatalf("reasoning = %q, want high", request.Reasoning)
			}
			if request.CWD != rootDir {
				t.Fatalf("cwd = %q, want %q", request.CWD, rootDir)
			}

			wantEnv := map[string]string{
				"GLOBAL":   "fixture-repo",
				"PROVIDER": "fixture-repo",
				"STEP":     "fixture-repo",
			}
			if !reflect.DeepEqual(request.Env, wantEnv) {
				t.Fatalf("env = %#v, want %#v", request.Env, wantEnv)
			}
			if request.Structured == nil {
				t.Fatalf("structured output request missing")
			}

			schemaFile, err := os.ReadFile(request.Structured.SchemaPath)
			if err != nil {
				t.Fatalf("read schema file: %v", err)
			}
			var schemaDocument map[string]any
			if err := json.Unmarshal(schemaFile, &schemaDocument); err != nil {
				t.Fatalf("decode schema file: %v", err)
			}
			if schemaDocument["type"] != "object" {
				t.Fatalf("schema type = %#v, want object", schemaDocument["type"])
			}
			if _, ok := schemaDocument["$ref"]; ok {
				t.Fatalf("schema artifact kept top-level $ref: %s", string(schemaFile))
			}
			defs, ok := schemaDocument["$defs"].(map[string]any)
			if !ok {
				t.Fatalf("schema artifact missing $defs: %s", string(schemaFile))
			}
			if _, ok := defs["workflow:result"]; !ok {
				t.Fatalf("schema artifact missing workflow:result definition: %s", string(schemaFile))
			}

			return agent.Response{
				Value:      map[string]any{"approved": true, "summary": "done"},
				HasValue:   true,
				Transcript: []byte("{\"approved\":true,\"summary\":\"done\"}\n"),
				Stdout:     []byte("codex stdout\n"),
				Stderr:     []byte("codex stderr\n"),
				Metadata: map[string]any{
					"structuredOutputMode": "provider_schema",
					"command":              []string{"codex", "exec"},
				},
			}, nil
		},
	}

	result := agent.New(provider).Execute(context.Background(), executor.StepContext{
		RunDir:    runDir,
		Spec:      document,
		Workspace: workspaceConfig,
		StepIndex: 0,
		Step:      step,
		Runtime:   runtimeForWorkspace(workspaceConfig, map[string]any{"repo": "fixture-repo", "model": "gpt-5.4", "reasoning": "high"}),
	})
	if result.Status != state.StepStatusSucceeded {
		t.Fatalf("result status = %q, error = %#v", result.Status, result.Error)
	}
	if !reflect.DeepEqual(result.Value, map[string]any{"approved": true, "summary": "done"}) {
		t.Fatalf("raw result value = %#v", result.Value)
	}

	resolved, failure := response.NewResolver(schema.NewRegistry(document.Schemas)).Apply(0, "", step, result)
	if failure != nil {
		t.Fatalf("response failure = %#v", failure)
	}

	wantValue := map[string]any{"approved": true, "summary": "done"}
	if !reflect.DeepEqual(resolved.Value, wantValue) {
		t.Fatalf("value = %#v, want %#v", resolved.Value, wantValue)
	}
	assertArtifactPathPrefix(t, resolved.Artifacts.Stdout, filepath.Join(runDir, "artifacts", "step-00-shared-agent"))
	assertArtifactPathPrefix(t, resolved.Artifacts.Stderr, filepath.Join(runDir, "artifacts", "step-00-shared-agent"))
	assertArtifactPathPrefix(t, resolved.Artifacts.Files["prompt"], filepath.Join(runDir, "artifacts", "step-00-shared-agent"))
	assertArtifactPathPrefix(t, resolved.Artifacts.Files["transcript"], filepath.Join(runDir, "artifacts", "step-00-shared-agent"))
	assertArtifactPathPrefix(t, resolved.Artifacts.Files["metadata"], filepath.Join(runDir, "artifacts", "step-00-shared-agent"))

	assertArtifactContents(t, resolved.Artifacts.Stdout, "codex stdout\n")
	assertArtifactContents(t, resolved.Artifacts.Stderr, "codex stderr\n")
	assertArtifactContents(t, resolved.Artifacts.Files["prompt"], "Implement fixture-repo.")
	assertArtifactContents(t, resolved.Artifacts.Files["transcript"], "{\"approved\":true,\"summary\":\"done\"}\n")

	metadataFile, err := os.ReadFile(resolved.Artifacts.Files["metadata"])
	if err != nil {
		t.Fatalf("read metadata artifact: %v", err)
	}
	var metadata map[string]any
	if err := json.Unmarshal(metadataFile, &metadata); err != nil {
		t.Fatalf("decode metadata artifact: %v", err)
	}
	if metadata["provider"] != "codex" {
		t.Fatalf("metadata provider = %#v, want codex", metadata["provider"])
	}
	if metadata["model"] != "gpt-5.4" {
		t.Fatalf("metadata model = %#v, want gpt-5.4", metadata["model"])
	}
	if metadata["reasoning"] != "high" {
		t.Fatalf("metadata reasoning = %#v, want high", metadata["reasoning"])
	}
	if metadata["structuredOutputRequested"] != true {
		t.Fatalf("metadata structuredOutputRequested = %#v, want true", metadata["structuredOutputRequested"])
	}
}

func TestExecutorOnlyRequestsStructuredOutputForResponseValue(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	document := spec.Document{
		Defaults: map[string]any{
			"executors": map[string]any{
				"claude": map[string]any{
					"model": "sonnet",
				},
			},
		},
	}
	step := spec.Step{
		ID:   "stdout-json",
		Type: "claude",
		Fields: map[string]any{
			"prompt": "Return review",
			"response": map[string]any{
				"from": "stdout",
				"schema": map[string]any{
					"type":                 "object",
					"required":             []any{"approved"},
					"additionalProperties": false,
					"properties": map[string]any{
						"approved": "boolean",
					},
				},
			},
		},
	}

	provider := &fakeProvider{
		name: "claude",
		execute: func(_ context.Context, request agent.Request) (agent.Response, *agent.Error) {
			if request.Structured != nil {
				t.Fatalf("structured output request should be disabled when response.from = stdout")
			}
			return agent.Response{
				Transcript: []byte("free-form transcript"),
				Stdout:     []byte("{\"approved\":true}"),
			}, nil
		},
	}

	result := agent.New(provider).Execute(context.Background(), executor.StepContext{
		RunDir: filepath.Join(rootDir, ".amata", "runs", "run-02"),
		Spec:   document,
		Workspace: workspace.Config{
			Root:     rootDir,
			StateDir: filepath.Join(rootDir, ".amata"),
		},
		StepIndex: 1,
		Step:      step,
		Runtime: runtimeForWorkspace(workspace.Config{
			Root:     rootDir,
			StateDir: filepath.Join(rootDir, ".amata"),
		}, nil),
	})

	resolved, failure := response.NewResolver(nil).Apply(1, "", step, result)
	if failure != nil {
		t.Fatalf("response failure = %#v", failure)
	}

	wantValue := map[string]any{"approved": true}
	if !reflect.DeepEqual(resolved.Value, wantValue) {
		t.Fatalf("value = %#v, want %#v", resolved.Value, wantValue)
	}
}

func TestExecutorUsesSchemaFilePathForCodexStructuredOutput(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	specPath := filepath.Join(rootDir, "workflow.yaml")
	schemaPath := filepath.Join(rootDir, "review_result.schema.json")
	if err := os.WriteFile(schemaPath, []byte(`{"type":"object","required":["approved"],"additionalProperties":false,"properties":{"approved":{"type":"boolean"}}}`), 0o644); err != nil {
		t.Fatalf("write schema file: %v", err)
	}

	workspaceConfig := workspace.Config{
		Root:     rootDir,
		StateDir: filepath.Join(rootDir, ".amata"),
	}
	step := spec.Step{
		ID:   "schema-path",
		Type: "codex",
		Fields: map[string]any{
			"prompt": "Return review",
			"response": map[string]any{
				"schema": "./review_result.schema.json",
			},
		},
	}

	provider := &fakeProvider{
		name: "codex",
		execute: func(_ context.Context, request agent.Request) (agent.Response, *agent.Error) {
			if request.Structured == nil {
				t.Fatalf("structured output request missing")
			}
			if request.Structured.SchemaPath != schemaPath {
				t.Fatalf("schema path = %q, want %q", request.Structured.SchemaPath, schemaPath)
			}
			if !strings.Contains(request.Structured.JSON, `"approved"`) {
				t.Fatalf("schema json = %q, want schema file content", request.Structured.JSON)
			}
			return agent.Response{
				Value:      map[string]any{"approved": true},
				HasValue:   true,
				Transcript: []byte("{\"approved\":true}\n"),
			}, nil
		},
	}

	result := agent.New(provider).Execute(context.Background(), executor.StepContext{
		RunDir:   filepath.Join(rootDir, ".amata", "runs", "run-path"),
		SpecPath: specPath,
		Spec: spec.Document{
			Defaults: map[string]any{
				"executors": map[string]any{
					"codex": map[string]any{
						"model": "gpt-5.4",
					},
				},
			},
		},
		Workspace: workspaceConfig,
		StepIndex: 0,
		Step:      step,
		Runtime:   runtimeForWorkspace(workspaceConfig, nil),
	})
	if result.Status != state.StepStatusSucceeded {
		t.Fatalf("result status = %q, error = %#v", result.Status, result.Error)
	}

	resolved, failure := response.NewResolver(nil).Apply(0, specPath, step, result)
	if failure != nil {
		t.Fatalf("response failure = %#v", failure)
	}
	if !reflect.DeepEqual(resolved.Value, map[string]any{"approved": true}) {
		t.Fatalf("value = %#v", resolved.Value)
	}
}

func TestExecutorRejectsUnsupportedCodexStructuredSchemaKeyword(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	workspaceConfig := workspace.Config{
		Root:     rootDir,
		StateDir: filepath.Join(rootDir, ".amata"),
	}
	step := spec.Step{
		ID:   "invalid-schema",
		Type: "codex",
		Fields: map[string]any{
			"prompt": "Return review",
			"response": map[string]any{
				"schema": map[string]any{
					"type": "object",
					"allOf": []any{
						map[string]any{
							"required": []any{"approved"},
						},
					},
				},
			},
		},
	}

	provider := &fakeProvider{
		name: "codex",
		execute: func(_ context.Context, request agent.Request) (agent.Response, *agent.Error) {
			t.Fatalf("provider should not be called for unsupported schema")
			return agent.Response{}, nil
		},
	}

	result := agent.New(provider).Execute(context.Background(), executor.StepContext{
		RunDir: filepath.Join(rootDir, ".amata", "runs", "run-invalid"),
		Spec: spec.Document{
			Defaults: map[string]any{
				"executors": map[string]any{
					"codex": map[string]any{
						"model": "gpt-5.4",
					},
				},
			},
		},
		Workspace: workspaceConfig,
		StepIndex: 0,
		Step:      step,
		Runtime:   runtimeForWorkspace(workspaceConfig, nil),
	})
	if result.Status != state.StepStatusFailed {
		t.Fatalf("result status = %q, want failed", result.Status)
	}
	if result.Error == nil {
		t.Fatalf("expected result error")
	}
	if result.Error.Code != "invalid_response_schema" {
		t.Fatalf("error code = %q, want invalid_response_schema", result.Error.Code)
	}
	if got := result.Error.Message; !strings.Contains(got, `does not support "allOf"`) {
		t.Fatalf("error message = %q", got)
	}
}

func TestExecutorPersistsProviderAdjustedPrompt(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	workspaceConfig := workspace.Config{
		Root:     rootDir,
		StateDir: filepath.Join(rootDir, ".amata"),
	}
	step := spec.Step{
		ID:   "prompt-adjusted",
		Type: "claude",
		Fields: map[string]any{
			"prompt": "Original prompt",
		},
	}

	provider := &fakeProvider{
		name: "claude",
		execute: func(_ context.Context, request agent.Request) (agent.Response, *agent.Error) {
			return agent.Response{
				Prompt:     request.Prompt + "\n\nReturn only JSON.",
				Transcript: []byte("done"),
				Stdout:     []byte("done"),
			}, nil
		},
	}

	result := agent.New(provider).Execute(context.Background(), executor.StepContext{
		RunDir: filepath.Join(rootDir, ".amata", "runs", "run-03"),
		Spec: spec.Document{
			Defaults: map[string]any{
				"executors": map[string]any{
					"claude": map[string]any{
						"model": "sonnet",
					},
				},
			},
		},
		Workspace: workspaceConfig,
		StepIndex: 2,
		Step:      step,
		Runtime:   runtimeForWorkspace(workspaceConfig, nil),
	})

	if result.Status != state.StepStatusSucceeded {
		t.Fatalf("result status = %q, error = %#v", result.Status, result.Error)
	}
	assertArtifactContents(t, result.Artifacts.Files["prompt"], "Original prompt\n\nReturn only JSON.")
}

func TestExecutorReturnsInvalidProviderPayloadFailureAndPersistsArtifacts(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	runDir := filepath.Join(rootDir, ".amata", "runs", "run-04")
	workspaceConfig := workspace.Config{
		Root:     rootDir,
		StateDir: filepath.Join(rootDir, ".amata"),
	}
	step := spec.Step{
		ID:   "invalid-provider-output",
		Type: "codex",
		Fields: map[string]any{
			"prompt": "Return JSON",
			"response": map[string]any{
				"schema": map[string]any{
					"type":                 "object",
					"required":             []any{"approved"},
					"additionalProperties": false,
					"properties": map[string]any{
						"approved": "boolean",
					},
				},
			},
		},
	}

	provider := &fakeProvider{
		name: "codex",
		execute: func(_ context.Context, request agent.Request) (agent.Response, *agent.Error) {
			if request.Structured == nil {
				t.Fatalf("structured output request missing")
			}
			return agent.Response{
					Prompt:     request.Prompt + "\n\nProvider attempted JSON output.",
					Transcript: []byte("not-json"),
					Stdout:     []byte("provider stdout\n"),
					Stderr:     []byte("provider stderr\n"),
					Metadata: map[string]any{
						"structuredOutputMode": "provider_schema",
					},
				}, &agent.Error{
					Code:    "invalid_provider_output",
					Message: "structured output does not contain valid JSON",
				}
		},
	}

	result := agent.New(provider).Execute(context.Background(), executor.StepContext{
		RunDir: runDir,
		Spec: spec.Document{
			Defaults: map[string]any{
				"executors": map[string]any{
					"codex": map[string]any{
						"model": "gpt-5.4",
					},
				},
			},
		},
		Workspace: workspaceConfig,
		StepIndex: 3,
		Step:      step,
		Runtime:   runtimeForWorkspace(workspaceConfig, nil),
	})

	if result.Status != state.StepStatusFailed {
		t.Fatalf("result status = %q, want failed", result.Status)
	}
	if result.Error == nil || result.Error.Code != "invalid_provider_output" {
		t.Fatalf("result error = %#v, want invalid_provider_output", result.Error)
	}
	if got := result.Error.Message; !strings.Contains(got, "structured output does not contain valid JSON") {
		t.Fatalf("result error message = %q, want provider error detail", got)
	}

	stepDir := filepath.Join(runDir, "artifacts", "step-03-invalid-provider-output")
	assertArtifactPathPrefix(t, result.Artifacts.Stdout, stepDir)
	assertArtifactPathPrefix(t, result.Artifacts.Stderr, stepDir)
	assertArtifactPathPrefix(t, result.Artifacts.Files["prompt"], stepDir)
	assertArtifactPathPrefix(t, result.Artifacts.Files["transcript"], stepDir)
	assertArtifactPathPrefix(t, result.Artifacts.Files["metadata"], stepDir)
	assertArtifactContents(t, result.Artifacts.Stdout, "provider stdout\n")
	assertArtifactContents(t, result.Artifacts.Stderr, "provider stderr\n")
	assertArtifactContents(t, result.Artifacts.Files["prompt"], "Return JSON\n\nProvider attempted JSON output.")
	assertArtifactContents(t, result.Artifacts.Files["transcript"], "not-json")

	metadataFile, err := os.ReadFile(result.Artifacts.Files["metadata"])
	if err != nil {
		t.Fatalf("read metadata artifact: %v", err)
	}
	var metadata map[string]any
	if err := json.Unmarshal(metadataFile, &metadata); err != nil {
		t.Fatalf("decode metadata artifact: %v", err)
	}
	if metadata["provider"] != "codex" {
		t.Fatalf("metadata provider = %#v, want codex", metadata["provider"])
	}
	if metadata["structuredOutputRequested"] != true {
		t.Fatalf("metadata structuredOutputRequested = %#v, want true", metadata["structuredOutputRequested"])
	}
	if metadata["structuredOutputMode"] != "provider_schema" {
		t.Fatalf("metadata structuredOutputMode = %#v, want provider_schema", metadata["structuredOutputMode"])
	}
}

func TestExecutorFailsWithArtifactCaptureFailedWhenStreamOpenFails(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	runDir := filepath.Join(rootDir, ".amata", "runs", "run-capture-open-fail")
	workspaceConfig := workspace.Config{
		Root:     rootDir,
		StateDir: filepath.Join(rootDir, ".amata"),
	}
	step := spec.Step{
		ID:   "capture-open-fail",
		Type: "claude",
		Fields: map[string]any{
			"prompt": "do something",
		},
	}

	// Pre-create the stepDir as read-only so MkdirAll succeeds (dir already
	// exists) but OpenStreamCapture cannot create files inside it.
	stepDir := filepath.Join(runDir, "artifacts", "step-00-capture-open-fail")
	if err := os.MkdirAll(stepDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Chmod(stepDir, 0o444); err != nil {
		t.Fatalf("setup chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(stepDir, 0o755) })

	provider := &fakeProvider{
		name: "claude",
		execute: func(_ context.Context, _ agent.Request) (agent.Response, *agent.Error) {
			t.Fatalf("provider must not be called when stream open fails")
			return agent.Response{}, nil
		},
	}

	result := agent.New(provider).Execute(context.Background(), executor.StepContext{
		RunDir: runDir,
		Spec: spec.Document{
			Defaults: map[string]any{
				"executors": map[string]any{
					"claude": map[string]any{"model": "sonnet"},
				},
			},
		},
		Workspace: workspaceConfig,
		StepIndex: 0,
		Step:      step,
		Runtime:   runtimeForWorkspace(workspaceConfig, nil),
	})

	if result.Status != state.StepStatusFailed {
		t.Fatalf("result status = %q, want failed", result.Status)
	}
	if result.Error == nil || result.Error.Code != "artifact_capture_failed" {
		t.Fatalf("result error = %#v, want artifact_capture_failed", result.Error)
	}
}

func TestExecutorFailsWithArtifactCaptureFailedWhenStreamWriteFails(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	runDir := filepath.Join(rootDir, ".amata", "runs", "run-capture-write-fail")
	workspaceConfig := workspace.Config{
		Root:     rootDir,
		StateDir: filepath.Join(rootDir, ".amata"),
	}
	step := spec.Step{
		ID:   "capture-write-fail",
		Type: "claude",
		Fields: map[string]any{
			"prompt": "do something",
		},
	}

	provider := &fakeProvider{
		name: "claude",
		execute: func(_ context.Context, request agent.Request) (agent.Response, *agent.Error) {
			// Close the underlying file descriptor to force a write failure when
			// the executor calls capture.Write(response.Stdout, ...) after Execute returns.
			f, ok := request.StdoutWriter.(*os.File)
			if !ok {
				t.Fatalf("StdoutWriter is not *os.File")
			}
			f.Close()
			return agent.Response{
				Stdout: []byte("will fail to write"),
			}, nil
		},
	}

	result := agent.New(provider).Execute(context.Background(), executor.StepContext{
		RunDir: runDir,
		Spec: spec.Document{
			Defaults: map[string]any{
				"executors": map[string]any{
					"claude": map[string]any{"model": "sonnet"},
				},
			},
		},
		Workspace: workspaceConfig,
		StepIndex: 0,
		Step:      step,
		Runtime:   runtimeForWorkspace(workspaceConfig, nil),
	})

	if result.Status != state.StepStatusFailed {
		t.Fatalf("result status = %q, want failed", result.Status)
	}
	if result.Error == nil || result.Error.Code != "artifact_capture_failed" {
		t.Fatalf("result error = %#v, want artifact_capture_failed", result.Error)
	}
}

func TestExecutorPreservesStreamWriterContentOnProviderError(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	runDir := filepath.Join(rootDir, ".amata", "runs", "run-partial-stream")
	workspaceConfig := workspace.Config{
		Root:     rootDir,
		StateDir: filepath.Join(rootDir, ".amata"),
	}
	step := spec.Step{
		ID:   "partial-stream",
		Type: "claude",
		Fields: map[string]any{
			"prompt": "do something",
		},
	}

	provider := &fakeProvider{
		name: "claude",
		execute: func(_ context.Context, request agent.Request) (agent.Response, *agent.Error) {
			// Simulate a streaming provider: write directly to artifact writers
			// before returning an error (e.g. provider crash mid-execution).
			if _, err := request.StdoutWriter.Write([]byte("partial output\n")); err != nil {
				t.Fatalf("write to StdoutWriter: %v", err)
			}
			if _, err := request.StderrWriter.Write([]byte("partial error\n")); err != nil {
				t.Fatalf("write to StderrWriter: %v", err)
			}
			// Return agent_failed, which is what real providers (codex, claude)
			// surface for process-level crashes. The executor boundary must
			// normalize this to provider_crashed so callers see a stable code.
			return agent.Response{
				Transcript: []byte("partial"),
				// Stdout/Stderr are empty: content was streamed via writers.
			}, &agent.Error{
				Code:    "agent_failed",
				Message: "provider crashed mid-execution",
			}
		},
	}

	result := agent.New(provider).Execute(context.Background(), executor.StepContext{
		RunDir: runDir,
		Spec: spec.Document{
			Defaults: map[string]any{
				"executors": map[string]any{
					"claude": map[string]any{"model": "sonnet"},
				},
			},
		},
		Workspace: workspaceConfig,
		StepIndex: 0,
		Step:      step,
		Runtime:   runtimeForWorkspace(workspaceConfig, nil),
	})

	if result.Status != state.StepStatusFailed {
		t.Fatalf("result status = %q, want failed", result.Status)
	}
	if result.Error == nil || result.Error.Code != "provider_crashed" {
		t.Fatalf("result error = %#v, want provider_crashed", result.Error)
	}

	// Content written to both writers before the crash must be persisted.
	assertArtifactContents(t, result.Artifacts.Stdout, "partial output\n")
	assertArtifactContents(t, result.Artifacts.Stderr, "partial error\n")
}

func TestExecutorMapsCancellationToStableCode(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	runDir := filepath.Join(rootDir, ".amata", "runs", "run-cancel-code")
	workspaceConfig := workspace.Config{
		Root:     rootDir,
		StateDir: filepath.Join(rootDir, ".amata"),
	}
	step := spec.Step{
		ID:   "cancel-code",
		Type: "claude",
		Fields: map[string]any{
			"prompt": "do something",
		},
	}

	ctx, cancel := context.WithCancel(context.Background())

	provider := &fakeProvider{
		name: "claude",
		execute: func(_ context.Context, _ agent.Request) (agent.Response, *agent.Error) {
			cancel()
			// Provider surfaces a different code; executor must override it.
			return agent.Response{}, &agent.Error{Code: "agent_failed", Message: "provider saw cancellation"}
		},
	}

	result := agent.New(provider).Execute(ctx, executor.StepContext{
		RunDir: runDir,
		Spec: spec.Document{
			Defaults: map[string]any{
				"executors": map[string]any{
					"claude": map[string]any{"model": "sonnet"},
				},
			},
		},
		Workspace: workspaceConfig,
		StepIndex: 0,
		Step:      step,
		Runtime:   runtimeForWorkspace(workspaceConfig, nil),
	})

	if result.Status != state.StepStatusFailed {
		t.Fatalf("result status = %q, want failed", result.Status)
	}
	if result.Error == nil || result.Error.Code != "canceled" {
		t.Fatalf("result error = %#v, want code=canceled", result.Error)
	}
}

func TestExecutorPreservesPartialArtifactsOnCancellation(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	runDir := filepath.Join(rootDir, ".amata", "runs", "run-cancel-partial")
	workspaceConfig := workspace.Config{
		Root:     rootDir,
		StateDir: filepath.Join(rootDir, ".amata"),
	}
	step := spec.Step{
		ID:   "cancel-partial",
		Type: "claude",
		Fields: map[string]any{
			"prompt": "do something",
		},
	}

	ctx, cancel := context.WithCancel(context.Background())

	provider := &fakeProvider{
		name: "claude",
		execute: func(_ context.Context, request agent.Request) (agent.Response, *agent.Error) {
			if _, err := request.StdoutWriter.Write([]byte("partial stdout\n")); err != nil {
				t.Fatalf("write to StdoutWriter: %v", err)
			}
			if _, err := request.StderrWriter.Write([]byte("partial stderr\n")); err != nil {
				t.Fatalf("write to StderrWriter: %v", err)
			}
			cancel()
			return agent.Response{
				Transcript: []byte("partial"),
			}, &agent.Error{Code: "agent_failed", Message: "canceled mid-execution"}
		},
	}

	result := agent.New(provider).Execute(ctx, executor.StepContext{
		RunDir: runDir,
		Spec: spec.Document{
			Defaults: map[string]any{
				"executors": map[string]any{
					"claude": map[string]any{"model": "sonnet"},
				},
			},
		},
		Workspace: workspaceConfig,
		StepIndex: 0,
		Step:      step,
		Runtime:   runtimeForWorkspace(workspaceConfig, nil),
	})

	if result.Status != state.StepStatusFailed {
		t.Fatalf("result status = %q, want failed", result.Status)
	}
	if result.Error == nil || result.Error.Code != "canceled" {
		t.Fatalf("result error = %#v, want code=canceled", result.Error)
	}

	// Partial content written to both stream writers must survive cancellation.
	assertArtifactContents(t, result.Artifacts.Stdout, "partial stdout\n")
	assertArtifactContents(t, result.Artifacts.Stderr, "partial stderr\n")
}

func TestExecutorNormalizesProviderCrashForCrush(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	runDir := filepath.Join(rootDir, ".amata", "runs", "run-crush-crash")
	workspaceConfig := workspace.Config{
		Root:     rootDir,
		StateDir: filepath.Join(rootDir, ".amata"),
	}
	step := spec.Step{
		ID:   "crush-crash",
		Type: "crush",
		Fields: map[string]any{
			"prompt": "do something",
		},
	}

	provider := &fakeProvider{
		name: "crush",
		execute: func(_ context.Context, _ agent.Request) (agent.Response, *agent.Error) {
			return agent.Response{}, &agent.Error{Code: "agent_failed", Message: "crush crashed"}
		},
	}

	result := agent.New(provider).Execute(context.Background(), executor.StepContext{
		RunDir: runDir,
		Spec: spec.Document{
			Defaults: map[string]any{
				"executors": map[string]any{
					"crush": map[string]any{"model": "sonnet-5"},
				},
			},
		},
		Workspace: workspaceConfig,
		StepIndex: 0,
		Step:      step,
		Runtime:   runtimeForWorkspace(workspaceConfig, nil),
	})

	if result.Status != state.StepStatusFailed {
		t.Fatalf("result status = %q, want failed", result.Status)
	}
	if result.Error == nil || result.Error.Code != "provider_crashed" {
		t.Fatalf("result error = %#v, want provider_crashed", result.Error)
	}
}

type fakeProvider struct {
	name    string
	execute func(context.Context, agent.Request) (agent.Response, *agent.Error)
}

func (p *fakeProvider) Name() string {
	return p.name
}

func (p *fakeProvider) Execute(ctx context.Context, request agent.Request) (agent.Response, *agent.Error) {
	return p.execute(ctx, request)
}

func runtimeForWorkspace(workspaceConfig workspace.Config, params map[string]any) exprruntime.Runtime {
	return exprruntime.NewRuntime(map[string]any{
		"ctx": map[string]any{
			"workspace": map[string]any{
				"root":      workspaceConfig.Root,
				"state_dir": workspaceConfig.StateDir,
			},
			"params": params,
			"prev":   nil,
		},
	})
}

func assertArtifactContents(t *testing.T, path string, want string) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read artifact %s: %v", path, err)
	}
	if string(data) != want {
		t.Fatalf("artifact %s = %q, want %q", path, string(data), want)
	}
}

func assertArtifactPathPrefix(t *testing.T, path string, wantPrefix string) {
	t.Helper()

	if !strings.HasPrefix(path, wantPrefix+string(os.PathSeparator)) && path != wantPrefix {
		t.Fatalf("artifact path %q, want prefix %q", path, wantPrefix)
	}
}
