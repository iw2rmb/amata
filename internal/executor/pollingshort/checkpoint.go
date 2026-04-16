package pollingshort

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/iw2rmb/amata/internal/executor"
	"github.com/iw2rmb/amata/internal/jsonutil"
	"github.com/iw2rmb/amata/internal/state"
)

const checkpointVersion = 1

type checkpointStore interface {
	Load(executor.CheckpointKey) (pollState, bool, error)
	Save(executor.CheckpointKey, pollState) error
	Delete(executor.CheckpointKey) error
}

type fileCheckpointStore struct{}

type checkpointDocument struct {
	Version int       `json:"version"`
	State   pollState `json:"state"`
}

type pollState struct {
	Request        httpResponse   `json:"request"`
	Confirm        confirmState   `json:"confirm"`
	ConfirmStarted time.Time      `json:"confirm_started"`
	Terminal       *terminalState `json:"terminal,omitempty"`
}

type confirmState struct {
	Attempts    int           `json:"attempts"`
	Response    *httpResponse `json:"response,omitempty"`
	LastAttempt time.Time     `json:"last_attempt,omitempty"`
}

type terminalState struct {
	Status state.StepStatus `json:"status"`
	Value  any              `json:"value,omitempty"`
	Error  *state.Failure   `json:"error,omitempty"`
}

func (s fileCheckpointStore) Load(key executor.CheckpointKey) (pollState, bool, error) {
	if err := validateCheckpointKey(key); err != nil {
		return pollState{}, false, err
	}

	path := checkpointPath(key)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return pollState{}, false, nil
		}
		return pollState{}, false, fmt.Errorf("read %s: %w", path, err)
	}

	var document checkpointDocument
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return pollState{}, false, fmt.Errorf("decode %s: %w", path, err)
	}
	if err := ensureJSONEOF(decoder, path); err != nil {
		return pollState{}, false, err
	}
	if document.Version != checkpointVersion {
		return pollState{}, false, fmt.Errorf("decode %s: unsupported version %d", path, document.Version)
	}
	if err := document.State.validate(); err != nil {
		return pollState{}, false, fmt.Errorf("decode %s: %w", path, err)
	}

	return document.State, true, nil
}

func (s fileCheckpointStore) Save(key executor.CheckpointKey, poll pollState) error {
	if err := validateCheckpointKey(key); err != nil {
		return err
	}
	if err := poll.validate(); err != nil {
		return err
	}

	path := checkpointPath(key)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create checkpoint directory: %w", err)
	}

	document := checkpointDocument{
		Version: checkpointVersion,
		State:   poll,
	}
	payload, err := json.Marshal(document)
	if err != nil {
		return fmt.Errorf("marshal checkpoint: %w", err)
	}
	payload = append(payload, '\n')

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, payload, 0o644); err != nil {
		return fmt.Errorf("write checkpoint temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename checkpoint temp file: %w", err)
	}

	return nil
}

func (s fileCheckpointStore) Delete(key executor.CheckpointKey) error {
	if err := validateCheckpointKey(key); err != nil {
		return err
	}

	path := checkpointPath(key)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	if err := os.Remove(path + ".tmp"); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s.tmp: %w", path, err)
	}
	return nil
}

func checkpointPath(key executor.CheckpointKey) string {
	frameID := strings.ReplaceAll(key.FrameID, string(os.PathSeparator), "_")
	name := fmt.Sprintf("%s-step-%06d.json", frameID, key.StepIndex)
	return filepath.Join(key.RunDir, "checkpoints", name)
}

func validateCheckpointKey(key executor.CheckpointKey) error {
	if strings.TrimSpace(key.RunDir) == "" {
		return fmt.Errorf("checkpoint run directory is required")
	}
	if strings.TrimSpace(key.FrameID) == "" {
		return fmt.Errorf("checkpoint frame id is required")
	}
	if key.StepIndex < 0 {
		return fmt.Errorf("checkpoint step index must be non-negative")
	}
	return nil
}

func ensureJSONEOF(decoder *json.Decoder, path string) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("decode %s: unexpected extra JSON value", path)
		}
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func (p *pollState) validate() error {
	if p == nil {
		return fmt.Errorf("checkpoint state is required")
	}
	if p.ConfirmStarted.IsZero() {
		return fmt.Errorf("confirm_started is required")
	}

	if err := validateHTTPResponse(&p.Request, "request"); err != nil {
		return err
	}
	if err := p.Confirm.validate(); err != nil {
		return err
	}
	if p.Terminal != nil {
		if err := p.Terminal.validate(); err != nil {
			return err
		}
	}

	return nil
}

func (c *confirmState) validate() error {
	if c == nil {
		return fmt.Errorf("confirm state is required")
	}
	if c.Attempts < 0 {
		return fmt.Errorf("confirm.attempts must be non-negative")
	}
	if c.Attempts == 0 {
		if c.Response != nil {
			return fmt.Errorf("confirm.response must be null when attempts is zero")
		}
		if !c.LastAttempt.IsZero() {
			return fmt.Errorf("confirm.last_attempt must be empty when attempts is zero")
		}
		return nil
	}

	if c.Response == nil {
		return fmt.Errorf("confirm.response is required when attempts is positive")
	}
	if c.LastAttempt.IsZero() {
		return fmt.Errorf("confirm.last_attempt is required when attempts is positive")
	}
	if err := validateHTTPResponse(c.Response, "confirm.response"); err != nil {
		return err
	}

	return nil
}

func validateHTTPResponse(response *httpResponse, field string) error {
	if response == nil {
		return fmt.Errorf("%s is required", field)
	}
	if response.Status < 100 || response.Status > 999 {
		return fmt.Errorf("%s.status must be a valid HTTP status code", field)
	}
	if response.Headers == nil {
		response.Headers = map[string][]string{}
	}

	keys := make([]string, 0, len(response.Headers))
	for key := range response.Headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if response.Headers[key] == nil {
			response.Headers[key] = []string{}
		}
	}

	return nil
}

func (t *terminalState) validate() error {
	if t == nil {
		return fmt.Errorf("terminal is required")
	}

	switch t.Status {
	case state.StepStatusSucceeded:
		if t.Error != nil {
			return fmt.Errorf("terminal.error must be null for succeeded status")
		}
	case state.StepStatusFailed:
		if t.Error == nil {
			return fmt.Errorf("terminal.error is required for failed status")
		}
		if strings.TrimSpace(t.Error.Code) == "" {
			return fmt.Errorf("terminal.error.code is required")
		}
		if strings.TrimSpace(t.Error.Message) == "" {
			return fmt.Errorf("terminal.error.message is required")
		}
	default:
		return fmt.Errorf("terminal.status %q is unsupported", t.Status)
	}

	return nil
}

func (p pollState) value() map[string]any {
	value := map[string]any{
		"request": map[string]any{
			"response": responseValue(&p.Request),
		},
		"confirm": map[string]any{
			"attempts": p.Confirm.Attempts,
			"response": responseValue(p.Confirm.Response),
		},
	}
	return value
}

func responseValue(response *httpResponse) any {
	if response == nil {
		return nil
	}

	headers := make(map[string]any, len(response.Headers))
	keys := make([]string, 0, len(response.Headers))
	for key := range response.Headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		values := response.Headers[key]
		items := make([]any, len(values))
		for index, value := range values {
			items[index] = value
		}
		headers[key] = items
	}

	return map[string]any{
		"status":  response.Status,
		"headers": headers,
		"value":   jsonutil.CloneValue(response.Value),
	}
}

func terminalFromResult(result state.StepResult) *terminalState {
	terminal := &terminalState{
		Status: result.Status,
		Value:  jsonutil.CloneValue(result.Value),
		Error:  state.CloneFailure(result.Error),
	}
	return terminal
}

func (t terminalState) result() state.StepResult {
	return executor.NormalizeResult(state.StepResult{
		Status:    t.Status,
		Value:     jsonutil.CloneValue(t.Value),
		Error:     state.CloneFailure(t.Error),
		Artifacts: executor.EmptyArtifacts(),
	})
}
