package runtime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/iw2rmb/amata/internal/jsonutil"
	"github.com/iw2rmb/amata/internal/schema"
	"github.com/iw2rmb/amata/internal/spec"
	"github.com/iw2rmb/amata/internal/state"
)

const (
	responseCodeInvalid        = "invalid_response"
	responseCodeInvalidSchema  = "invalid_response_schema"
	responseCodeSchemaMismatch = "response_schema_mismatch"
)

type responseResolver struct {
	schemas *schema.Registry
}

type responseConfig struct {
	from   responseSource
	schema any
}

type responseSource struct {
	kind     string
	artifact string
}

func newResponseResolver(schemas *schema.Registry) responseResolver {
	return responseResolver{schemas: schemas}
}

func (r responseResolver) apply(stepIndex int, specPath string, step spec.Step, result state.StepResult) (state.StepResult, *state.Failure) {
	cfg, ok, err := loadResponseConfig(step)
	if err != nil {
		return result, responseFailure(responseCodeInvalid, stepIndex, "response is invalid", err)
	}
	if !ok {
		return result, nil
	}

	value, err := cfg.from.resolve(result)
	if err != nil {
		return result, responseFailure(responseCodeInvalid, stepIndex, "response.from is invalid", err)
	}
	result.Value = jsonutil.CloneValue(value)

	if cfg.schema == nil {
		return result, nil
	}

	if r.schemas == nil {
		r.schemas = schema.NewRegistry(nil)
	}

	compiledSchema, err := resolveResponseSchema(cfg.schema, specPath)
	if err != nil {
		return result, responseFailure(responseCodeInvalidSchema, stepIndex, "response.schema is invalid", err)
	}

	compiled, err := r.schemas.Compile(compiledSchema)
	if err != nil {
		return result, responseFailure(responseCodeInvalidSchema, stepIndex, "response.schema is invalid", err)
	}
	if err := compiled.Validate(result.Value); err != nil {
		return result, responseFailure(responseCodeSchemaMismatch, stepIndex, "response.schema rejected value", err)
	}

	return result, nil
}

func resolveResponseSchema(rawSchema any, specPath string) (any, error) {
	path, ok, err := schema.ResolveResponseSchemaPath(rawSchema, specPath)
	if err != nil {
		return nil, err
	}
	if !ok {
		return rawSchema, nil
	}

	document, _, err := schema.LoadResponseSchemaFile(path)
	if err != nil {
		return nil, err
	}
	return document, nil
}

func loadResponseConfig(step spec.Step) (responseConfig, bool, error) {
	value, ok := step.Fields["response"]
	if !ok {
		return responseConfig{}, false, nil
	}

	fields, ok := value.(map[string]any)
	if !ok {
		return responseConfig{}, false, fmt.Errorf("must be a map")
	}

	cfg := responseConfig{
		from: responseSource{kind: "value"},
	}

	if rawFrom, ok := fields["from"]; ok {
		from, err := parseResponseSource(rawFrom)
		if err != nil {
			return responseConfig{}, false, err
		}
		cfg.from = from
	}

	if rawSchema, ok := fields["schema"]; ok {
		cfg.schema = rawSchema
	}

	return cfg, true, nil
}

func parseResponseSource(value any) (responseSource, error) {
	text, ok := value.(string)
	if !ok {
		return responseSource{}, fmt.Errorf("from must be a string")
	}

	switch {
	case text == "value", text == "stdout", text == "stderr", text == "stdout_lines", text == "stderr_lines":
		return responseSource{kind: text}, nil
	case strings.HasPrefix(text, "artifact:"):
		name := strings.TrimPrefix(text, "artifact:")
		if name == "" {
			return responseSource{}, fmt.Errorf("artifact source must include a name")
		}
		return responseSource{kind: "artifact", artifact: name}, nil
	default:
		return responseSource{}, fmt.Errorf("unsupported source %q", text)
	}
}

func (s responseSource) resolve(result state.StepResult) (any, error) {
	switch s.kind {
	case "value":
		return jsonutil.CloneValue(result.Value), nil
	case "stdout":
		return readResponseArtifactValue("stdout", result.Artifacts.Stdout)
	case "stderr":
		return readResponseArtifactValue("stderr", result.Artifacts.Stderr)
	case "stdout_lines":
		return readResponseArtifactLines("stdout", result.Artifacts.Stdout)
	case "stderr_lines":
		return readResponseArtifactLines("stderr", result.Artifacts.Stderr)
	case "artifact":
		path, ok := result.Artifacts.Files[s.artifact]
		if !ok || path == "" {
			return nil, fmt.Errorf("artifact %q was not produced", s.artifact)
		}
		return readResponseArtifactValue(fmt.Sprintf("artifact %q", s.artifact), path)
	default:
		return nil, fmt.Errorf("unsupported source %q", s.kind)
	}
}

func readResponseArtifactValue(label string, path string) (any, error) {
	if path == "" {
		return nil, fmt.Errorf("%s is unavailable", label)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", label, err)
	}
	return decodeResponseTextValue(data), nil
}

func decodeResponseTextValue(data []byte) any {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 {
		var decoded any
		if err := json.Unmarshal(trimmed, &decoded); err == nil {
			return decoded
		}
	}
	return string(data)
}

func readResponseArtifactLines(label string, path string) ([]any, error) {
	if path == "" {
		return nil, fmt.Errorf("%s is unavailable", label)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", label, err)
	}

	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return []any{}, nil
	}

	rawLines := strings.Split(text, "\n")
	lines := make([]any, 0, len(rawLines))
	for _, line := range rawLines {
		lines = append(lines, strings.TrimSuffix(line, "\r"))
	}
	return lines, nil
}

func responseFailure(code string, stepIndex int, summary string, err error) *state.Failure {
	return &state.Failure{
		Code:    code,
		Message: fmt.Sprintf("step %d %s: %v", stepIndex, summary, err),
	}
}
