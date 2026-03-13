package progress

import (
	"sync"
	"time"

	"auto/internal/jsonutil"
	"auto/internal/state"
)

type EventKind string

const (
	EventRunStarted   EventKind = "run_started"
	EventRunResumed   EventKind = "run_resumed"
	EventStepStarted  EventKind = "step_started"
	EventStepFinished EventKind = "step_finished"
	EventRunFinished  EventKind = "run_finished"
)

type RunStatus string

const (
	RunStatusRunning   RunStatus = "running"
	RunStatusSucceeded RunStatus = "succeeded"
	RunStatusFailed    RunStatus = "failed"
)

type StepStatus string

const (
	StepStatusRunning   StepStatus = "running"
	StepStatusSucceeded StepStatus = "succeeded"
	StepStatusFailed    StepStatus = "failed"
	StepStatusSkipped   StepStatus = "skipped"
)

type Failure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Artifacts struct {
	Stdout string            `json:"stdout,omitempty"`
	Stderr string            `json:"stderr,omitempty"`
	Files  map[string]string `json:"files,omitempty"`
}

type Step struct {
	Flow       string     `json:"flow"`
	Index      int        `json:"index"`
	ID         string     `json:"id,omitempty"`
	Type       string     `json:"type,omitempty"`
	Status     StepStatus `json:"status"`
	Value      any        `json:"value,omitempty"`
	Error      *Failure   `json:"error,omitempty"`
	Artifacts  Artifacts  `json:"artifacts"`
	StartedAt  time.Time  `json:"started_at,omitempty"`
	FinishedAt time.Time  `json:"finished_at,omitempty"`
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
	r.snapshot.Failure = cloneFailure(failure)
	r.emitLocked(Event{
		Kind:    EventRunFinished,
		At:      time.Now().UTC(),
		RunID:   r.snapshot.RunID,
		Command: r.snapshot.Command,
		Status:  status,
		Failure: cloneFailure(failure),
	})
}

func (r *Reporter) emitLocked(event Event) {
	event.Command = firstNonEmpty(event.Command, r.snapshot.Command)
	if event.Status == "" {
		event.Status = r.snapshot.Status
	}
	event.Failure = cloneFailure(event.Failure)
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
		Failure: cloneFailure(snapshot.Failure),
	}
	if len(snapshot.Active) > 0 {
		cloned.Active = make([]Step, len(snapshot.Active))
		for index, step := range snapshot.Active {
			cloned.Active[index] = cloneStep(step)
		}
	} else {
		cloned.Active = []Step{}
	}
	if len(snapshot.Steps) > 0 {
		cloned.Steps = make([]Step, len(snapshot.Steps))
		for index, step := range snapshot.Steps {
			cloned.Steps[index] = cloneStep(step)
		}
	} else {
		cloned.Steps = []Step{}
	}
	return cloned
}

func cloneStep(step Step) Step {
	step.Value = jsonutil.CloneValue(step.Value)
	step.Error = cloneFailure(step.Error)
	step.Artifacts = cloneArtifacts(step.Artifacts)
	return step
}

func cloneFailure(failure *Failure) *Failure {
	if failure == nil {
		return nil
	}
	cloned := *failure
	return &cloned
}

func cloneArtifacts(artifacts Artifacts) Artifacts {
	cloned := Artifacts{
		Stdout: artifacts.Stdout,
		Stderr: artifacts.Stderr,
	}
	if len(artifacts.Files) > 0 {
		cloned.Files = make(map[string]string, len(artifacts.Files))
		for name, path := range artifacts.Files {
			cloned.Files[name] = path
		}
	} else {
		cloned.Files = map[string]string{}
	}
	return cloned
}

func StepFromResult(flowName string, result state.StepResult) Step {
	return Step{
		Flow:      flowName,
		Index:     result.Index,
		ID:        result.ID,
		Type:      result.Type,
		Status:    stepStatusFromState(result.Status),
		Value:     jsonutil.CloneValue(result.Value),
		Error:     failureFromState(result.Error),
		Artifacts: artifactsFromState(result.Artifacts),
	}
}

func failureFromState(failure *state.Failure) *Failure {
	if failure == nil {
		return nil
	}
	return &Failure{
		Code:    failure.Code,
		Message: failure.Message,
	}
}

func artifactsFromState(artifacts state.Artifacts) Artifacts {
	cloned := Artifacts{
		Stdout: artifacts.Stdout,
		Stderr: artifacts.Stderr,
	}
	if len(artifacts.Files) > 0 {
		cloned.Files = make(map[string]string, len(artifacts.Files))
		for name, path := range artifacts.Files {
			cloned.Files[name] = path
		}
	} else {
		cloned.Files = map[string]string{}
	}
	return cloned
}

func stepStatusFromState(status state.StepStatus) StepStatus {
	switch status {
	case state.StepStatusSucceeded:
		return StepStatusSucceeded
	case state.StepStatusSkipped:
		return StepStatusSkipped
	default:
		return StepStatusFailed
	}
}
