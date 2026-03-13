package gitadapter

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type gitCLI struct{}

func (gitCLI) stagePaths(ctx context.Context, repoRoot string, paths []string) error {
	args := append([]string{"add", "-A", "--"}, paths...)
	if _, err := runGitCommand(ctx, repoRoot, args...); err != nil {
		return fmt.Errorf("stage included paths: %w", err)
	}
	return nil
}

func (gitCLI) hasCachedDiff(ctx context.Context, repoRoot string, paths []string) (bool, error) {
	args := append([]string{"diff", "--cached", "--quiet", "--exit-code", "--"}, paths...)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoRoot

	output, err := cmd.CombinedOutput()
	if err == nil {
		return false, nil
	}

	exitErr, ok := err.(*exec.ExitError)
	if ok && exitErr.ExitCode() == 1 {
		return true, nil
	}

	return false, formatGitError(args, err, output)
}

func (gitCLI) commitPaths(ctx context.Context, repoRoot string, message string, paths []string) (string, error) {
	args := append([]string{"commit", "--quiet", "-m", message, "--"}, paths...)
	if _, err := runGitCommand(ctx, repoRoot, args...); err != nil {
		return "", fmt.Errorf("commit included paths: %w", err)
	}

	output, err := runGitCommand(ctx, repoRoot, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("resolve commit sha: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

func runGitCommand(ctx context.Context, repoRoot string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoRoot

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, formatGitError(args, err, output)
	}
	return output, nil
}

func formatGitError(args []string, err error, output []byte) error {
	if message := strings.TrimSpace(string(output)); message != "" {
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, message)
	}
	return fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
}
