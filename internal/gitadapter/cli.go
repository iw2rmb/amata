package gitadapter

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strconv"
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

func (gitCLI) commitPaths(ctx context.Context, repoRoot string, message string, body string, paths []string) (string, error) {
	args := []string{"commit", "--quiet", "-m", message}
	if strings.TrimSpace(body) != "" {
		args = append(args, "-m", body)
	}
	args = append(args, "--")
	args = append(args, paths...)
	if _, err := runGitCommand(ctx, repoRoot, args...); err != nil {
		return "", fmt.Errorf("commit included paths: %w", err)
	}

	output, err := runGitCommand(ctx, repoRoot, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("resolve commit sha: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

func (gitCLI) commitMetadata(ctx context.Context, repoRoot string, commit string) (*CommitMetadata, error) {
	shortOutput, err := runGitCommand(ctx, repoRoot, "rev-parse", "--short", commit)
	if err != nil {
		return nil, fmt.Errorf("resolve short commit sha: %w", err)
	}

	statsOutput, err := runGitCommand(ctx, repoRoot, "show", "--numstat", "--format=", "--no-renames", commit)
	if err != nil {
		return nil, fmt.Errorf("load commit stats: %w", err)
	}

	fileStats, err := parseCommitFileStats(statsOutput)
	if err != nil {
		return nil, fmt.Errorf("parse commit stats: %w", err)
	}

	metadata := &CommitMetadata{
		ShortCommit: strings.TrimSpace(string(shortOutput)),
		FileStats:   fileStats,
	}
	for _, stat := range fileStats {
		metadata.ChangedFileCount++
		metadata.Insertions += stat.Insertions
		metadata.Deletions += stat.Deletions
	}

	return metadata, nil
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

func parseCommitFileStats(output []byte) ([]CommitFileStat, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	stats := []CommitFileStat{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		fields := strings.SplitN(line, "\t", 3)
		if len(fields) != 3 {
			return nil, fmt.Errorf("unexpected numstat line %q", line)
		}

		insertions, err := parseNumstatCount(fields[0])
		if err != nil {
			return nil, fmt.Errorf("parse insertions for %q: %w", fields[2], err)
		}
		deletions, err := parseNumstatCount(fields[1])
		if err != nil {
			return nil, fmt.Errorf("parse deletions for %q: %w", fields[2], err)
		}

		stats = append(stats, CommitFileStat{
			Path:       fields[2],
			Insertions: insertions,
			Deletions:  deletions,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return stats, nil
}

func parseNumstatCount(value string) (int, error) {
	if value == "-" {
		return 0, nil
	}
	count, err := strconv.Atoi(value)
	if err != nil {
		return 0, err
	}
	return count, nil
}
