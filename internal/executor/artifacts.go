package executor

import (
	"fmt"
	"path/filepath"
)

func StepArtifactDir(runDir string, stepIndex int, stepID string) string {
	label := fmt.Sprintf("step-%02d", stepIndex)
	if stepID == "" {
		return filepath.Join(runDir, "artifacts", label)
	}
	return filepath.Join(runDir, "artifacts", label+"-"+SanitizeArtifactName(stepID))
}

func SanitizeArtifactName(value string) string {
	replaced := make([]rune, 0, len(value))
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			replaced = append(replaced, r)
		case r >= 'A' && r <= 'Z':
			replaced = append(replaced, r)
		case r >= '0' && r <= '9':
			replaced = append(replaced, r)
		case r == '-', r == '_', r == '.':
			replaced = append(replaced, r)
		default:
			replaced = append(replaced, '-')
		}
	}

	for len(replaced) > 0 && replaced[0] == '-' {
		replaced = replaced[1:]
	}
	for len(replaced) > 0 && replaced[len(replaced)-1] == '-' {
		replaced = replaced[:len(replaced)-1]
	}
	if len(replaced) == 0 {
		return "artifact"
	}
	return string(replaced)
}
