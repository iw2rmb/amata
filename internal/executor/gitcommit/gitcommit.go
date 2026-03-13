package gitcommit

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"auto/internal/executor"
	"auto/internal/gitadapter"
	"auto/internal/state"
)

type commitService interface {
	Inspect(context.Context, string) (gitadapter.Snapshot, error)
	Commit(context.Context, gitadapter.Snapshot, gitadapter.CommitOptions) (gitadapter.CommitResult, error)
}

type Executor struct {
	service commitService
}

func New() executor.Executor {
	return NewWithService(gitadapter.New())
}

func NewWithService(service commitService) executor.Executor {
	return &Executor{service: service}
}

func (e *Executor) Execute(ctx context.Context, stepCtx executor.StepContext) state.StepResult {
	if e.service == nil {
		return executor.Failed("invalid_executor", fmt.Sprintf("step %d: git commit service is required", stepCtx.StepIndex))
	}

	message, err := resolveMessage(stepCtx)
	if err != nil {
		return executor.Failed("invalid_message", fmt.Sprintf("step %d: %v", stepCtx.StepIndex, err))
	}

	cwd, err := resolveCWD(stepCtx)
	if err != nil {
		return executor.Failed("invalid_cwd", fmt.Sprintf("step %d: %v", stepCtx.StepIndex, err))
	}

	excludePaths, err := resolveExcludePaths(stepCtx)
	if err != nil {
		return executor.Failed("invalid_exclude_paths", fmt.Sprintf("step %d: %v", stepCtx.StepIndex, err))
	}

	snapshot, err := e.service.Inspect(ctx, cwd)
	if err != nil {
		return executor.Failed("git_commit_failed", fmt.Sprintf("step %d: %v", stepCtx.StepIndex, err))
	}
	if !snapshot.IsRepo {
		return executor.Failed("not_git_repo", fmt.Sprintf("step %d: %q is not inside a git work tree", stepCtx.StepIndex, cwd))
	}

	excludePaths = append(excludePaths, stepCtx.Workspace.StateDir)
	result, err := e.service.Commit(ctx, snapshot, gitadapter.CommitOptions{
		Message:      message,
		ExcludePaths: excludePaths,
	})
	if err != nil {
		return executor.Failed("git_commit_failed", fmt.Sprintf("step %d: %v", stepCtx.StepIndex, err))
	}

	var commit any
	if result.Committed {
		commit = result.Commit
	}

	return executor.Succeeded(map[string]any{
		"committed": result.Committed,
		"commit":    commit,
		"paths":     result.Paths,
	})
}

func resolveMessage(stepCtx executor.StepContext) (string, error) {
	value, ok := stepCtx.Step.Fields["message"]
	if !ok {
		return "", fmt.Errorf("message is required")
	}

	text, err := stepCtx.Runtime.ResolveString(value)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("message must not be empty")
	}
	return text, nil
}

func resolveCWD(stepCtx executor.StepContext) (string, error) {
	value, ok := stepCtx.Step.Fields["cwd"]
	if !ok {
		return stepCtx.Workspace.Root, nil
	}

	text, err := stepCtx.Runtime.ResolveString(value)
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(text) {
		return filepath.Clean(text), nil
	}

	return filepath.Clean(filepath.Join(stepCtx.Workspace.Root, text)), nil
}

func resolveExcludePaths(stepCtx executor.StepContext) ([]string, error) {
	value, ok := stepCtx.Step.Fields["exclude_paths"]
	if !ok {
		return nil, nil
	}

	switch raw := value.(type) {
	case []any:
		paths := make([]string, 0, len(raw))
		for index, item := range raw {
			text, err := stepCtx.Runtime.ResolveString(item)
			if err != nil {
				return nil, fmt.Errorf("exclude_paths[%d]: %w", index, err)
			}
			paths = append(paths, text)
		}
		return paths, nil
	case []string:
		paths := make([]string, 0, len(raw))
		for index, item := range raw {
			text, err := stepCtx.Runtime.ResolveString(item)
			if err != nil {
				return nil, fmt.Errorf("exclude_paths[%d]: %w", index, err)
			}
			paths = append(paths, text)
		}
		return paths, nil
	default:
		return nil, fmt.Errorf("exclude_paths must be an array")
	}
}
