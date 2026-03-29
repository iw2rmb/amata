package dataget

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/itchyny/gojq"

	"github.com/iw2rmb/amata/internal/executor"
	"github.com/iw2rmb/amata/internal/state"
	"gopkg.in/yaml.v3"
)

type Executor struct{}

func New() executor.Executor {
	return &Executor{}
}

func (e *Executor) Execute(_ context.Context, stepCtx executor.StepContext) state.StepResult {
	filePath, err := resolveFilePath(stepCtx)
	if err != nil {
		return executor.Failed("invalid_file", fmt.Sprintf("step %d: %v", stepCtx.StepIndex, err))
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return executor.Failed("read_failed", fmt.Sprintf("step %d: read %s: %v", stepCtx.StepIndex, filePath, err))
	}

	format, err := resolveFormat(stepCtx, filePath)
	if err != nil {
		return executor.Failed("invalid_format", fmt.Sprintf("step %d: %v", stepCtx.StepIndex, err))
	}

	document, err := parseDocument(format, data)
	if err != nil {
		return executor.Failed("parse_failed", fmt.Sprintf("step %d: %v", stepCtx.StepIndex, err))
	}

	query, err := resolveQuery(stepCtx)
	if err != nil {
		return executor.Failed("invalid_query", fmt.Sprintf("step %d: %v", stepCtx.StepIndex, err))
	}

	result, err := runQuery(query, document)
	if err != nil {
		return executor.Failed("query_failed", fmt.Sprintf("step %d: %v", stepCtx.StepIndex, err))
	}
	if result == nil {
		if fallback, ok := stepCtx.Step.Fields["default"]; ok {
			resolved, err := stepCtx.Runtime.Resolve(fallback)
			if err != nil {
				return executor.Failed("invalid_default", fmt.Sprintf("step %d: %v", stepCtx.StepIndex, err))
			}
			return executor.Succeeded(resolved)
		}
		return executor.Failed("query_empty", fmt.Sprintf("step %d: query returned no results", stepCtx.StepIndex))
	}

	return executor.Succeeded(result)
}

func resolveFilePath(stepCtx executor.StepContext) (string, error) {
	raw, ok := stepCtx.Step.Fields["file"]
	if !ok {
		return "", fmt.Errorf("file is required")
	}

	resolved, err := stepCtx.Runtime.ResolveString(raw)
	if err != nil {
		return "", fmt.Errorf("file: %w", err)
	}

	if filepath.IsAbs(resolved) {
		return filepath.Clean(resolved), nil
	}

	cwd, err := executor.ResolveCWD(stepCtx)
	if err != nil {
		return "", fmt.Errorf("cwd: %w", err)
	}
	return filepath.Clean(filepath.Join(cwd, resolved)), nil
}

func resolveFormat(stepCtx executor.StepContext, filePath string) (string, error) {
	if raw, ok := stepCtx.Step.Fields["format"]; ok {
		resolved, err := stepCtx.Runtime.ResolveString(raw)
		if err != nil {
			return "", fmt.Errorf("format: %w", err)
		}
		switch strings.ToLower(strings.TrimSpace(resolved)) {
		case "json":
			return "json", nil
		case "yaml", "yml":
			return "yaml", nil
		default:
			return "", fmt.Errorf("format must be json or yaml")
		}
	}

	switch strings.ToLower(filepath.Ext(filePath)) {
	case ".yaml", ".yml":
		return "yaml", nil
	case ".json":
		return "json", nil
	default:
		return "yaml", nil
	}
}

func resolveQuery(stepCtx executor.StepContext) (string, error) {
	raw, ok := stepCtx.Step.Fields["query"]
	if !ok {
		return ".", nil
	}

	resolved, err := stepCtx.Runtime.ResolveString(raw)
	if err != nil {
		return "", err
	}
	return resolved, nil
}

func parseDocument(format string, data []byte) (any, error) {
	switch format {
	case "json":
		var value any
		decoder := json.NewDecoder(strings.NewReader(string(data)))
		decoder.UseNumber()
		if err := decoder.Decode(&value); err != nil {
			return nil, fmt.Errorf("decode json: %w", err)
		}
		return value, nil
	case "yaml":
		var value any
		if err := yaml.Unmarshal(data, &value); err != nil {
			return nil, fmt.Errorf("decode yaml: %w", err)
		}
		return value, nil
	default:
		return nil, fmt.Errorf("unsupported format %q", format)
	}
}

func runQuery(query string, document any) (any, error) {
	parsed, err := gojq.Parse(query)
	if err != nil {
		return nil, fmt.Errorf("parse query %q: %w", query, err)
	}

	code, err := gojq.Compile(parsed)
	if err != nil {
		return nil, fmt.Errorf("compile query %q: %w", query, err)
	}

	iter := code.Run(document)
	var (
		result any
		count  int
	)
	for {
		value, ok := iter.Next()
		if !ok {
			break
		}
		if err, ok := value.(error); ok {
			return nil, fmt.Errorf("run query %q: %w", query, err)
		}
		result = value
		count++
		if count > 1 {
			return nil, fmt.Errorf("query produced multiple results (%d); wrap it in [] when multiple values are expected", count)
		}
	}
	if count == 0 {
		return nil, nil
	}
	return result, nil
}
