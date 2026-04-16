package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/iw2rmb/amata/internal/progress"
	"github.com/iw2rmb/amata/internal/schema"
	"github.com/iw2rmb/amata/internal/spec"
	"github.com/iw2rmb/amata/internal/state"
)

type Runner struct {
	registry     *Registry
	progressSink progress.Sink
	retryWait    func(context.Context, time.Duration) error
	cleanupQueue map[stepCleanupKey]func() error
}

type RunnerOption func(*Runner)

type RunFailedError struct {
	RunID   string
	Failure state.Failure
}

func (e RunFailedError) Error() string {
	return fmt.Sprintf("run %q failed: %s", e.RunID, e.Failure.Message)
}

func WithRunnerProgressSink(sink progress.Sink) RunnerOption {
	return func(runner *Runner) {
		runner.progressSink = sink
	}
}

func withRunnerRetryWait(wait func(context.Context, time.Duration) error) RunnerOption {
	return func(runner *Runner) {
		runner.retryWait = wait
	}
}

func NewRunner(registry *Registry, options ...RunnerOption) *Runner {
	if registry == nil {
		registry = builtinRegistry()
	}

	runner := &Runner{
		registry:     registry,
		retryWait:    waitWithContext,
		cleanupQueue: map[stepCleanupKey]func() error{},
	}
	for _, option := range options {
		if option != nil {
			option(runner)
		}
	}

	return runner
}

func (r *Runner) Run(ctx context.Context, config Config) (state.Snapshot, error) {
	return r.execute(ctx, config, false)
}

func (r *Runner) Resume(ctx context.Context, config Config) (state.Snapshot, error) {
	return r.execute(ctx, config, true)
}

func (r *Runner) execute(ctx context.Context, config Config, resume bool) (state.Snapshot, error) {
	r.cleanupQueue = map[stepCleanupKey]func() error{}

	plan, err := buildFlowPlan(config.Spec)
	if err != nil {
		return state.Snapshot{}, err
	}
	entryFlow, ok := plan.Lookup(config.Spec.Entry)
	if !ok {
		return state.Snapshot{}, fmt.Errorf("entry flow %q is not defined", config.Spec.Entry)
	}
	responses := newResponseResolver(schema.NewRegistry(config.Spec.Schemas), config.Spec.Schemas)
	reporter := progress.NewReporter(config.RunID, r.progressSink)

	store := state.NewStore(config.RunDir)
	snapshot, err := store.LoadSnapshot()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return state.Snapshot{}, err
	}

	if resume {
		if errors.Is(err, os.ErrNotExist) {
			return state.Snapshot{}, fmt.Errorf("run %q has no stored state", config.RunID)
		}
		if len(snapshot.Frames) == 0 {
			return state.Snapshot{}, fmt.Errorf("run %q has no flow frame state", config.RunID)
		}

		switch snapshot.Status {
		case state.RunStatusSucceeded:
			return snapshot, nil
		case state.RunStatusFailed:
			failure := failureForSnapshot(config.RunID, snapshot)
			return snapshot, RunFailedError{
				RunID:   config.RunID,
				Failure: *failure,
			}
		}

		if failed := durableFailedStep(snapshot); failed != nil {
			failure := failureForStep(*failed)
			snapshot, err = store.Append(state.RunEvent{
				Kind:    state.EventRunFinished,
				Status:  state.RunStatusFailed,
				Failure: failure,
			})
			if err != nil {
				return state.Snapshot{}, err
			}
			reporter.RunFinished(progress.RunStatusFailed, state.CloneFailure(failure))
			return snapshot, RunFailedError{
				RunID:   config.RunID,
				Failure: *failure,
			}
		}

		if len(snapshot.Frames) > 0 {
			snapshot, err = store.Append(state.RunEvent{
				Kind:    state.EventRunResumed,
				Command: "resume",
			})
			if err != nil {
				return state.Snapshot{}, err
			}
			reporter.RunResumed(resumeActiveProgressSteps(config, plan, snapshot))
		}
	} else {
		if !errors.Is(err, os.ErrNotExist) {
			return state.Snapshot{}, fmt.Errorf("run %q already has stored state", config.RunID)
		}
		snapshot, err = store.Append(state.RunEvent{
			Kind: state.EventRunInitialized,
			Frame: &state.FlowFrame{
				ID:        state.FrameID(1),
				Flow:      config.Spec.Entry,
				StepCount: len(entryFlow.Steps),
			},
			Command: "run",
		})
		if err != nil {
			return state.Snapshot{}, err
		}
		reporter.RunStarted("run")
	}

	for {
		if len(snapshot.Frames) == 0 {
			return state.Snapshot{}, fmt.Errorf("run %q has no flow frame state", config.RunID)
		}

		frame := snapshot.Frames[len(snapshot.Frames)-1]
		previous := snapshot.StepByRef(frame.Previous)
		produced := snapshot.StepByRef(frame.Produced)
		lookup := snapshot.StepByRef
		flow, ok := plan.Lookup(frame.Flow)
		if !ok {
			return state.Snapshot{}, fmt.Errorf("flow %q is not defined", frame.Flow)
		}

		if frame.NextStep >= frame.StepCount {
			if frame.Return == nil {
				snapshot, err = store.Append(state.RunEvent{
					Kind:   state.EventRunFinished,
					Status: state.RunStatusSucceeded,
				})
				if err != nil {
					return state.Snapshot{}, err
				}
				reporter.RunFinished(progress.RunStatusSucceeded, nil)
				return snapshot, nil
			}

			parentFrame := snapshot.Frames[len(snapshot.Frames)-2]
			parentPrevious := snapshot.StepByRef(parentFrame.Previous)
			parentFlow, ok := plan.Lookup(parentFrame.Flow)
			if !ok {
				return state.Snapshot{}, fmt.Errorf("flow %q is not defined", parentFrame.Flow)
			}
			parentStep := parentFlow.Steps[frame.Return.StepIndex]
			if frame.Return.StepType == "for_each" {
				nextFrame, finalized := r.prepareForEachContinuation(config, plan, responses, lookup, parentFrame, parentPrevious, parentStep, frame.Return, produced)
				if nextFrame != nil {
					nextFrame.ID = state.FrameID(snapshot.LastSequence + 1)
					snapshot, err = store.Append(state.RunEvent{
						Kind:  state.EventControlContinued,
						Frame: nextFrame,
					})
					if err != nil {
						return state.Snapshot{}, err
					}
					continue
				}

				if snapshot, err = r.recordResultEvent(store, reporter, config, config.RunID, parentFrame.Flow, parentFrame.ID, parentStep, parentPrevious, parentFrame.Bindings, state.EventControlReturned, finalized, lookup, nil); err != nil {
					return snapshot, err
				}
				continue
			}

			returned := returnedControlResult(frame.Return, produced)
			finalized := r.finalizeStepResult(config, responses, lookup, parentPrevious, parentFrame.Bindings, parentStep, returned)

			if snapshot, err = r.recordResultEvent(store, reporter, config, config.RunID, parentFrame.Flow, parentFrame.ID, parentStep, parentPrevious, parentFrame.Bindings, state.EventControlReturned, finalized, lookup, nil); err != nil {
				return snapshot, err
			}
			continue
		}

		stepIndex := frame.NextStep
		step := flow.Steps[stepIndex]
		startedExecutionLabel := stepExecutionLabel(snapshot.LastSequence+2, 1)
		if snapshot, err = r.recordStepStartedEvent(store, reporter, config, frame.Flow, frame.ID, stepIndex, step, startedExecutionLabel, previous, frame.Bindings, lookup); err != nil {
			return state.Snapshot{}, err
		}

		if snapshot, err = r.dispatchStep(ctx, store, reporter, config, plan, responses, snapshot, frame, stepIndex, step, previous); err != nil {
			return snapshot, err
		}
	}
}

func (r *Runner) dispatchStep(
	ctx context.Context,
	store *state.Store,
	reporter *progress.Reporter,
	config Config,
	plan *flowPlan,
	responses responseResolver,
	snapshot state.Snapshot,
	frame state.FlowFrame,
	stepIndex int,
	step spec.Step,
	previous *state.StepResult,
) (state.Snapshot, error) {
	lookup := snapshot.StepByRef
	runtime := newStepRuntime(config, previous, lookup, frame.Bindings)

	var action stepAction
	var result state.StepResult
	var checkpointCleanup func() error
	switch step.ExecutorType() {
	case "call":
		action, result = r.prepareStepAction(config, runtime, previous, stepIndex, step)
	case "switch":
		action, result = r.prepareSwitch(config, runtime, plan, responses, lookup, frame.Flow, previous, frame.Bindings, stepIndex, step)
	case "for_each":
		action, result = r.prepareForEach(config, runtime, plan, responses, lookup, frame.Flow, previous, frame.Bindings, stepIndex, step)
	default:
		action, result, checkpointCleanup = r.executeStep(ctx, reporter, config, responses, snapshot, frame.Flow, frame.ID, stepIndex, step, previous, frame.Bindings)
	}

	if action.pushFrame != nil {
		action.pushFrame.ID = state.FrameID(snapshot.LastSequence + 1)
		return store.Append(state.RunEvent{
			Kind:  state.EventFramePushed,
			Frame: action.pushFrame,
		})
	}
	if checkpointCleanup != nil {
		r.scheduleStepCleanup(frame.ID, stepIndex, checkpointCleanup)
	}
	return r.recordResultEvent(store, reporter, config, config.RunID, frame.Flow, frame.ID, step, previous, frame.Bindings, state.EventStepRecorded, result, lookup, func() error {
		return r.runStepCleanup(frame.ID, result.Index)
	})
}

type stepCleanupKey struct {
	frameID   string
	stepIndex int
}

func (r *Runner) scheduleStepCleanup(frameID string, stepIndex int, cleanup func() error) {
	if cleanup == nil {
		return
	}
	if r.cleanupQueue == nil {
		r.cleanupQueue = map[stepCleanupKey]func() error{}
	}
	r.cleanupQueue[stepCleanupKey{frameID: frameID, stepIndex: stepIndex}] = cleanup
}

func (r *Runner) runStepCleanup(frameID string, stepIndex int) error {
	if len(r.cleanupQueue) == 0 {
		return nil
	}
	key := stepCleanupKey{frameID: frameID, stepIndex: stepIndex}
	cleanup, ok := r.cleanupQueue[key]
	if !ok {
		return nil
	}
	delete(r.cleanupQueue, key)
	return cleanup()
}
