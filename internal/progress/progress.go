package progress

import (
	"sync"
	"time"

	"github.com/iw2rmb/amata/internal/jsonutil"
	"github.com/iw2rmb/amata/internal/state"
)

type EventKind string

const (
	EventRunStarted   EventKind = "run_started"
	EventRunResumed   EventKind = "run_resumed"
	EventStepStarted  EventKind = "step_started"
	EventStepFinished EventKind = "step_finished"
	EventRunFinished  EventKind = "run_finished"
)

type RunStatus = state.RunStatus

const (
	RunStatusRunning   = state.RunStatusRunning
	RunStatusSucceeded = state.RunStatusSucceeded
	RunStatusFailed    = state.RunStatusFailed
)

type StepStatus = state.StepStatus

const (
	StepStatusRunning   = state.StepStatusRunning
	StepStatusSucceeded = state.StepStatusSucceeded
	StepStatusFailed    = state.StepStatusFailed
	StepStatusSkipped   = state.StepStatusSkipped
)

type Failure = state.Failure

type Artifacts = state.Artifacts

type Step struct {
	Flow       string          `json:"flow"`
	Index      int             `json:"index"`
	ID         string          `json:"id,omitempty"`
	Type       string          `json:"type,omitempty"`
	Status     StepStatus      `json:"status"`
	Value      any             `json:"value,omitempty"`
	Error      *Failure        `json:"error,omitempty"`
	Artifacts  Artifacts       `json:"artifacts"`
	Descriptor *DescriptorData `json:"descriptor,omitempty"`
	StartedAt  time.Time       `json:"started_at,omitempty"`
	FinishedAt time.Time       `json:"finished_at,omitempty"`
}

type Snapshot struct {
	RunID   string    `json:"run_id"`
	Command string    `json:"command,omitempty"`
	Status  RunStatus `json:"status"`
	Active  []Step    `json:"active,omitempty"`
	Steps   []Step    `json:"steps,omitempty"`
	Failure *Failure  `json:"failure,omitempty"`
}

type Event struct {
	Kind     EventKind `json:"kind"`
	At       time.Time `json:"at"`
	RunID    string    `json:"run_id"`
	Command  string    `json:"command,omitempty"`
	Status   RunStatus `json:"status,omitempty"`
	Step     *Step     `json:"step,omitempty"`
	Failure  *Failure  `json:"failure,omitempty"`
	Snapshot Snapshot  `json:"snapshot"`
}

type Sink interface {
	WriteProgress(Event)
}

type SinkFunc func(Event)

func (f SinkFunc) WriteProgress(event Event) {
	f(event)
}

type Reporter struct {
	mu       sync.Mutex
	sink     Sink
	snapshot Snapshot
}

func NewReporter(runID string, sink Sink) *Reporter {
	return &Reporter{
		sink: sink,
		snapshot: Snapshot{
			RunID:  runID,
			Active: []Step{},
			Steps:  []Step{},
		},
	}
}

func (r *Reporter) Snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()

	return cloneSnapshot(r.snapshot)
}

func (r *Reporter) RunStarted(command string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.snapshot.Command = command
	r.snapshot.Status = RunStatusRunning
	r.snapshot.Failure = nil
	r.emitLocked(Event{
		Kind:    EventRunStarted,
		At:      time.Now().UTC(),
		RunID:   r.snapshot.RunID,
		Command: command,
		Status:  RunStatusRunning,
	})
}

func (r *Reporter) RunResumed(active []Step) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.snapshot.Command = "resume"
	r.snapshot.Status = RunStatusRunning
	r.snapshot.Failure = nil
	if len(active) > 0 {
		r.snapshot.Active = make([]Step, len(active))
		for index, step := range active {
			normalized := cloneStep(step)
			if normalized.Status == "" {
				normalized.Status = StepStatusRunning
			}
			r.snapshot.Active[index] = normalized
		}
	} else {
		r.snapshot.Active = []Step{}
	}
	r.emitLocked(Event{
		Kind:    EventRunResumed,
		At:      time.Now().UTC(),
		RunID:   r.snapshot.RunID,
		Command: "resume",
		Status:  RunStatusRunning,
	})
}

func (r *Reporter) StepStarted(step Step) {
	r.mu.Lock()
	defer r.mu.Unlock()

	normalized := cloneStep(step)
	if normalized.Status == "" {
		normalized.Status = StepStatusRunning
	}
	if normalized.StartedAt.IsZero() {
		normalized.StartedAt = time.Now().UTC()
	}
	normalized.FinishedAt = time.Time{}
	r.snapshot.Active = append(r.snapshot.Active, normalized)
	r.emitLocked(Event{
		Kind:  EventStepStarted,
		At:    normalized.StartedAt,
		RunID: r.snapshot.RunID,
		Step:  stepPointer(normalized),
	})
}

func (r *Reporter) StepFinished(step Step) {
	r.mu.Lock()
	defer r.mu.Unlock()

	normalized := cloneStep(step)
	match := findActiveStep(r.snapshot.Active, normalized)
	if match >= 0 {
		if normalized.StartedAt.IsZero() {
			normalized.StartedAt = r.snapshot.Active[match].StartedAt
		}
		r.snapshot.Active = append(r.snapshot.Active[:match], r.snapshot.Active[match+1:]...)
	}
	if normalized.FinishedAt.IsZero() {
		normalized.FinishedAt = time.Now().UTC()
	}
	r.snapshot.Steps = append(r.snapshot.Steps, normalized)
	r.emitLocked(Event{
		Kind:  EventStepFinished,
		At:    normalized.FinishedAt,
		RunID: r.snapshot.RunID,
		Step:  stepPointer(normalized),
	})
}

func (r *Reporter) RunFinished(status RunStatus, failure *Failure) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.snapshot.Status = status
	r.snapshot.Failure = state.CloneFailure(failure)
	r.emitLocked(Event{
		Kind:    EventRunFinished,
		At:      time.Now().UTC(),
		RunID:   r.snapshot.RunID,
		Command: r.snapshot.Command,
		Status:  status,
		Failure: state.CloneFailure(failure),
	})
}

func (r *Reporter) emitLocked(event Event) {
	event.Command = firstNonEmpty(event.Command, r.snapshot.Command)
	if event.Status == "" {
		event.Status = r.snapshot.Status
	}
	event.Failure = state.CloneFailure(event.Failure)
	event.Step = cloneStepPointer(event.Step)
	event.Snapshot = cloneSnapshot(r.snapshot)
	if r.sink != nil {
		r.sink.WriteProgress(event)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func findActiveStep(active []Step, target Step) int {
	for index := len(active) - 1; index >= 0; index-- {
		candidate := active[index]
		if candidate.Flow != target.Flow {
			continue
		}
		if candidate.Index != target.Index {
			continue
		}
		if candidate.ID != target.ID {
			continue
		}
		if candidate.Type != target.Type {
			continue
		}
		return index
	}
	return -1
}

func stepPointer(step Step) *Step {
	cloned := cloneStep(step)
	return &cloned
}

func cloneStepPointer(step *Step) *Step {
	if step == nil {
		return nil
	}
	cloned := cloneStep(*step)
	return &cloned
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	cloned := Snapshot{
		RunID:   snapshot.RunID,
		Command: snapshot.Command,
		Status:  snapshot.Status,
		Failure: state.CloneFailure(snapshot.Failure),
		Active:  cloneSteps(snapshot.Active),
		Steps:   cloneSteps(snapshot.Steps),
	}
	return cloned
}

func cloneSteps(steps []Step) []Step {
	if len(steps) == 0 {
		return []Step{}
	}
	cloned := make([]Step, len(steps))
	for index, step := range steps {
		cloned[index] = cloneStep(step)
	}
	return cloned
}

func cloneStep(step Step) Step {
	step.Value = jsonutil.CloneValue(step.Value)
	step.Error = state.CloneFailure(step.Error)
	step.Artifacts = state.CloneArtifacts(step.Artifacts)
	step.Descriptor = cloneDescriptorData(step.Descriptor)
	return step
}

func StepFromResult(flowName string, result state.StepResult) Step {
	return Step{
		Flow:      flowName,
		Index:     result.Index,
		ID:        result.ID,
		Type:      result.Type,
		Status:    result.Status,
		Value:     jsonutil.CloneValue(result.Value),
		Error:     state.CloneFailure(result.Error),
		Artifacts: state.CloneArtifacts(result.Artifacts),
	}
}
