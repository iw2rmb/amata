package shell

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"auto/internal/executor"
	"auto/internal/state"
)

type Executor struct{}

type namedFile struct {
	name   string
	source string
}

func New() executor.Executor {
	return &Executor{}
}

func (e *Executor) Execute(ctx context.Context, stepCtx executor.StepContext) state.StepResult {
	command, err := resolveCommand(stepCtx, stepCtx.Step.Fields["command"])
	if err != nil {
		return executor.Failed("invalid_command", fmt.Sprintf("step %d: %v", stepCtx.StepIndex, err))
	}

	cwd, err := executor.ResolveCWD(stepCtx)
	if err != nil {
		return executor.Failed("invalid_cwd", fmt.Sprintf("step %d: %v", stepCtx.StepIndex, err))
	}

	files, err := resolveNamedFiles(stepCtx)
	if err != nil {
		return executor.Failed("invalid_files", fmt.Sprintf("step %d: %v", stepCtx.StepIndex, err))
	}

	stepDir := executor.StepArtifactDir(stepCtx.RunDir, stepCtx.StepIndex, stepCtx.Step.ID, stepCtx.ExecutionLabel)
	if err := os.MkdirAll(stepDir, 0o755); err != nil {
		return executor.Failed("artifact_dir_failed", fmt.Sprintf("step %d: create artifact directory: %v", stepCtx.StepIndex, err))
	}

	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Dir = cwd

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()

	artifacts, artifactErr := captureArtifacts(stepDir, stdout.Bytes(), stderr.Bytes(), files)
	result := executor.Succeeded(map[string]any{
		"exitCode": exitCode(cmd.ProcessState),
	})
	result.Artifacts = artifacts
	if artifactErr != nil {
		result.Status = state.StepStatusFailed
		result.Error = &state.Failure{
			Code:    "artifact_capture_failed",
			Message: fmt.Sprintf("step %d: capture artifacts: %v", stepCtx.StepIndex, artifactErr),
		}
		return result
	}

	if runErr == nil {
		return result
	}

	result.Status = state.StepStatusFailed
	result.Error = &state.Failure{
		Code:    "shell_failed",
		Message: fmt.Sprintf("step %d command failed: %v", stepCtx.StepIndex, runErr),
	}
	return result
}

func resolveCommand(stepCtx executor.StepContext, value any) ([]string, error) {
	switch command := value.(type) {
	case string:
		resolved, err := stepCtx.Runtime.ResolveString(command)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(resolved) == "" {
			return nil, fmt.Errorf("command must not be empty")
		}
		return []string{"sh", "-lc", resolved}, nil
	case []any:
		if len(command) == 0 {
			return nil, fmt.Errorf("command array must not be empty")
		}
		args := make([]string, 0, len(command))
		for index, part := range command {
			text, err := stepCtx.Runtime.ResolveString(part)
			if err != nil {
				return nil, fmt.Errorf("command[%d]: %w", index, err)
			}
			args = append(args, text)
		}
		return args, nil
	default:
		return nil, fmt.Errorf("command must be a string or string array")
	}
}

func captureArtifacts(
	stepDir string,
	stdout []byte,
	stderr []byte,
	files []namedFile,
) (state.Artifacts, error) {
	artifacts := executor.EmptyArtifacts()
	stdoutPath := filepath.Join(stepDir, "stdout.txt")
	if err := os.WriteFile(stdoutPath, stdout, 0o644); err != nil {
		return artifacts, err
	}
	artifacts.Stdout = stdoutPath

	stderrPath := filepath.Join(stepDir, "stderr.txt")
	if err := os.WriteFile(stderrPath, stderr, 0o644); err != nil {
		return artifacts, err
	}
	artifacts.Stderr = stderrPath

	capturedFiles, err := captureNamedFiles(stepDir, files)
	artifacts.Files = capturedFiles
	if err != nil {
		return artifacts, err
	}

	return artifacts, nil
}

func resolveNamedFiles(stepCtx executor.StepContext) ([]namedFile, error) {
	value, ok := stepCtx.Step.Fields["files"]
	if !ok {
		return nil, nil
	}

	rawFiles, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("files must be a map of artifact names to paths")
	}

	files := make([]namedFile, 0, len(rawFiles))
	for name, rawPath := range rawFiles {
		source, err := stepCtx.Runtime.ResolveString(rawPath)
		if err != nil {
			return nil, fmt.Errorf("files[%q]: %w", name, err)
		}
		if !filepath.IsAbs(source) {
			source = filepath.Join(stepCtx.Workspace.Root, source)
		}
		files = append(files, namedFile{
			name:   name,
			source: filepath.Clean(source),
		})
	}

	return files, nil
}

func captureNamedFiles(stepDir string, files []namedFile) (map[string]string, error) {
	captured := map[string]string{}
	if len(files) == 0 {
		return captured, nil
	}

	filesDir := filepath.Join(stepDir, "files")
	if err := os.MkdirAll(filesDir, 0o755); err != nil {
		return nil, err
	}

	for _, file := range files {
		data, err := os.ReadFile(file.source)
		if err != nil {
			return captured, fmt.Errorf("files[%q]: %w", file.name, err)
		}

		destination := filepath.Join(filesDir, executor.SanitizeArtifactName(file.name))
		if err := os.WriteFile(destination, data, 0o644); err != nil {
			return captured, fmt.Errorf("files[%q]: %w", file.name, err)
		}
		captured[file.name] = destination
	}

	return captured, nil
}

func exitCode(state *os.ProcessState) int {
	if state == nil {
		return -1
	}
	return state.ExitCode()
}
