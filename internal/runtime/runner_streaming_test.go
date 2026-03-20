package runtime

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	executorapi "github.com/iw2rmb/amata/internal/executor"
	codexexec "github.com/iw2rmb/amata/internal/executor/codex"
	"github.com/iw2rmb/amata/internal/progress"
	"github.com/iw2rmb/amata/internal/spec"
	"github.com/iw2rmb/amata/internal/state"
)

func TestRunnerAgentArtifactsAreReadableDuringAndAfterExecution(t *testing.T) {
	t.Parallel()

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "sample",
		Entry:   "main",
		Flows: map[string]spec.Flow{
			"main": {
				Steps: []spec.Step{
					{ID: "stream-step", Type: "fake"},
				},
			},
		},
	})

	mustPersist(t, config)

	// firstChunkReady carries the stdout path once the first chunk is on disk,
	// allowing the test to read the file while the executor is still running.
	firstChunkReady := make(chan string, 1)
	continueExecution := make(chan struct{})

	registry := NewRegistry()
	if err := registry.Register("fake", func() executorapi.Executor {
		return &fakeExecutor{
			calls: new([]string),
			executeWithContext: func(_ context.Context, ctx executorapi.StepContext) state.StepResult {
				stepDir := executorapi.StepArtifactDir(ctx.RunDir, ctx.StepIndex, ctx.Step.ID, ctx.ExecutionLabel)
				if err := os.MkdirAll(stepDir, 0o755); err != nil {
					return state.StepResult{
						Status: state.StepStatusFailed,
						Error:  &state.Failure{Code: "mkdir_failed", Message: err.Error()},
					}
				}

				stdoutPath := filepath.Join(stepDir, "stdout.txt")
				stderrPath := filepath.Join(stepDir, "stderr.txt")

				// Pre-create artifact files and write first chunk, as StreamCapture does
				// before the provider process starts.
				if err := os.WriteFile(stdoutPath, []byte("chunk1\n"), 0o644); err != nil {
					return state.StepResult{
						Status: state.StepStatusFailed,
						Error:  &state.Failure{Code: "write_failed", Message: err.Error()},
					}
				}
				if err := os.WriteFile(stderrPath, []byte{}, 0o644); err != nil {
					return state.StepResult{
						Status: state.StepStatusFailed,
						Error:  &state.Failure{Code: "write_failed", Message: err.Error()},
					}
				}

				firstChunkReady <- stdoutPath
				<-continueExecution

				f, err := os.OpenFile(stdoutPath, os.O_APPEND|os.O_WRONLY, 0o644)
				if err != nil {
					return state.StepResult{
						Status: state.StepStatusFailed,
						Error:  &state.Failure{Code: "open_failed", Message: err.Error()},
					}
				}
				_, writeErr := f.WriteString("chunk2\n")
				_ = f.Close()
				if writeErr != nil {
					return state.StepResult{
						Status: state.StepStatusFailed,
						Error:  &state.Failure{Code: "write_failed", Message: writeErr.Error()},
					}
				}

				return state.StepResult{
					Status: state.StepStatusSucceeded,
					Artifacts: state.Artifacts{
						Stdout: stdoutPath,
						Stderr: stderrPath,
						Files:  map[string]string{},
					},
				}
			},
		}
	}); err != nil {
		t.Fatalf("register executor: %v", err)
	}

	type runResult struct {
		snapshot state.Snapshot
		err      error
	}
	resultCh := make(chan runResult, 1)
	go func() {
		snapshot, err := NewRunner(registry).Run(context.Background(), config)
		resultCh <- runResult{snapshot, err}
	}()

	// Confirm artifact file is readable while the executor is still running.
	stdoutPath := <-firstChunkReady
	midRunData, err := os.ReadFile(stdoutPath)
	if err != nil {
		t.Fatalf("read stdout during execution: %v", err)
	}
	if string(midRunData) != "chunk1\n" {
		t.Fatalf("mid-run stdout = %q, want %q", string(midRunData), "chunk1\n")
	}
	eventsLog, err := os.ReadFile(filepath.Join(config.RunDir, "events.ndjson"))
	if err != nil {
		t.Fatalf("read events log during execution: %v", err)
	}
	if !strings.Contains(string(eventsLog), `"kind":"step_started"`) {
		t.Fatalf("events log during execution missing step_started event:\n%s", eventsLog)
	}
	if !strings.Contains(string(eventsLog), `"id":"stream-step"`) {
		t.Fatalf("events log during execution missing stream-step id:\n%s", eventsLog)
	}

	close(continueExecution)
	res := <-resultCh
	if res.err != nil {
		t.Fatalf("run: %v", res.err)
	}

	// Confirm artifact paths in snapshot are stable and contain complete output.
	if len(res.snapshot.Steps) != 1 {
		t.Fatalf("step count = %d, want 1", len(res.snapshot.Steps))
	}
	step := res.snapshot.Steps[0]
	if step.Status != state.StepStatusSucceeded {
		t.Fatalf("step status = %q, want succeeded", step.Status)
	}
	if step.Artifacts.Stdout == "" {
		t.Fatal("stdout artifact path empty after step completion")
	}
	finalData, err := os.ReadFile(step.Artifacts.Stdout)
	if err != nil {
		t.Fatalf("read stdout after completion: %v", err)
	}
	if string(finalData) != "chunk1\nchunk2\n" {
		t.Fatalf("final stdout = %q, want %q", string(finalData), "chunk1\nchunk2\n")
	}
}

func TestRunnerAgentExecutorStreamsThroughRealCapturePath(t *testing.T) {
	t.Parallel()

	config := testConfig(t, spec.Document{
		Version: spec.Version,
		Name:    "agent-stream",
		Entry:   "main",
		Flows: map[string]spec.Flow{
			"main": {
				Steps: []spec.Step{
					{
						ID:   "agent-step",
						Type: "codex",
						Fields: map[string]any{
							"prompt": "hello",
							"model":  "fake-model",
						},
					},
				},
			},
		},
	})

	mustPersist(t, config)

	firstChunkReady := make(chan string, 1)
	continueExecution := make(chan struct{})

	fakeRun := codexexec.RunnerFunc(func(_ context.Context, args []string, _ string, _ []string, _ []byte, stdout, _ io.Writer) error {
		// Derive artifact dir from the -o flag so we can compute the stdout path.
		var outputPath string
		for i, arg := range args {
			if arg == "-o" && i+1 < len(args) {
				outputPath = args[i+1]
			}
		}
		stdoutPath := filepath.Join(filepath.Dir(outputPath), "stdout.txt")

		// Stream first chunk through the writer wired to the pre-created artifact file.
		if _, err := fmt.Fprint(stdout, "chunk1\n"); err != nil {
			return err
		}

		firstChunkReady <- stdoutPath
		<-continueExecution

		if _, err := fmt.Fprint(stdout, "chunk2\n"); err != nil {
			return err
		}
		return os.WriteFile(outputPath, []byte("fake transcript"), 0o644)
	})

	registry := NewRegistry()
	if err := registry.Register("codex", func() executorapi.Executor {
		return codexexec.NewWithRunner(fakeRun)
	}); err != nil {
		t.Fatalf("register codex executor: %v", err)
	}

	var events []progress.Event
	sink := progress.SinkFunc(func(event progress.Event) {
		events = append(events, event)
	})

	type runResult struct {
		snapshot state.Snapshot
		err      error
	}
	resultCh := make(chan runResult, 1)
	go func() {
		snapshot, err := NewRunner(registry, WithRunnerProgressSink(sink)).Run(context.Background(), config)
		resultCh <- runResult{snapshot, err}
	}()

	// Confirm artifact file is readable while the agent executor is still running,
	// exercising the agent.StreamCapture pre-creation contract.
	stdoutPath := <-firstChunkReady
	midRunData, err := os.ReadFile(stdoutPath)
	if err != nil {
		t.Fatalf("read stdout during agent execution: %v", err)
	}
	if string(midRunData) != "chunk1\n" {
		t.Fatalf("mid-run stdout = %q, want %q", string(midRunData), "chunk1\n")
	}

	close(continueExecution)
	res := <-resultCh
	if res.err != nil {
		t.Fatalf("run: %v", res.err)
	}

	if len(res.snapshot.Steps) != 1 {
		t.Fatalf("step count = %d, want 1", len(res.snapshot.Steps))
	}
	step := res.snapshot.Steps[0]
	if step.Status != state.StepStatusSucceeded {
		t.Fatalf("step status = %q, want succeeded", step.Status)
	}
	if step.Artifacts.Stdout == "" {
		t.Fatal("stdout artifact path empty after step completion")
	}
	finalData, err := os.ReadFile(step.Artifacts.Stdout)
	if err != nil {
		t.Fatalf("read stdout after completion: %v", err)
	}
	if string(finalData) != "chunk1\nchunk2\n" {
		t.Fatalf("final stdout = %q, want %q", string(finalData), "chunk1\nchunk2\n")
	}

	// Confirm progress event ordering is stable throughout the streaming scenario.
	assertProgressKindsAndSteps(t, events,
		[]progress.EventKind{
			progress.EventRunStarted,
			progress.EventStepStarted,
			progress.EventStepFinished,
			progress.EventRunFinished,
		},
		[]string{"", "agent-step", "agent-step", ""},
	)
	finished := events[len(events)-1]
	if finished.Status != progress.RunStatusSucceeded {
		t.Fatalf("final event status = %q, want succeeded", finished.Status)
	}
}
