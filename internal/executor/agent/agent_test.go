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
	"github.com/iw2rmb/amata/internal/spec"
	"github.com/iw2rmb/amata/internal/state"
	"github.com/iw2rmb/amata/internal/testutil"
	"github.com/iw2rmb/amata/internal/workspace"
)

func TestExecutorResolvesDefaultsTemplatesAndPersistsArtifacts(t *testing.T) {
	t.Parallel()

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

	sc := newStepContext(t, step,
		withDocument(document),
		withParams(map[string]any{"repo": "fixture-repo", "model": "gpt-5.4", "reasoning": "high"}),
	)
	rootDir := sc.Workspace.Root

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

	result := agent.New(provider).Execute(context.Background(), sc)
	if result.Status != state.StepStatusSucceeded {
		t.Fatalf("result status = %q, error = %#v", result.Status, result.Error)
	}
	wantValue := map[string]any{"approved": true, "summary": "done"}
	if !reflect.DeepEqual(result.Value, wantValue) {
		t.Fatalf("value = %#v, want %#v", result.Value, wantValue)
	}

	stepDir := filepath.Join(sc.RunDir, "artifacts", "step-00-shared-agent")
	assertArtifactPaths(t, result.Artifacts, stepDir)

	testutil.AssertFileContents(t, result.Artifacts.Stdout, "codex stdout\n")
	testutil.AssertFileContents(t, result.Artifacts.Stderr, "codex stderr\n")
	testutil.AssertFileContents(t, result.Artifacts.Files["prompt"], "Implement fixture-repo.")
	testutil.AssertFileContents(t, result.Artifacts.Files["transcript"], "{\"approved\":true,\"summary\":\"done\"}\n")

	assertMetadata(t, result.Artifacts.Files["metadata"], map[string]any{
		"provider":                  "codex",
		"model":                     "gpt-5.4",
		"reasoning":                 "high",
		"structuredOutputRequested": true,
	})
}

func TestExecutorClaudeRequestsStructuredOutputEvenWhenResponseFromIsStdout(t *testing.T) {
	t.Parallel()

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
			if request.Structured == nil {
				t.Fatalf("structured output request missing for claude")
			}
			return agent.Response{
				Value:      map[string]any{"approved": true},
				HasValue:   true,
				Transcript: []byte("{\"approved\":true}"),
				Stdout:     []byte("{\"approved\":true}\n"),
			}, nil
		},
	}

	sc := newStepContext(t, step,
		withDocument(documentWithProviderDefaults("claude", "sonnet")),
		withStepIndex(1),
	)
	result := agent.New(provider).Execute(context.Background(), sc)

	if result.Status != state.StepStatusSucceeded {
		t.Fatalf("result status = %q, error = %#v", result.Status, result.Error)
	}
	if !reflect.DeepEqual(result.Value, map[string]any{"approved": true}) {
		t.Fatalf("value = %#v, want approved=true object", result.Value)
	}
	testutil.AssertFileContents(t, result.Artifacts.Stdout, "{\"approved\":true}\n")
}

func TestExecutorClaudeDefaultsStructuredSchemaWhenResponseSchemaMissing(t *testing.T) {
	t.Parallel()

	step := spec.Step{
		ID:   "default-schema",
		Type: "claude",
		Fields: map[string]any{
			"prompt": "Summarize repository",
		},
	}

	provider := &fakeProvider{
		name: "claude",
		execute: func(_ context.Context, request agent.Request) (agent.Response, *agent.Error) {
			if request.Structured == nil {
				t.Fatalf("structured output request missing for claude")
			}
			if !strings.Contains(request.Structured.JSON, `"summary"`) {
				t.Fatalf("structured schema = %q, want summary field", request.Structured.JSON)
			}
			return agent.Response{
				Value:      map[string]any{"summary": "one-liner"},
				HasValue:   true,
				Transcript: []byte("{\"summary\":\"one-liner\"}\n"),
			}, nil
		},
	}

	sc := newStepContext(t, step, withDocument(documentWithProviderDefaults("claude", "sonnet")))
	result := agent.New(provider).Execute(context.Background(), sc)

	if result.Status != state.StepStatusSucceeded {
		t.Fatalf("result status = %q, error = %#v", result.Status, result.Error)
	}
	if !reflect.DeepEqual(result.Value, map[string]any{"summary": "one-liner"}) {
		t.Fatalf("value = %#v, want summary object", result.Value)
	}
}

func TestExecutorUsesSchemaFilePathForCodexStructuredOutput(t *testing.T) {
	t.Parallel()

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

	sc := newStepContext(t, step, withDocument(documentWithProviderDefaults("codex", "gpt-5.4")))
	sc.SpecPath = filepath.Join(sc.Workspace.Root, "workflow.yaml")

	schemaPath := filepath.Join(sc.Workspace.Root, "review_result.schema.json")
	if err := os.WriteFile(schemaPath, []byte(`{"type":"object","required":["approved"],"additionalProperties":false,"properties":{"approved":{"type":"boolean"}}}`), 0o644); err != nil {
		t.Fatalf("write schema file: %v", err)
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

	result := agent.New(provider).Execute(context.Background(), sc)
	if result.Status != state.StepStatusSucceeded {
		t.Fatalf("result status = %q, error = %#v", result.Status, result.Error)
	}
	if !reflect.DeepEqual(result.Value, map[string]any{"approved": true}) {
		t.Fatalf("value = %#v", result.Value)
	}
}

func TestExecutorRejectsUnsupportedCodexStructuredSchemaKeyword(t *testing.T) {
	t.Parallel()

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

	sc := newStepContext(t, step, withDocument(documentWithProviderDefaults("codex", "gpt-5.4")))
	result := agent.New(provider).Execute(context.Background(), sc)

	assertFailedWithCode(t, result, "invalid_response_schema")
	if got := result.Error.Message; !strings.Contains(got, `does not support "allOf"`) {
		t.Fatalf("error message = %q", got)
	}
}

func TestExecutorPersistsProviderAdjustedPrompt(t *testing.T) {
	t.Parallel()

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

	sc := newStepContext(t, step,
		withDocument(documentWithProviderDefaults("claude", "sonnet")),
		withStepIndex(2),
	)
	result := agent.New(provider).Execute(context.Background(), sc)

	if result.Status != state.StepStatusSucceeded {
		t.Fatalf("result status = %q, error = %#v", result.Status, result.Error)
	}
	testutil.AssertFileContents(t, result.Artifacts.Files["prompt"], "Original prompt\n\nReturn only JSON.")
}

func TestExecutorReturnsInvalidProviderPayloadFailureAndPersistsArtifacts(t *testing.T) {
	t.Parallel()

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

	sc := newStepContext(t, step,
		withDocument(documentWithProviderDefaults("codex", "gpt-5.4")),
		withStepIndex(3),
	)
	result := agent.New(provider).Execute(context.Background(), sc)

	assertFailedWithCode(t, result, "invalid_provider_output")
	if got := result.Error.Message; !strings.Contains(got, "structured output does not contain valid JSON") {
		t.Fatalf("result error message = %q, want provider error detail", got)
	}

	stepDir := filepath.Join(sc.RunDir, "artifacts", "step-03-invalid-provider-output")
	assertArtifactPaths(t, result.Artifacts, stepDir)
	testutil.AssertFileContents(t, result.Artifacts.Stdout, "provider stdout\n")
	testutil.AssertFileContents(t, result.Artifacts.Stderr, "provider stderr\n")
	testutil.AssertFileContents(t, result.Artifacts.Files["prompt"], "Return JSON\n\nProvider attempted JSON output.")
	testutil.AssertFileContents(t, result.Artifacts.Files["transcript"], "not-json")

	assertMetadata(t, result.Artifacts.Files["metadata"], map[string]any{
		"provider":                  "codex",
		"structuredOutputRequested": true,
		"structuredOutputMode":      "provider_schema",
	})
}

func TestExecutorFailsWithArtifactCaptureFailed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider string
		model    string
		setup    func(t *testing.T, sc executor.StepContext)
		execute  func(t *testing.T, ctx context.Context, request agent.Request) (agent.Response, *agent.Error)
	}{
		{
			name:     "stream_open_fails",
			provider: "codex",
			model:    "gpt-5.4",
			setup: func(t *testing.T, sc executor.StepContext) {
				t.Helper()
				stepDir := filepath.Join(sc.RunDir, "artifacts", "step-00-capture-fail")
				if err := os.MkdirAll(stepDir, 0o755); err != nil {
					t.Fatalf("setup: %v", err)
				}
				if err := os.Chmod(stepDir, 0o444); err != nil {
					t.Fatalf("setup chmod: %v", err)
				}
				t.Cleanup(func() { os.Chmod(stepDir, 0o755) })
			},
			execute: func(t *testing.T, _ context.Context, _ agent.Request) (agent.Response, *agent.Error) {
				t.Fatalf("provider must not be called when stream open fails")
				return agent.Response{}, nil
			},
		},
		{
			name:     "stream_write_fails",
			provider: "claude",
			model:    "sonnet",
			execute: func(t *testing.T, _ context.Context, request agent.Request) (agent.Response, *agent.Error) {
				f, ok := request.StdoutWriter.(*os.File)
				if !ok {
					t.Fatalf("StdoutWriter is not *os.File")
				}
				f.Close()
				return agent.Response{Stdout: []byte("will fail to write")}, nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			step := spec.Step{
				ID:   "capture-fail",
				Type: tt.provider,
				Fields: map[string]any{
					"prompt": "do something",
				},
			}
			sc := newStepContext(t, step, withDocument(documentWithProviderDefaults(tt.provider, tt.model)))
			if tt.setup != nil {
				tt.setup(t, sc)
			}

			provider := &fakeProvider{
				name: tt.provider,
				execute: func(ctx context.Context, request agent.Request) (agent.Response, *agent.Error) {
					return tt.execute(t, ctx, request)
				},
			}
			result := agent.New(provider).Execute(context.Background(), sc)
			assertFailedWithCode(t, result, "artifact_capture_failed")
		})
	}
}

func TestExecutorNormalizesErrorCodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		provider       string
		model          string
		cancel         bool
		streamPartial  bool
		wantCode       string
		checkArtifacts bool
	}{
		{
			name:           "provider_crash/claude",
			provider:       "claude",
			model:          "sonnet",
			streamPartial:  true,
			wantCode:       "provider_crashed",
			checkArtifacts: true,
		},
		{
			name:     "provider_crash/crush",
			provider: "crush",
			model:    "sonnet-5",
			wantCode: "provider_crashed",
		},
		{
			name:     "cancellation",
			provider: "claude",
			model:    "sonnet",
			cancel:   true,
			wantCode: "canceled",
		},
		{
			name:           "cancellation/partial_artifacts",
			provider:       "claude",
			model:          "sonnet",
			cancel:         true,
			streamPartial:  true,
			wantCode:       "canceled",
			checkArtifacts: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			step := spec.Step{
				ID:   "error-norm",
				Type: tt.provider,
				Fields: map[string]any{
					"prompt": "do something",
				},
			}
			sc := newStepContext(t, step, withDocument(documentWithProviderDefaults(tt.provider, tt.model)))

			provider := &fakeProvider{
				name: tt.provider,
				execute: func(_ context.Context, request agent.Request) (agent.Response, *agent.Error) {
					if tt.streamPartial {
						if _, err := request.StdoutWriter.Write([]byte("partial stdout\n")); err != nil {
							t.Fatalf("write to StdoutWriter: %v", err)
						}
						if _, err := request.StderrWriter.Write([]byte("partial stderr\n")); err != nil {
							t.Fatalf("write to StderrWriter: %v", err)
						}
					}
					if tt.cancel {
						cancel()
					}
					resp := agent.Response{}
					if tt.streamPartial {
						resp.Transcript = []byte("partial")
					}
					return resp, &agent.Error{Code: "agent_failed", Message: "provider error"}
				},
			}

			result := agent.New(provider).Execute(ctx, sc)
			assertFailedWithCode(t, result, tt.wantCode)
			if tt.checkArtifacts {
				testutil.AssertFileContents(t, result.Artifacts.Stdout, "partial stdout\n")
				testutil.AssertFileContents(t, result.Artifacts.Stderr, "partial stderr\n")
			}
		})
	}
}

func TestExecutorResolvesDefaultsAndPersistsArtifactsForCrush(t *testing.T) {
	t.Parallel()

	document := spec.Document{
		Defaults: map[string]any{
			"cwd": "$.workspace.root",
			"executors": map[string]any{
				"crush": map[string]any{
					"model": "$.params.model",
				},
			},
		},
	}
	step := spec.Step{
		ID:   "crush-step",
		Type: "crush",
		Fields: map[string]any{
			"prompt": "Implement {{ ctx.params.task }}.",
			"response": map[string]any{
				"schema": map[string]any{
					"type":                 "object",
					"required":             []any{"done"},
					"additionalProperties": false,
					"properties": map[string]any{
						"done": "boolean",
					},
				},
			},
		},
	}

	sc := newStepContext(t, step,
		withDocument(document),
		withParams(map[string]any{"task": "crush-task", "model": "claude-sonnet-5"}),
	)
	rootDir := sc.Workspace.Root

	provider := &fakeProvider{
		name: "crush",
		execute: func(_ context.Context, request agent.Request) (agent.Response, *agent.Error) {
			if request.Prompt == "" {
				t.Fatalf("prompt must not be empty")
			}
			if !strings.Contains(request.Prompt, "Implement crush-task.") {
				t.Fatalf("prompt = %q, want rendered template", request.Prompt)
			}
			if request.Model != "claude-sonnet-5" {
				t.Fatalf("model = %q, want claude-sonnet-5", request.Model)
			}
			if request.CWD != rootDir {
				t.Fatalf("cwd = %q, want %q", request.CWD, rootDir)
			}
			// crush does not support reasoning — callers must omit it.
			if request.Reasoning != "" {
				t.Fatalf("reasoning = %q, want empty", request.Reasoning)
			}
			// Structured output is requested (crush uses prompt_fallback mode).
			if request.Structured == nil {
				t.Fatalf("structured output request missing")
			}
			return agent.Response{
				Value:      map[string]any{"done": true},
				HasValue:   true,
				Transcript: []byte("{\"done\":true}\n"),
				Stdout:     []byte("crush stdout\n"),
				Stderr:     []byte("crush stderr\n"),
				Metadata: map[string]any{
					"structuredOutputMode": "prompt_fallback",
					"command":              []string{"crush", "run"},
				},
			}, nil
		},
	}

	result := agent.New(provider).Execute(context.Background(), sc)
	if result.Status != state.StepStatusSucceeded {
		t.Fatalf("result status = %q, error = %#v", result.Status, result.Error)
	}
	if !reflect.DeepEqual(result.Value, map[string]any{"done": true}) {
		t.Fatalf("raw result value = %#v", result.Value)
	}

	stepDir := filepath.Join(sc.RunDir, "artifacts", "step-00-crush-step")
	assertArtifactPaths(t, result.Artifacts, stepDir)

	testutil.AssertFileContents(t, result.Artifacts.Stdout, "crush stdout\n")
	testutil.AssertFileContents(t, result.Artifacts.Stderr, "crush stderr\n")
	testutil.AssertFileContents(t, result.Artifacts.Files["transcript"], "{\"done\":true}\n")

	metadata := assertMetadata(t, result.Artifacts.Files["metadata"], map[string]any{
		"provider":                  "crush",
		"model":                     "claude-sonnet-5",
		"structuredOutputRequested": true,
		"structuredOutputMode":      "prompt_fallback",
	})
	// reasoning must be absent (crush does not support it).
	if r, ok := metadata["reasoning"]; ok && r != "" && r != nil {
		t.Fatalf("metadata reasoning = %#v, want absent or empty", r)
	}
}

// --- Test helpers ---

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

type stepContextOption func(*testing.T, *executor.StepContext)

func withDocument(d spec.Document) stepContextOption {
	return func(_ *testing.T, sc *executor.StepContext) { sc.Spec = d }
}

func withParams(p map[string]any) stepContextOption {
	return func(_ *testing.T, sc *executor.StepContext) {
		sc.Runtime = runtimeForWorkspace(sc.Workspace, p)
	}
}

func withStepIndex(i int) stepContextOption {
	return func(_ *testing.T, sc *executor.StepContext) { sc.StepIndex = i }
}

func newStepContext(t *testing.T, step spec.Step, opts ...stepContextOption) executor.StepContext {
	t.Helper()
	rootDir := t.TempDir()
	ws := workspace.Config{
		Root:     rootDir,
		StateDir: filepath.Join(rootDir, ".amata"),
	}
	sc := executor.StepContext{
		RunDir:    filepath.Join(rootDir, ".amata", "runs", "run-"+step.ID),
		Workspace: ws,
		Step:      step,
		Runtime:   runtimeForWorkspace(ws, nil),
	}
	for _, opt := range opts {
		opt(t, &sc)
	}
	return sc
}

func documentWithProviderDefaults(provider, model string) spec.Document {
	return spec.Document{
		Defaults: map[string]any{
			"executors": map[string]any{
				provider: map[string]any{
					"model": model,
				},
			},
		},
	}
}

func assertFailedWithCode(t *testing.T, result state.StepResult, code string) {
	t.Helper()
	if result.Status != state.StepStatusFailed {
		t.Fatalf("result status = %q, want failed", result.Status)
	}
	if result.Error == nil || result.Error.Code != code {
		t.Fatalf("result error = %#v, want code=%s", result.Error, code)
	}
}

func assertArtifactPaths(t *testing.T, artifacts state.Artifacts, stepDir string) {
	t.Helper()

	testutil.AssertPathPrefix(t, artifacts.Stdout, stepDir)
	testutil.AssertPathPrefix(t, artifacts.Stderr, stepDir)
	for _, key := range []string{"prompt", "transcript", "metadata"} {
		testutil.AssertPathPrefix(t, artifacts.Files[key], stepDir)
	}
}

func assertMetadata(t *testing.T, path string, want map[string]any) map[string]any {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	for key, wantVal := range want {
		if got[key] != wantVal {
			t.Fatalf("metadata[%q] = %#v, want %#v", key, got[key], wantVal)
		}
	}
	return got
}
