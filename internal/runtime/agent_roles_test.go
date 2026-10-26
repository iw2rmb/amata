package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	executorapi "github.com/iw2rmb/amata/internal/executor"
	"github.com/iw2rmb/amata/internal/progress"
	"github.com/iw2rmb/amata/internal/spec"
	"github.com/iw2rmb/amata/internal/state"
)

// Candidate retries finish before model continuation; changing agents resets only this step's edits.
func TestRoleFallbackRetriesContinuesAndResets(t *testing.T) {
	for _, succeeds := range []bool{true, false} {
		t.Run(fmt.Sprintf("last_candidate_succeeds_%t", succeeds), func(t *testing.T) {
			document, err := spec.Decode([]byte(`version: amata/v1
name: fallback
entry: main
defaults:
  tpm:
    retries: 2
  agents:
    roles:
      coder:
        - codex:
            - model: first
            - model: second
        - nessy
        - claude: opus
        - nessy: glm
flows:
  main:
    steps:
      - id: coding
        coder: fix it
      - assert:
          expr: 'ctx.prev.value == "done"'
`))
			if err != nil {
				t.Fatal(err)
			}
			config := testConfig(t, document)
			initGitRepository(t, config.Workspace.Root)
			source := filepath.Join(config.Workspace.Root, "source")
			writeFile(t, source, "committed")
			runGit(t, config.Workspace.Root, "add", "source")
			runGit(t, config.Workspace.Root, "commit", "-m", "initial")
			writeFile(t, source, "earlier step")
			mustPersist(t, config)
			registry := builtinRegistry()
			calls := []string{}
			labels := map[string]bool{}
			metadataPaths := []string{}
			for _, name := range []string{"codex", "nessy", "claude"} {
				registry.factories[name] = func() executorapi.Executor {
					return &fakeExecutor{calls: new([]string), execute: func(sc executorapi.StepContext) state.StepResult {
						model, _ := sc.Step.Fields["model"].(string)
						calls = append(calls, name+":"+model)
						ordinal := len(calls)
						wantContent := "earlier step"
						wantSession, wantPrompt := "", ""
						if ordinal > 1 && ordinal <= 6 {
							wantContent, wantSession, wantPrompt = "codex partial", "codex-session", "continue"
						}
						if got := readFile(t, source); got != wantContent {
							t.Fatalf("call %d content = %q, want %q", ordinal, got, wantContent)
						}
						if sc.ContinuationSessionID != wantSession || sc.ContinuationPrompt != wantPrompt || sc.Step.Fields["prompt"] != "fix it" {
							t.Fatalf("call %d context = %#v", ordinal, sc)
						}
						if labels[sc.ExecutionLabel] {
							t.Fatalf("reused artifact label %q", sc.ExecutionLabel)
						}
						labels[sc.ExecutionLabel] = true
						writeFile(t, source, name+" partial")
						dir := executorapi.StepArtifactDir(sc.RunDir, sc.StepIndex, sc.Step.ID, sc.ExecutionLabel)
						path := filepath.Join(dir, "provider-metadata.json")
						sessionID := name + "-session"
						if ordinal == 3 {
							sessionID = ""
						}
						data, _ := json.Marshal(map[string]any{"continuation_session_id": sessionID, "model": model})
						writeFile(t, path, string(data))
						metadataPaths = append(metadataPaths, path)
						result := state.StepResult{Status: state.StepStatusFailed, Error: &state.Failure{Code: "provider_crashed", Message: "proxy failed"}, Artifacts: state.Artifacts{Files: map[string]string{"metadata": path}}}
						if name == "codex" {
							result.Error.Details = map[string]any{"provider_error": map[string]any{"code": "429", "session_id": sessionID}}
						}
						if ordinal == 9 && succeeds {
							result.Status, result.Error, result.Value = state.StepStatusSucceeded, nil, "done"
						}
						return result
					}}
				}
			}
			waits := 0
			snapshot, err := NewRunner(registry, withRunnerRetryWait(func(context.Context, time.Duration) error { waits++; return nil })).Run(context.Background(), config)
			if succeeds {
				if err != nil || snapshot.Status != state.RunStatusSucceeded || len(snapshot.Steps) != 2 {
					t.Fatalf("run = %#v, %v", snapshot, err)
				}
			} else {
				assertRunFailed(t, err, "provider_crashed")
				if len(snapshot.Steps) != 1 {
					t.Fatalf("steps = %#v", snapshot.Steps)
				}
			}
			want := []string{"codex:first", "codex:first", "codex:first", "codex:second", "codex:second", "codex:second", "nessy:", "claude:opus", "nessy:glm"}
			if !reflect.DeepEqual(calls, want) || waits != 4 {
				t.Fatalf("calls = %q, waits = %d", calls, waits)
			}
			for _, path := range metadataPaths {
				if _, err := os.Stat(path); err != nil {
					t.Fatal(err)
				}
			}
			// Exhaustion keeps the final attempt's edits, like ordinary failed steps.
			if got := readFile(t, source); got != "nessy partial" {
				t.Fatalf("final content = %q", got)
			}
		})
	}
}

// Nessy model fallback carries its exact session; cancellation stops the chain.
func TestRoleNessyContinuationAndCancellation(t *testing.T) {
	for _, cancelRun := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel_%t", cancelRun), func(t *testing.T) {
			document, err := spec.Decode([]byte(`version: amata/v1
name: nessy-role
entry: main
defaults:
  agents:
    roles:
      coder:
        - nessy:
            - model: first
            - model: second
flows:
  main:
    steps:
      - coder: fix it
`))
			if err != nil {
				t.Fatal(err)
			}
			config := testConfig(t, document)
			mustPersist(t, config)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			registry := builtinRegistry()
			registry.factories["nessy"] = func() executorapi.Executor {
				return &fakeExecutor{calls: new([]string), execute: func(sc executorapi.StepContext) state.StepResult {
					calls++
					if calls == 2 {
						if sc.ContinuationSessionID != "nessy-session" || sc.ContinuationPrompt != "continue" {
							t.Fatalf("continuation = %#v", sc)
						}
						return state.StepResult{Status: state.StepStatusSucceeded}
					}
					path := filepath.Join(sc.RunDir, "metadata.json")
					writeFile(t, path, `{"continuation_session_id":"nessy-session"}`)
					if cancelRun {
						cancel()
					}
					return state.StepResult{Status: state.StepStatusFailed, Error: &state.Failure{Code: "provider_crashed"}, Artifacts: state.Artifacts{Files: map[string]string{"metadata": path}}}
				}}
			}
			_, err = NewRunner(registry).Run(ctx, config)
			if cancelRun {
				if err == nil || calls != 1 {
					t.Fatalf("calls=%d, err=%v", calls, err)
				}
			} else if err != nil || calls != 2 {
				t.Fatalf("calls=%d, err=%v", calls, err)
			}
		})
	}
}

// A skipped role never snapshots the repository or starts an agent.
func TestSkippedRoleDoesNotRequireRepository(t *testing.T) {
	config := testConfig(t, spec.Document{Version: spec.Version, Name: "skip", Entry: "main", Defaults: map[string]any{"agents": map[string]any{"roles": map[string]any{"coder": []any{"codex", "nessy"}}}}, Flows: map[string]spec.Flow{"main": {Steps: []spec.Step{{Type: "coder", Fields: map[string]any{"prompt": "fix", "when": false}}}}}})
	mustPersist(t, config)
	snapshot, err := NewRunner(builtinRegistry()).Run(context.Background(), config)
	if err != nil || snapshot.Steps[0].Status != state.StepStatusSkipped {
		t.Fatalf("snapshot=%#v, err=%v", snapshot, err)
	}
}

// First-attempt success does not invoke remaining candidates.
func TestRoleStopsAtFirstSuccess(t *testing.T) {
	config := testConfig(t, spec.Document{Version: spec.Version, Name: "success", Entry: "main", Defaults: map[string]any{"agents": map[string]any{"roles": map[string]any{"coder": []any{"codex", "codex"}}}}, Flows: map[string]spec.Flow{"main": {Steps: []spec.Step{{Type: "coder", Fields: map[string]any{"prompt": "fix"}}}}}})
	mustPersist(t, config)
	registry := builtinRegistry()
	calls := 0
	registry.factories["codex"] = func() executorapi.Executor {
		return &fakeExecutor{calls: new([]string), execute: func(executorapi.StepContext) state.StepResult {
			calls++
			return state.StepResult{Status: state.StepStatusSucceeded}
		}}
	}
	var events []progress.Event
	_, err := NewRunner(registry, WithRunnerProgressSink(progress.SinkFunc(func(e progress.Event) { events = append(events, e) }))).Run(context.Background(), config)
	if err != nil || calls != 1 {
		t.Fatalf("calls=%d, err=%v", calls, err)
	}
	for _, event := range events {
		if event.Kind == progress.EventStepStarted && (event.Step.Type != "codex" || event.Step.Artifacts.Stdout == "") {
			t.Fatalf("first candidate progress=%#v", event.Step)
		}
	}
}

// Structured recovery has a fresh attempt budget for each fallback model.
func TestRoleStructuredRecoveryBeforeModelFallback(t *testing.T) {
	doc, err := spec.Decode([]byte(`version: amata/v1
name: structured-role
entry: main
defaults:
  executors:
    nessy:
      structured_retry:
        attempts: 2
  agents:
    roles:
      reviewer:
        - nessy:
            - model: first
            - model: second
flows:
  main:
    steps:
      - reviewer: review
        response:
          type: object
          properties:
            ok: {type: boolean}
          required: [ok]
`))
	if err != nil {
		t.Fatal(err)
	}
	config := testConfig(t, doc)
	mustPersist(t, config)
	calls := 0
	registry := builtinRegistry()
	registry.factories["nessy"] = func() executorapi.Executor {
		return &fakeExecutor{calls: new([]string), execute: func(sc executorapi.StepContext) state.StepResult {
			calls++
			wantModel := "first"
			if calls > 2 {
				wantModel = "second"
			}
			if sc.Step.Fields["model"] != wantModel {
				t.Fatalf("call %d model=%v", calls, sc.Step.Fields["model"])
			}
			if calls == 3 && (sc.ContinuationSessionID != "nessy-session" || sc.ContinuationPrompt != "continue") {
				t.Fatalf("model continuation=%#v", sc)
			}
			if calls == 4 {
				return state.StepResult{Status: state.StepStatusSucceeded, Value: map[string]any{"ok": true}}
			}
			path := filepath.Join(sc.RunDir, "metadata.json")
			writeFile(t, path, `{"continuation_session_id":"nessy-session"}`)
			return state.StepResult{Status: state.StepStatusFailed, Error: &state.Failure{Code: "invalid_provider_output", Message: "invalid JSON"}, Artifacts: state.Artifacts{Files: map[string]string{"metadata": path}}}
		}}
	}
	snapshot, err := NewRunner(registry).Run(context.Background(), config)
	if err != nil || calls != 4 || len(snapshot.Steps) != 1 {
		t.Fatalf("calls=%d snapshot=%#v err=%v", calls, snapshot, err)
	}
}

// A stalled attempt that ignores cancellation cannot overlap a fallback agent.
func TestRoleDoesNotFallbackWhileAttemptIsRunning(t *testing.T) {
	doc, err := spec.Decode([]byte(`version: amata/v1
name: stall
entry: main
defaults:
  agents:
    roles:
      coder: [codex, nessy]
flows:
  main:
    steps:
      - coder: fix
        stall:
          after: 1ms
          type: error
`))
	if err != nil {
		t.Fatal(err)
	}
	config := testConfig(t, doc)
	initGitRepository(t, config.Workspace.Root)
	mustPersist(t, config)
	release := make(chan struct{})
	defer close(release)
	calls := make(chan string, 2)
	registry := builtinRegistry()
	for _, name := range []string{"codex", "nessy"} {
		registry.factories[name] = func() executorapi.Executor {
			return &fakeExecutor{calls: new([]string), execute: func(executorapi.StepContext) state.StepResult {
				calls <- name
				<-release
				return state.StepResult{Status: state.StepStatusFailed}
			}}
		}
	}
	_, err = NewRunner(registry).Run(context.Background(), config)
	assertRunFailed(t, err, "step_stalled")
	if len(calls) != 1 || <-calls != "codex" {
		t.Fatal("fallback started while previous attempt was running")
	}
}
