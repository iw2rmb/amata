package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"auto/internal/executor"
	"auto/internal/jsonutil"
	"auto/internal/schema"
)

func loadRequest(stepCtx executor.StepContext, providerName string, stepDir string) (Request, *Error) {
	commonDefaults, providerDefaults, defaultsErr := loadDefaults(stepCtx.Spec.Defaults, providerName)
	if defaultsErr != nil {
		return Request{}, defaultsErr
	}

	prompt, err := resolveRequiredString(stepCtx, selectValue(stepCtx.Step.Fields, providerDefaults, nil, "prompt"))
	if err != nil {
		return Request{}, invalidFieldError("prompt", err)
	}

	model, err := resolveRequiredString(stepCtx, selectValue(stepCtx.Step.Fields, providerDefaults, nil, "model"))
	if err != nil {
		return Request{}, invalidFieldError("model", err)
	}

	reasoning, err := resolveOptionalString(stepCtx, selectValue(stepCtx.Step.Fields, providerDefaults, nil, "reasoning"))
	if err != nil {
		return Request{}, invalidFieldError("reasoning", err)
	}

	cwd, err := resolveCWD(stepCtx, selectValue(stepCtx.Step.Fields, providerDefaults, commonDefaults, "cwd"))
	if err != nil {
		return Request{}, invalidFieldError("cwd", err)
	}

	env, err := resolveEnv(stepCtx, commonDefaults["env"], providerDefaults["env"], stepCtx.Step.Fields["env"])
	if err != nil {
		return Request{}, invalidFieldError("env", err)
	}

	structured, structuredErr := loadStructuredOutput(stepCtx, stepDir)
	if structuredErr != nil {
		return Request{}, structuredErr
	}

	return Request{
		Prompt:      prompt,
		Model:       model,
		Reasoning:   reasoning,
		CWD:         cwd,
		Env:         env,
		ArtifactDir: stepDir,
		Structured:  structured,
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
	for _, key := range sortedKeys(merged) {
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

func loadStructuredOutput(stepCtx executor.StepContext, stepDir string) (*StructuredOutput, *Error) {
	responseValue, ok := stepCtx.Step.Fields["response"]
	if !ok {
		return nil, nil
	}

	responseFields, ok := responseValue.(map[string]any)
	if !ok {
		return nil, &Error{
			Code:    "invalid_response",
			Message: "response must be a map",
		}
	}

	rawSchema, ok := responseFields["schema"]
	if !ok {
		return nil, nil
	}

	if rawFrom, ok := responseFields["from"]; ok {
		from, ok := rawFrom.(string)
		if !ok {
			return nil, &Error{
				Code:    "invalid_response",
				Message: "response.from must be a string",
			}
		}
		if from != "value" {
			return nil, nil
		}
	}

	document, jsonText, err := buildSchemaDocument(rawSchema, stepCtx.Spec.Schemas)
	if err != nil {
		return nil, &Error{
			Code:    "invalid_response_schema",
			Message: fmt.Sprintf("response.schema is invalid: %v", err),
		}
	}

	schemaPath := filepath.Join(stepDir, "response-schema.json")
	if err := os.WriteFile(schemaPath, []byte(jsonText), 0o644); err != nil {
		return nil, &Error{
			Code:    "artifact_capture_failed",
			Message: fmt.Sprintf("write response schema artifact: %v", err),
		}
	}

	return &StructuredOutput{
		Document:   document,
		JSON:       jsonText,
		SchemaPath: schemaPath,
	}, nil
}

func buildSchemaDocument(responseSchema any, workflowSchemas map[string]any) (any, string, error) {
	normalizedResponse, err := schema.Normalize(responseSchema)
	if err != nil {
		return nil, "", err
	}

	if document, ok := normalizedResponse.(map[string]any); ok {
		withSchemas := jsonutil.CloneMap(document)
		if len(workflowSchemas) > 0 {
			normalizedSchemas := make(map[string]any, len(workflowSchemas))
			for name, rawSchema := range workflowSchemas {
				normalized, err := schema.Normalize(rawSchema)
				if err != nil {
					return nil, "", fmt.Errorf("schemas.%s: %w", name, err)
				}
				normalizedSchemas[name] = normalized
			}
			withSchemas["schemas"] = normalizedSchemas
		}

		data, err := json.Marshal(withSchemas)
		if err != nil {
			return nil, "", err
		}
		return withSchemas, string(data), nil
	}

	data, err := json.Marshal(normalizedResponse)
	if err != nil {
		return nil, "", err
	}
	return normalizedResponse, string(data), nil
}

func invalidFieldError(field string, err error) *Error {
	return &Error{
		Code:    "invalid_agent",
		Message: fmt.Sprintf("%s is invalid: %v", field, err),
	}
}
