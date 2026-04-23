package runtime

import (
	"os"
	"path/filepath"
	"strings"

	exprruntime "github.com/iw2rmb/amata/internal/expr"
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

func environmentContext() map[string]any {
	ctx := map[string]any{}
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			continue
		}
		ctx[key] = value
	}
	return ctx
}

func newStepRuntime(config Config, previous *state.StepResult, lookup func(*state.StepRef) *state.StepResult, bindings map[string]any) exprruntime.Runtime {
	return exprruntime.NewRuntime(buildRuntimeContext(config, previous, lookup, bindings))
}

func buildRuntimeContext(config Config, previous *state.StepResult, lookup func(*state.StepRef) *state.StepResult, bindings map[string]any) map[string]any {
	ctx := map[string]any{
		"spec":      specContext(config.SpecPath),
		"workspace": workspaceContext(config.Workspace),
		"params":    jsonutil.CloneMap(config.Spec.Params),
		"env":       environmentContext(),
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
	value, meta := stepResultValueAndMeta(result)
	ctx := map[string]any{
		"index":     result.Index,
		"type":      result.Type,
		"status":    string(result.Status),
		"value":     value,
		"error":     failureContext(result.Error),
		"artifacts": artifactsContext(result.Artifacts),
	}
	if meta != nil {
		ctx["meta"] = meta
	}
	if lookup != nil {
		ctx["prev"] = previousContext(lookup(result.Previous), lookup)
	}
	return ctx
}

func stepResultValueAndMeta(result state.StepResult) (any, any) {
	value := jsonutil.CloneValue(result.Value)
	switch result.Type {
	case "call", "switch", "for_each":
		fields, ok := value.(map[string]any)
		if !ok {
			return value, nil
		}

		payload, hasPayload := fields["value"]
		if !hasPayload {
			return value, nil
		}
		payload = unwrapNestedControlPayload(payload)

		meta := make(map[string]any, len(fields)-1)
		for key, raw := range fields {
			if key == "value" {
				continue
			}
			meta[key] = jsonutil.CloneValue(raw)
		}
		if len(meta) == 0 {
			return payload, nil
		}
		return payload, meta
	default:
		return value, nil
	}
}

func unwrapNestedControlPayload(payload any) any {
	current := payload
	for {
		fields, ok := current.(map[string]any)
		if !ok || !isControlResultShape(fields) {
			return current
		}

		next, hasNext := fields["value"]
		if !hasNext {
			return current
		}
		current = next
	}
}

func isControlResultShape(fields map[string]any) bool {
	_, hasStatus := fields["status"]
	_, hasError := fields["error"]
	_, hasArtifacts := fields["artifacts"]
	if !hasStatus || !hasError || !hasArtifacts {
		return false
	}

	for _, key := range []string{"flow", "matched", "case", "count", "index", "item", "as"} {
		if _, ok := fields[key]; ok {
			return true
		}
	}
	return false
}

func failureContext(failure *state.Failure) any {
	if failure == nil {
		return nil
	}
	context := map[string]any{
		"code":    failure.Code,
		"message": failure.Message,
	}
	if len(failure.Details) > 0 {
		context["details"] = jsonutil.CloneMap(failure.Details)
	}
	return context
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
