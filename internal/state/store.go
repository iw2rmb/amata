package state

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"auto/internal/jsonutil"
)

type RunStatus string

const (
	RunStatusRunning   RunStatus = "running"
	RunStatusSucceeded RunStatus = "succeeded"
	RunStatusFailed    RunStatus = "failed"
)

type StepStatus string

const (
	StepStatusSucceeded StepStatus = "succeeded"
	StepStatusFailed    StepStatus = "failed"
	StepStatusSkipped   StepStatus = "skipped"
)

type EventKind string

const (
	EventRunInitialized   EventKind = "run_initialized"
	EventRunResumed       EventKind = "run_resumed"
	EventFramePushed      EventKind = "frame_pushed"
	EventControlContinued EventKind = "control_continued"
	EventControlReturned  EventKind = "control_returned"
	EventStepRecorded     EventKind = "step_recorded"
	EventRunFinished      EventKind = "run_finished"
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

type ForEachState struct {
	Items []any  `json:"items,omitempty"`
	Index int    `json:"index"`
	As    string `json:"as,omitempty"`
}

type FrameReturn struct {
	StepType   string        `json:"step_type"`
	ResultType string        `json:"result_type,omitempty"`
	StepIndex  int           `json:"step_index"`
	StepID     string        `json:"step_id,omitempty"`
	Flow       string        `json:"flow,omitempty"`
	CaseIndex  *int          `json:"case_index,omitempty"`
	ForEach    *ForEachState `json:"for_each,omitempty"`
}

type FlowFrame struct {
	Flow      string         `json:"flow"`
	StepCount int            `json:"step_count"`
	NextStep  int            `json:"next_step"`
	Previous  *StepResult    `json:"previous,omitempty"`
	Produced  *StepResult    `json:"produced,omitempty"`
	Bindings  map[string]any `json:"bindings,omitempty"`
	Return    *FrameReturn   `json:"return,omitempty"`
}

type StepResult struct {
	Index     int        `json:"index"`
	ID        string     `json:"id,omitempty"`
	Type      string     `json:"type,omitempty"`
	Status    StepStatus `json:"status"`
	Value     any        `json:"value,omitempty"`
	Error     *Failure   `json:"error,omitempty"`
	Artifacts Artifacts  `json:"artifacts"`
}

type Snapshot struct {
	Status       RunStatus    `json:"status"`
	Frames       []FlowFrame  `json:"frames"`
	Steps        []StepResult `json:"steps"`
	Failure      *Failure     `json:"failure,omitempty"`
	LastSequence int          `json:"last_sequence"`
}

type RunEvent struct {
	Sequence int         `json:"sequence"`
	Kind     EventKind   `json:"kind"`
	Status   RunStatus   `json:"status,omitempty"`
	Frame    *FlowFrame  `json:"frame,omitempty"`
	Step     *StepResult `json:"step,omitempty"`
	Failure  *Failure    `json:"failure,omitempty"`
	Command  string      `json:"command,omitempty"`
}

type Store struct {
	runDir       string
	eventsPath   string
	snapshotPath string
}

func NewStore(runDir string) *Store {
	return &Store{
		runDir:       runDir,
		eventsPath:   filepath.Join(runDir, "events.ndjson"),
		snapshotPath: filepath.Join(runDir, "snapshot.json"),
	}
}

func (s *Store) EventsPath() string {
	return s.eventsPath
}

func (s *Store) SnapshotPath() string {
	return s.snapshotPath
}

func (s *Store) Append(event RunEvent) (Snapshot, error) {
	current, err := s.LoadSnapshot()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Snapshot{}, err
	}

	if err := os.MkdirAll(s.runDir, 0o755); err != nil {
		return Snapshot{}, fmt.Errorf("create run directory: %w", err)
	}

	event.Sequence = current.LastSequence + 1
	next, err := apply(current, event)
	if err != nil {
		return Snapshot{}, err
	}

	record, err := json.Marshal(event)
	if err != nil {
		return Snapshot{}, fmt.Errorf("marshal event: %w", err)
	}

	file, err := os.OpenFile(s.eventsPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return Snapshot{}, fmt.Errorf("open events log: %w", err)
	}
	defer file.Close()

	if _, err := file.Write(append(record, '\n')); err != nil {
		return Snapshot{}, fmt.Errorf("append event: %w", err)
	}

	if err := s.writeSnapshot(next); err != nil {
		return Snapshot{}, err
	}

	return next, nil
}

func (s *Store) LoadSnapshot() (Snapshot, error) {
	data, err := os.ReadFile(s.snapshotPath)
	if err == nil {
		var snapshot Snapshot
		if err := json.Unmarshal(data, &snapshot); err != nil {
			rebuilt, rebuildErr := s.RebuildSnapshot()
			if rebuildErr != nil {
				return Snapshot{}, fmt.Errorf("decode snapshot: %w", err)
			}
			if rebuilt.LastSequence == 0 {
				return Snapshot{}, fmt.Errorf("decode snapshot: %w", err)
			}
			if err := s.writeSnapshot(rebuilt); err != nil {
				return Snapshot{}, err
			}
			return rebuilt, nil
		}
		return snapshot, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Snapshot{}, fmt.Errorf("read snapshot: %w", err)
	}

	snapshot, rebuildErr := s.RebuildSnapshot()
	if rebuildErr != nil {
		return Snapshot{}, rebuildErr
	}
	if snapshot.LastSequence == 0 {
		return Snapshot{}, os.ErrNotExist
	}
	if err := s.writeSnapshot(snapshot); err != nil {
		return Snapshot{}, err
	}

	return snapshot, nil
}

func (s *Store) RebuildSnapshot() (Snapshot, error) {
	file, err := os.Open(s.eventsPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Snapshot{}, os.ErrNotExist
		}
		return Snapshot{}, fmt.Errorf("open events log: %w", err)
	}
	defer file.Close()

	var snapshot Snapshot
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Bytes()
		var event RunEvent
		if err := json.Unmarshal(line, &event); err != nil {
			return Snapshot{}, fmt.Errorf("decode event: %w", err)
		}

		snapshot, err = apply(snapshot, event)
		if err != nil {
			return Snapshot{}, err
		}
	}
	if err := scanner.Err(); err != nil {
		return Snapshot{}, fmt.Errorf("scan events log: %w", err)
	}

	return snapshot, nil
}

func (s *Store) writeSnapshot(snapshot Snapshot) error {
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(s.snapshotPath, data, 0o644); err != nil {
		return fmt.Errorf("write snapshot: %w", err)
	}

	return nil
}

func apply(snapshot Snapshot, event RunEvent) (Snapshot, error) {
	switch event.Kind {
	case EventRunInitialized:
		if event.Frame == nil {
			return Snapshot{}, fmt.Errorf("run initialization event is missing frame")
		}
		snapshot.Status = RunStatusRunning
		snapshot.Frames = []FlowFrame{cloneFlowFrame(*event.Frame)}
		snapshot.Steps = nil
		snapshot.Failure = nil
	case EventRunResumed:
		snapshot.Status = RunStatusRunning
		snapshot.Failure = nil
	case EventFramePushed:
		if event.Frame == nil {
			return Snapshot{}, fmt.Errorf("frame push event is missing frame")
		}
		snapshot.Frames = append(snapshot.Frames, cloneFlowFrame(*event.Frame))
	case EventControlContinued:
		if event.Frame == nil {
			return Snapshot{}, fmt.Errorf("control continue event is missing frame")
		}
		if len(snapshot.Frames) < 2 {
			return Snapshot{}, fmt.Errorf("control continue event has no child and parent frame")
		}
		top := snapshot.Frames[len(snapshot.Frames)-1]
		if top.Return == nil {
			return Snapshot{}, fmt.Errorf("control continue event has no return metadata")
		}
		if top.NextStep < top.StepCount {
			return Snapshot{}, fmt.Errorf("control continue for flow %q before completion", top.Flow)
		}
		snapshot.Frames[len(snapshot.Frames)-1] = cloneFlowFrame(*event.Frame)
	case EventStepRecorded:
		if event.Step == nil {
			return Snapshot{}, fmt.Errorf("step event is missing step result")
		}
		if len(snapshot.Frames) == 0 {
			return Snapshot{}, fmt.Errorf("step event has no flow frame")
		}
		expected := snapshot.Frames[len(snapshot.Frames)-1].NextStep
		if event.Step.Index != expected {
			return Snapshot{}, fmt.Errorf("step event index %d does not match expected next step %d", event.Step.Index, expected)
		}
		step := cloneStepResult(*event.Step)
		snapshot.Steps = append(snapshot.Steps, step)
		snapshot.Frames[len(snapshot.Frames)-1].NextStep = event.Step.Index + 1
		if step.Status == StepStatusSucceeded {
			snapshot.Frames[len(snapshot.Frames)-1].Previous = &step
			snapshot.Frames[len(snapshot.Frames)-1].Produced = &step
		}
	case EventControlReturned:
		if event.Step == nil {
			return Snapshot{}, fmt.Errorf("control return event is missing step result")
		}
		if len(snapshot.Frames) < 2 {
			return Snapshot{}, fmt.Errorf("control return event has no child and parent frame")
		}
		top := snapshot.Frames[len(snapshot.Frames)-1]
		if top.Return == nil {
			return Snapshot{}, fmt.Errorf("control return event has no return metadata")
		}
		if top.NextStep < top.StepCount {
			return Snapshot{}, fmt.Errorf("control return for flow %q before completion", top.Flow)
		}
		expectedType := top.Return.StepType
		if top.Return.ResultType != "" {
			expectedType = top.Return.ResultType
		}
		if event.Step.Type != expectedType {
			return Snapshot{}, fmt.Errorf("control return step type %q does not match expected %q", event.Step.Type, expectedType)
		}
		if top.Return.StepID != "" && event.Step.ID != top.Return.StepID {
			return Snapshot{}, fmt.Errorf("control return step id %q does not match expected %q", event.Step.ID, top.Return.StepID)
		}
		snapshot.Frames = snapshot.Frames[:len(snapshot.Frames)-1]
		expected := snapshot.Frames[len(snapshot.Frames)-1].NextStep
		if event.Step.Index != expected {
			return Snapshot{}, fmt.Errorf("control return step index %d does not match expected next step %d", event.Step.Index, expected)
		}
		step := cloneStepResult(*event.Step)
		snapshot.Steps = append(snapshot.Steps, step)
		snapshot.Frames[len(snapshot.Frames)-1].NextStep = event.Step.Index + 1
		if step.Status == StepStatusSucceeded {
			snapshot.Frames[len(snapshot.Frames)-1].Previous = &step
			snapshot.Frames[len(snapshot.Frames)-1].Produced = &step
		}
	case EventRunFinished:
		snapshot.Status = event.Status
		snapshot.Failure = cloneFailure(event.Failure)
	default:
		return Snapshot{}, fmt.Errorf("unsupported event kind %q", event.Kind)
	}

	snapshot.LastSequence = event.Sequence
	return snapshot, nil
}

func cloneFailure(in *Failure) *Failure {
	if in == nil {
		return nil
	}

	out := *in
	return &out
}

func cloneFlowFrame(in FlowFrame) FlowFrame {
	return FlowFrame{
		Flow:      in.Flow,
		StepCount: in.StepCount,
		NextStep:  in.NextStep,
		Previous:  cloneStepResultPtr(in.Previous),
		Produced:  cloneStepResultPtr(in.Produced),
		Bindings:  cloneBindings(in.Bindings),
		Return:    cloneFrameReturn(in.Return),
	}
}

func cloneFrameReturn(in *FrameReturn) *FrameReturn {
	if in == nil {
		return nil
	}

	out := *in
	if in.CaseIndex != nil {
		caseIndex := *in.CaseIndex
		out.CaseIndex = &caseIndex
	}
	out.ForEach = cloneForEachState(in.ForEach)
	return &out
}

func cloneForEachState(in *ForEachState) *ForEachState {
	if in == nil {
		return nil
	}

	return &ForEachState{
		Items: jsonutil.CloneValue(in.Items).([]any),
		Index: in.Index,
		As:    in.As,
	}
}

func cloneBindings(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	return jsonutil.CloneMap(in)
}

func cloneStepResultPtr(in *StepResult) *StepResult {
	if in == nil {
		return nil
	}

	out := cloneStepResult(*in)
	return &out
}

func cloneStepResult(in StepResult) StepResult {
	out := in
	out.Value = jsonutil.CloneValue(in.Value)
	out.Error = cloneFailure(in.Error)
	out.Artifacts = cloneArtifacts(in.Artifacts)
	return out
}

func cloneArtifacts(in Artifacts) Artifacts {
	out := Artifacts{
		Stdout: in.Stdout,
		Stderr: in.Stderr,
	}
	if len(in.Files) == 0 {
		out.Files = map[string]string{}
		return out
	}

	out.Files = make(map[string]string, len(in.Files))
	for name, path := range in.Files {
		out.Files[name] = path
	}
	return out
}
