package executor

import "github.com/iw2rmb/amata/internal/state"

func Succeeded(value any) state.StepResult {
	return state.StepResult{
		Status:    state.StepStatusSucceeded,
		Value:     value,
		Artifacts: EmptyArtifacts(),
	}
}

func Failed(code string, message string) state.StepResult {
	return state.StepResult{
		Status: state.StepStatusFailed,
		Error: &state.Failure{
			Code:    code,
			Message: message,
		},
		Artifacts: EmptyArtifacts(),
	}
}

func Skipped() state.StepResult {
	return state.StepResult{
		Status:    state.StepStatusSkipped,
		Artifacts: EmptyArtifacts(),
	}
}

func EmptyArtifacts() state.Artifacts {
	return state.Artifacts{
		Files: map[string]string{},
	}
}

func NormalizeResult(result state.StepResult) state.StepResult {
	if result.Artifacts.Files == nil {
		result.Artifacts.Files = map[string]string{}
	}
	return result
}
