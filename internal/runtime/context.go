package runtime

import (
	"auto/internal/state"
	"auto/internal/workspace"
)

func buildRuntimeContext(config Config, previous *state.StepResult) map[string]any {
	return map[string]any{
		"ctx": map[string]any{
			"workspace": workspaceContext(config.Workspace),
			"params":    cloneMap(config.Spec.Params),
			"prev":      previousContext(previous),
		},
	}
}

func workspaceContext(config workspace.Config) map[string]any {
	return map[string]any{
		"root":      config.Root,
		"state_dir": config.StateDir,
	}
}

func previousContext(previous *state.StepResult) any {
	if previous == nil {
		return nil
	}
	return stepResultContext(*previous)
}

func stepResultContext(result state.StepResult) map[string]any {
	return map[string]any{
		"index":     result.Index,
		"id":        result.ID,
		"type":      result.Type,
		"status":    string(result.Status),
		"value":     cloneValue(result.Value),
		"error":     failureContext(result.Error),
		"artifacts": artifactsContext(result.Artifacts),
	}
}

func failureContext(failure *state.Failure) any {
	if failure == nil {
		return nil
	}
	return map[string]any{
		"code":    failure.Code,
		"message": failure.Message,
	}
}

func artifactsContext(artifacts state.Artifacts) map[string]any {
	files := make(map[string]any, len(artifacts.Files))
	for name, path := range artifacts.Files {
		files[name] = path
	}
	return map[string]any{
		"stdout": artifacts.Stdout,
		"stderr": artifacts.Stderr,
		"files":  files,
	}
}

func cloneMap(source map[string]any) map[string]any {
	if source == nil {
		return map[string]any{}
	}
	cloned := make(map[string]any, len(source))
	for key, value := range source {
		cloned[key] = cloneValue(value)
	}
	return cloned
}

func cloneValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneMap(typed)
	case []any:
		cloned := make([]any, len(typed))
		for index, item := range typed {
			cloned[index] = cloneValue(item)
		}
		return cloned
	default:
		return value
	}
}
