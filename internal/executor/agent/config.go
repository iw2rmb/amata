package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/iw2rmb/amata/internal/executor"
	exprruntime "github.com/iw2rmb/amata/internal/expr"
	"github.com/iw2rmb/amata/internal/jsonutil"
	"github.com/iw2rmb/amata/internal/schema"
	templateapi "github.com/iw2rmb/amata/internal/template"
)

type ResolvedStep struct {
	Prompt    string
	Model     string
	Reasoning string
	CWD       string
	Env       map[string]string
}

var defaultClaudeResponseSchema = map[string]any{
	"type":                 "object",
	"additionalProperties": false,
	"required":             []any{"summary"},
	"properties": map[string]any{
		"summary": map[string]any{
			"type":     "string",
			"$comment": "One-liner summary",
		},
	},
}

func loadRequest(stepCtx executor.StepContext, providerName string, stepDir string) (Request, *Error) {
	resolved, resolvedErr := ResolveStep(stepCtx, providerName)
	if resolvedErr != nil {
		return Request{}, resolvedErr
	}

	structured, structuredErr := loadStructuredOutput(stepCtx, providerName, stepDir)
	if structuredErr != nil {
		return Request{}, structuredErr
	}

	prompt := resolved.Prompt
	if stepCtx.ContinuationPrompt != "" {
		prompt = stepCtx.ContinuationPrompt
	}

	return Request{
		Prompt:                prompt,
		Model:                 resolved.Model,
		Reasoning:             resolved.Reasoning,
		CWD:                   resolved.CWD,
		Env:                   resolved.Env,
		ArtifactDir:           stepDir,
		Structured:            structured,
		ContinuationSessionID: stepCtx.ContinuationSessionID,
	}, nil
}

func ResolveStep(stepCtx executor.StepContext, providerName string) (ResolvedStep, *Error) {
	commonDefaults, providerDefaults, defaultsErr := loadDefaults(stepCtx.Spec.Defaults, providerName)
	if defaultsErr != nil {
		return ResolvedStep{}, defaultsErr
	}

	prompt, err := resolveRequiredString(stepCtx, selectValue(stepCtx.Step.Fields, providerDefaults, nil, "prompt"))
	if err != nil {
		return ResolvedStep{}, invalidFieldError(stepCtx, "prompt", err)
	}

	model, err := resolveAgentModel(stepCtx, providerName, selectValue(stepCtx.Step.Fields, providerDefaults, nil, "model"))
	if err != nil {
		return ResolvedStep{}, invalidFieldError(stepCtx, "model", err)
	}

	reasoning, err := resolveOptionalString(stepCtx, selectValue(stepCtx.Step.Fields, providerDefaults, nil, "reasoning"))
	if err != nil {
		return ResolvedStep{}, invalidFieldError(stepCtx, "reasoning", err)
	}

	cwd, err := resolveCWD(stepCtx, selectValue(stepCtx.Step.Fields, providerDefaults, commonDefaults, "cwd"))
	if err != nil {
		return ResolvedStep{}, invalidFieldError(stepCtx, "cwd", err)
	}

	env, err := resolveEnv(stepCtx, commonDefaults["env"], providerDefaults["env"], stepCtx.Step.Fields["env"])
	if err != nil {
		return ResolvedStep{}, invalidFieldError(stepCtx, "env", err)
	}

	return ResolvedStep{
		Prompt:    prompt,
		Model:     model,
		Reasoning: reasoning,
		CWD:       cwd,
		Env:       env,
	}, nil
}

func loadDefaults(defaults map[string]any, providerName string) (map[string]any, map[string]any, *Error) {
	if len(defaults) == 0 {
		return map[string]any{}, map[string]any{}, nil
	}

	common := jsonutil.CloneMap(defaults)
	delete(common, "executors")

	providerDefaults := map[string]any{}
	rawExecutors, ok := defaults["executors"]
	if !ok {
		return common, providerDefaults, nil
	}

	executors, ok := rawExecutors.(map[string]any)
	if !ok {
		return nil, nil, &Error{
			Code:    "invalid_defaults",
			Message: "defaults.executors must be a map",
		}
	}

	rawProvider, ok := executors[providerName]
	if !ok {
		return common, providerDefaults, nil
	}
	providerDefaults, ok = rawProvider.(map[string]any)
	if !ok {
		return nil, nil, &Error{
			Code:    "invalid_defaults",
			Message: fmt.Sprintf("defaults.executors.%s must be a map", providerName),
		}
	}

	return common, jsonutil.CloneMap(providerDefaults), nil
}

func selectValue(stepFields map[string]any, providerDefaults map[string]any, commonDefaults map[string]any, key string) any {
	if value, ok := stepFields[key]; ok {
		return value
	}
	if value, ok := providerDefaults[key]; ok {
		return value
	}
	if value, ok := commonDefaults[key]; ok {
		return value
	}
	return nil
}

func resolveRequiredString(stepCtx executor.StepContext, raw any) (string, error) {
	if raw == nil {
		return "", fmt.Errorf("is required")
	}
	return resolveString(stepCtx, raw, false)
}

func resolveOptionalString(stepCtx executor.StepContext, raw any) (string, error) {
	if raw == nil {
		return "", nil
	}
	return resolveString(stepCtx, raw, true)
}

func resolveAgentModel(stepCtx executor.StepContext, providerName string, raw any) (string, error) {
	if providerName == "codex" {
		return resolveOptionalString(stepCtx, raw)
	}
	return resolveRequiredString(stepCtx, raw)
}

func resolveString(stepCtx executor.StepContext, raw any, allowEmpty bool) (string, error) {
	value, err := stepCtx.Runtime.Resolve(raw)
	if err != nil {
		return "", err
	}

	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("must resolve to a string")
	}
	if !allowEmpty && text == "" {
		return "", fmt.Errorf("must not be empty")
	}
	return text, nil
}

func resolveCWD(stepCtx executor.StepContext, raw any) (string, error) {
	if raw == nil {
		return stepCtx.Workspace.Root, nil
	}

	text, err := resolveString(stepCtx, raw, false)
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(text) {
		return filepath.Clean(text), nil
	}
	return filepath.Clean(filepath.Join(stepCtx.Workspace.Root, text)), nil
}

func resolveEnv(stepCtx executor.StepContext, values ...any) (map[string]string, error) {
	merged := map[string]any{}
	for _, raw := range values {
		if raw == nil {
			continue
		}

		current, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("must be a map of environment variable names to values")
		}

		keys := make([]string, 0, len(current))
		for key := range current {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		for _, key := range keys {
			merged[key] = current[key]
		}
	}

	if len(merged) == 0 {
		return map[string]string{}, nil
	}

	resolved := make(map[string]string, len(merged))
	for _, key := range jsonutil.SortedKeys(merged) {
		if key == "" {
			return nil, fmt.Errorf("environment variable names must not be empty")
		}

		text, err := resolveString(stepCtx, merged[key], true)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		resolved[key] = text
	}

	return resolved, nil
}

func loadStructuredOutput(stepCtx executor.StepContext, providerName string, stepDir string) (*StructuredOutput, *Error) {
	responseValue, ok := stepCtx.Step.Fields["response"]
	if !ok && providerName != "claude" {
		return nil, nil
	}

	responseFields := map[string]any{}
	if ok {
		var casted bool
		responseFields, casted = responseValue.(map[string]any)
		if !casted {
			return nil, &Error{
				Code:    "invalid_response",
				Message: "response must be a map",
			}
		}
	}

	rawSchema, hasSchema := responseFields["schema"]

	if rawFrom, ok := responseFields["from"]; ok {
		from, ok := rawFrom.(string)
		if !ok {
			return nil, &Error{
				Code:    "invalid_response",
				Message: "response.from must be a string",
			}
		}
		if from != "value" && providerName != "claude" {
			return nil, nil
		}
	}

	if !hasSchema {
		if providerName != "claude" {
			return nil, nil
		}
		rawSchema = jsonutil.CloneValue(defaultClaudeResponseSchema)
	}

	document, jsonText, schemaPath, err := buildStructuredSchema(stepCtx, providerName, stepDir, rawSchema)
	if err != nil {
		return nil, &Error{
			Code:    "invalid_response_schema",
			Message: fmt.Sprintf("response.schema is invalid: %v", err),
		}
	}

	return &StructuredOutput{
		Document:   document,
		JSON:       jsonText,
		SchemaPath: schemaPath,
	}, nil
}

func buildStructuredSchema(stepCtx executor.StepContext, providerName string, stepDir string, rawSchema any) (any, string, string, error) {
	if sourcePath, ok, err := schema.ResolveResponseSchemaPath(rawSchema, stepCtx.SpecPath); err != nil {
		return nil, "", "", err
	} else if ok {
		document, jsonText, err := schema.LoadResponseSchemaFile(sourcePath)
		if err != nil {
			return nil, "", "", err
		}
		if providerName == "codex" {
			validated, err := schema.ValidateProviderDocument(document)
			if err != nil {
				return nil, "", "", err
			}
			document = validated
			data, err := json.Marshal(document)
			if err != nil {
				return nil, "", "", err
			}
			schemaPath := filepath.Join(stepDir, "response-schema.json")
			if err := os.WriteFile(schemaPath, data, 0o644); err != nil {
				return nil, "", "", fmt.Errorf("write response schema artifact: %w", err)
			}
			return document, string(data), schemaPath, nil
		}
		return document, jsonText, sourcePath, nil
	}

	document, err := schema.ExpandedDocument(rawSchema, stepCtx.Spec.Schemas)
	if err != nil {
		return nil, "", "", err
	}
	if providerName == "codex" {
		validated, err := schema.ValidateProviderDocument(document)
		if err != nil {
			return nil, "", "", err
		}
		document = validated
	}

	data, err := json.Marshal(document)
	if err != nil {
		return nil, "", "", err
	}
	schemaPath := filepath.Join(stepDir, "response-schema.json")
	if err := os.WriteFile(schemaPath, data, 0o644); err != nil {
		return nil, "", "", fmt.Errorf("write response schema artifact: %w", err)
	}
	return document, string(data), schemaPath, nil
}

func invalidFieldError(stepCtx executor.StepContext, field string, err error) *Error {
	details := map[string]any{
		"field":     field,
		"flow":      stepCtx.FlowName,
		"stepIndex": stepCtx.StepIndex,
		"stepType":  stepCtx.Step.ExecutorType(),
	}
	if stepCtx.Step.ID != "" {
		details["stepID"] = stepCtx.Step.ID
	}

	rootCause := err
	if exprErr := (*templateapi.ExpressionError)(nil); errors.As(err, &exprErr) {
		details["expression"] = exprErr.Expression
		details["expressionIndex"] = exprErr.Index + 1
		rootCause = exprErr.Cause
	}
	if evalErr := (*exprruntime.EvaluationError)(nil); errors.As(rootCause, &evalErr) {
		details["expression"] = evalErr.Expression
		rootCause = evalErr.Cause
	}
	if rootCause == nil {
		rootCause = err
	}
	details["cause"] = rootCause.Error()

	locator := stepLocator(stepCtx)
	message := fmt.Sprintf("%s is invalid at %s: %v", field, locator, rootCause)
	if expression, ok := details["expression"].(string); ok && expression != "" {
		message = fmt.Sprintf("%s is invalid at %s: expression %q failed: %v", field, locator, expression, rootCause)
	}
	if hint := failureHint(rootCause.Error()); hint != "" {
		details["hint"] = hint
		message = fmt.Sprintf("%s. hint: %s", message, hint)
	}

	return &Error{
		Code:    "invalid_agent",
		Message: message,
		Details: details,
	}
}

func stepLocator(stepCtx executor.StepContext) string {
	parts := []string{}
	if stepCtx.FlowName != "" {
		parts = append(parts, fmt.Sprintf("flow %q", stepCtx.FlowName))
	}
	parts = append(parts, fmt.Sprintf("step %d", stepCtx.StepIndex))
	if stepType := stepCtx.Step.ExecutorType(); stepType != "" {
		parts = append(parts, fmt.Sprintf("executor %q", stepType))
	}
	if stepCtx.Step.ID != "" {
		parts = append(parts, fmt.Sprintf("id %q", stepCtx.Step.ID))
	}
	return strings.Join(parts, ", ")
}

func failureHint(cause string) string {
	if strings.Contains(cause, "unknown binary op: string + object") {
		return "expected string list items; in YAML arrays an unquoted ':' can create an object value"
	}
	return ""
}
