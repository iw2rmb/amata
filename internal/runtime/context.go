package runtime

import (
	"path/filepath"

	"github.com/iw2rmb/amata/internal/jsonutil"
	"github.com/iw2rmb/amata/internal/state"
	"github.com/iw2rmb/amata/internal/workspace"
)

func specContext(path string) map[string]any {
	return map[string]any{
		"path": path,
		"dir":  filepath.Dir(path),
	}
}

func workspaceContext(config workspace.Config) map[string]any {
	return map[string]any{
		"root":      config.Root,
		"state_dir": config.StateDir,
	}
}

func buildRuntimeContext(config Config, previous *state.StepResult, lookup func(*state.StepRef) *state.StepResult, bindings map[string]any) map[string]any {
	ctx := map[string]any{
		"spec":      specContext(config.SpecPath),
		"workspace": workspaceContext(config.Workspace),
		"params":    jsonutil.CloneMap(config.Spec.Params),
		"prev":      previousContext(previous, lookup),
	}
	for key, value := range bindings {
		ctx[key] = jsonutil.CloneValue(value)
	}

	return map[string]any{"ctx": ctx}
}

func previousContext(previous *state.StepResult, lookup func(*state.StepRef) *state.StepResult) any {
	if previous == nil {
		return nil
	}
	return stepResultContext(*previous, lookup)
}

func stepResultContext(result state.StepResult, lookup func(*state.StepRef) *state.StepResult) map[string]any {
	ctx := map[string]any{
		"index":     result.Index,
		"type":      result.Type,
		"status":    string(result.Status),
		"value":     jsonutil.CloneValue(result.Value),
		"error":     failureContext(result.Error),
		"artifacts": artifactsContext(result.Artifacts),
	}
	if lookup != nil {
		ctx["prev"] = previousContext(lookup(result.Previous), lookup)
	}
	return ctx
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
