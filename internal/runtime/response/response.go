package response

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"auto/internal/schema"
	"auto/internal/spec"
	"auto/internal/state"
)

const (
	codeInvalidResponse        = "invalid_response"
	codeInvalidResponseSchema  = "invalid_response_schema"
	codeResponseSchemaMismatch = "response_schema_mismatch"
)

type Resolver struct {
	schemas *schema.Registry
}

type config struct {
	from   source
	schema any
}

type source struct {
	kind     string
	artifact string
}

func NewResolver(schemas *schema.Registry) Resolver {
	return Resolver{schemas: schemas}
}

func (r Resolver) Apply(stepIndex int, step spec.Step, result state.StepResult) (state.StepResult, *state.Failure) {
	cfg, ok, err := load(step)
	if err != nil {
		return result, failure(codeInvalidResponse, stepIndex, "response is invalid", err)
	}
	if !ok {
		return result, nil
	}

	value, err := cfg.from.resolve(result)
	if err != nil {
		return result, failure(codeInvalidResponse, stepIndex, "response.from is invalid", err)
	}
	result.Value = cloneValue(value)

	if cfg.schema == nil {
		return result, nil
	}

	if r.schemas == nil {
		r.schemas = schema.NewRegistry(nil)
	}

	compiled, err := r.schemas.Compile(cfg.schema)
	if err != nil {
		return result, failure(codeInvalidResponseSchema, stepIndex, "response.schema is invalid", err)
	}
	if err := compiled.Validate(result.Value); err != nil {
		return result, failure(codeResponseSchemaMismatch, stepIndex, "response.schema rejected value", err)
	}

	return result, nil
}

func load(step spec.Step) (config, bool, error) {
	value, ok := step.Fields["response"]
	if !ok {
		return config{}, false, nil
	}

	fields, ok := value.(map[string]any)
	if !ok {
		return config{}, false, fmt.Errorf("must be a map")
	}

	cfg := config{
		from: source{kind: "value"},
	}

	if rawFrom, ok := fields["from"]; ok {
		from, err := parseSource(rawFrom)
		if err != nil {
			return config{}, false, err
		}
		cfg.from = from
	}

	if rawSchema, ok := fields["schema"]; ok {
		cfg.schema = rawSchema
	}

	return cfg, true, nil
}

func parseSource(value any) (source, error) {
	text, ok := value.(string)
	if !ok {
		return source{}, fmt.Errorf("from must be a string")
	}

	switch {
	case text == "value", text == "stdout", text == "stderr":
		return source{kind: text}, nil
	case strings.HasPrefix(text, "artifact:"):
		name := strings.TrimPrefix(text, "artifact:")
		if name == "" {
			return source{}, fmt.Errorf("artifact source must include a name")
		}
		return source{kind: "artifact", artifact: name}, nil
	default:
		return source{}, fmt.Errorf("unsupported source %q", text)
	}
}

func (s source) resolve(result state.StepResult) (any, error) {
	switch s.kind {
	case "value":
		return cloneValue(result.Value), nil
	case "stdout":
		return readArtifactValue("stdout", result.Artifacts.Stdout)
	case "stderr":
		return readArtifactValue("stderr", result.Artifacts.Stderr)
	case "artifact":
		path, ok := result.Artifacts.Files[s.artifact]
		if !ok || path == "" {
			return nil, fmt.Errorf("artifact %q was not produced", s.artifact)
		}
		return readArtifactValue(fmt.Sprintf("artifact %q", s.artifact), path)
	default:
		return nil, fmt.Errorf("unsupported source %q", s.kind)
	}
}

func readArtifactValue(label string, path string) (any, error) {
	if path == "" {
		return nil, fmt.Errorf("%s is unavailable", label)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", label, err)
	}
	return decodeTextValue(data), nil
}

func decodeTextValue(data []byte) any {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 {
		var decoded any
		if err := json.Unmarshal(trimmed, &decoded); err == nil {
			return decoded
		}
	}
	return string(data)
}

func failure(code string, stepIndex int, summary string, err error) *state.Failure {
	return &state.Failure{
		Code:    code,
		Message: fmt.Sprintf("step %d %s: %v", stepIndex, summary, err),
	}
}

func cloneValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		cloned := make(map[string]any, len(typed))
		for key, child := range typed {
			cloned[key] = cloneValue(child)
		}
		return cloned
	case []any:
		cloned := make([]any, len(typed))
		for index, child := range typed {
			cloned[index] = cloneValue(child)
		}
		return cloned
	default:
		return value
	}
}
