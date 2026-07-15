package agent

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"

	"github.com/iw2rmb/amata/internal/jsonutil"
)

// ProviderErrorObserver proxies provider stdout while extracting provider error
// envelopes and emitting normalized error events to stderr.
type ProviderErrorObserver struct {
	stdout  io.Writer
	stderr  io.Writer
	pending []byte
	details map[string]any
}

func NewProviderErrorObserver(stdout io.Writer, stderr io.Writer) *ProviderErrorObserver {
	return &ProviderErrorObserver{
		stdout: stdout,
		stderr: stderr,
	}
}

func (o *ProviderErrorObserver) Write(p []byte) (int, error) {
	if o == nil {
		return len(p), nil
	}

	written := len(p)
	var writeErr error
	if o.stdout != nil {
		written, writeErr = o.stdout.Write(p)
	}
	if written > 0 {
		if err := o.consume(p[:written]); err != nil && writeErr == nil {
			writeErr = err
		}
	}
	return written, writeErr
}

func (o *ProviderErrorObserver) Close() error {
	if o == nil || len(o.pending) == 0 {
		return nil
	}
	line := append([]byte(nil), o.pending...)
	o.pending = nil
	return o.processLine(line)
}

func (o *ProviderErrorObserver) ProviderErrorDetails() map[string]any {
	if o == nil || len(o.details) == 0 {
		return nil
	}
	return jsonutil.CloneMap(o.details)
}

func (o *ProviderErrorObserver) consume(chunk []byte) error {
	if len(chunk) == 0 {
		return nil
	}
	o.pending = append(o.pending, chunk...)
	for {
		index := bytes.IndexByte(o.pending, '\n')
		if index < 0 {
			return nil
		}
		line := append([]byte(nil), o.pending[:index]...)
		o.pending = o.pending[index+1:]
		if err := o.processLine(line); err != nil {
			return err
		}
	}
}

func (o *ProviderErrorObserver) processLine(line []byte) error {
	normalized, details, matched, ok := normalizeProviderErrorLine(line)
	if !matched {
		return nil
	}
	if len(details) > 0 {
		o.details = jsonutil.CloneMap(details)
	}
	if o.stderr == nil {
		return nil
	}

	toWrite := normalized
	if !ok {
		toWrite = bytes.TrimSpace(line)
	}
	if len(toWrite) == 0 {
		return nil
	}
	if _, err := o.stderr.Write(toWrite); err != nil {
		return err
	}
	_, err := o.stderr.Write([]byte("\n"))
	return err
}

func normalizeProviderErrorLine(line []byte) ([]byte, map[string]any, bool, bool) {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 {
		return nil, nil, false, false
	}

	var event map[string]any
	if err := json.Unmarshal(trimmed, &event); err != nil {
		return nil, nil, false, false
	}

	eventType, _ := event["type"].(string)
	if eventType != "error" {
		return nil, nil, false, false
	}

	providerError := extractProviderError(event)
	if providerError == nil {
		message, _ := event["message"].(string)
		message = strings.TrimSpace(message)
		if message == "" {
			return nil, nil, true, false
		}
		return nil, map[string]any{
			"message": message,
			"type":    "",
			"param":   nil,
			"code":    "",
		}, true, false
	}

	details := map[string]any{
		"message": stringValue(providerError, "message"),
		"type":    stringValue(providerError, "type"),
		"param":   providerError["param"],
		"code":    stringValue(providerError, "code"),
	}

	normalized, err := json.Marshal(map[string]any{
		"type":  "error",
		"error": details,
	})
	if err != nil {
		return nil, details, true, false
	}
	return normalized, details, true, true
}

func extractProviderError(event map[string]any) map[string]any {
	if value, ok := event["error"].(map[string]any); ok {
		return value
	}

	rawMessage, _ := event["message"].(string)
	rawMessage = strings.TrimSpace(rawMessage)
	if rawMessage == "" {
		return nil
	}

	var decoded any
	if err := json.Unmarshal([]byte(rawMessage), &decoded); err != nil {
		return nil
	}

	decodedMap, ok := decoded.(map[string]any)
	if !ok {
		return nil
	}
	if value, ok := decodedMap["error"].(map[string]any); ok {
		return value
	}
	if hasProviderErrorFields(decodedMap) {
		return decodedMap
	}
	return nil
}

func hasProviderErrorFields(value map[string]any) bool {
	for _, key := range []string{"message", "type", "param", "code"} {
		if _, ok := value[key]; ok {
			return true
		}
	}
	return false
}

func stringValue(value map[string]any, key string) string {
	text, _ := value[key].(string)
	return text
}
