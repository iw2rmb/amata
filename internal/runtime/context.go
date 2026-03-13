package runtime

import (
	"auto/internal/jsonutil"
	"auto/internal/state"
	"auto/internal/workspace"
)

func buildRuntimeContext(config Config, previous *state.StepResult) map[string]any {
	return map[string]any{
		"ctx": map[string]any{
			"workspace": workspaceContext(config.Workspace),
			"params":    jsonutil.CloneMap(config.Spec.Params),
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
		"type":      result.Type,
		"status":    string(result.Status),
		"value":     jsonutil.CloneValue(result.Value),
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
