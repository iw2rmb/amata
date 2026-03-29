package gitadapter

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type gitCLI struct{}

func (c gitCLI) inspectSnapshot(ctx context.Context, cwd string) (Snapshot, error) {
	repoRoot, isRepo, err := c.resolveRepoRoot(ctx, cwd)
	if err != nil {
		return Snapshot{}, err
	}
	if !isRepo {
		return Snapshot{
			IsRepo:  false,
			HasDiff: false,
			Files:   []string{},
		}, nil
	}

	files, err := c.changedPaths(ctx, repoRoot)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{
		IsRepo:  true,
		Root:    repoRoot,
		HasDiff: len(files) > 0,
		Files:   files,
	}, nil
}

func (gitCLI) resolveRepoRoot(ctx context.Context, cwd string) (string, bool, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
	cmd.Dir = cwd

	output, err := cmd.CombinedOutput()
	if err == nil {
		return filepath.Clean(strings.TrimSpace(string(output))), true, nil
	}

	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		return "", false, formatGitError([]string{"rev-parse", "--show-toplevel"}, err, output)
	}
	if exitErr.ExitCode() == 128 && strings.Contains(strings.ToLower(string(output)), "not a git repository") {
		return "", false, nil
	}
	return "", false, formatGitError([]string{"rev-parse", "--show-toplevel"}, err, output)
}

func (c gitCLI) changedPaths(ctx context.Context, repoRoot string) ([]string, error) {
	outputs := make([][]byte, 0, 3)

	worktreeDiff, err := runGitCommand(ctx, repoRoot, "diff", "--name-only")
	if err != nil {
		return nil, fmt.Errorf("load unstaged paths: %w", err)
	}
	outputs = append(outputs, worktreeDiff)

	cachedDiff, err := runGitCommand(ctx, repoRoot, "diff", "--cached", "--name-only")
	if err != nil {
		return nil, fmt.Errorf("load staged paths: %w", err)
	}
	outputs = append(outputs, cachedDiff)

	untracked, err := runGitCommand(ctx, repoRoot, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, fmt.Errorf("load untracked paths: %w", err)
	}
	outputs = append(outputs, untracked)

	seen := map[string]struct{}{}
	for _, output := range outputs {
		for _, line := range strings.Split(string(output), "\n") {
			rawPath := strings.TrimSpace(line)
			if rawPath == "" {
				continue
			}
			cleaned := path.Clean(filepath.ToSlash(rawPath))
			if cleaned == "." || cleaned == "" {
				continue
			}
			seen[cleaned] = struct{}{}
		}
	}

	files := make([]string, 0, len(seen))
	for file := range seen {
		files = append(files, file)
	}
	sort.Strings(files)
	return files, nil
}

func (c gitCLI) stagePaths(ctx context.Context, repoRoot string, paths []string) ([]string, error) {
	stageablePaths, err := c.resolveStageablePaths(ctx, repoRoot, paths)
	if err != nil {
		return nil, err
	}
	if len(stageablePaths) == 0 {
		return []string{}, nil
	}

	args := append([]string{"add", "-A", "--"}, stageablePaths...)
	if _, err := runGitCommand(ctx, repoRoot, args...); err != nil {
		return nil, fmt.Errorf("stage included paths: %w", err)
	}
	return stageablePaths, nil
}

func (gitCLI) resolveStageablePaths(ctx context.Context, repoRoot string, paths []string) ([]string, error) {
	stageablePaths := make([]string, 0, len(paths))
	for _, path := range paths {
		exists, err := pathExistsInWorktree(repoRoot, path)
		if err != nil {
			return nil, fmt.Errorf("check included path %q in worktree: %w", path, err)
		}
		if exists {
			stageablePaths = append(stageablePaths, path)
			continue
		}

		tracked, err := isPathTracked(ctx, repoRoot, path)
		if err != nil {
			return nil, fmt.Errorf("check included path %q in index: %w", path, err)
		}
		if tracked {
			stageablePaths = append(stageablePaths, path)
		}
	}
	return stageablePaths, nil
}

func pathExistsInWorktree(repoRoot string, path string) (bool, error) {
	worktreePath := filepath.Join(repoRoot, filepath.FromSlash(path))
	_, err := os.Stat(worktreePath)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func isPathTracked(ctx context.Context, repoRoot string, path string) (bool, error) {
	output, err := runGitCommand(ctx, repoRoot, "ls-files", "--", path)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(output)) != "", nil
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
	headBefore, hasHeadBefore, err := resolveCurrentHead(ctx, repoRoot)
	if err != nil {
		return "", fmt.Errorf("resolve pre-commit sha: %w", err)
	}

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
	headAfter := strings.TrimSpace(string(output))
	if !hasHeadBefore {
		return headAfter, nil
	}

	originalCommit, ok, err := resolveFirstCommitAfter(ctx, repoRoot, headBefore, headAfter)
	if err != nil {
		return "", fmt.Errorf("resolve original commit sha: %w", err)
	}
	if ok {
		return originalCommit, nil
	}
	return headAfter, nil
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

func resolveCurrentHead(ctx context.Context, repoRoot string) (string, bool, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--verify", "HEAD")
	cmd.Dir = repoRoot

	output, err := cmd.CombinedOutput()
	if err == nil {
		return strings.TrimSpace(string(output)), true, nil
	}

	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		return "", false, formatGitError([]string{"rev-parse", "--verify", "HEAD"}, err, output)
	}
	if exitErr.ExitCode() == 128 {
		return "", false, nil
	}
	return "", false, formatGitError([]string{"rev-parse", "--verify", "HEAD"}, err, output)
}

func resolveFirstCommitAfter(ctx context.Context, repoRoot string, before string, after string) (string, bool, error) {
	rangeSpec := before + ".." + after
	output, err := runGitCommand(ctx, repoRoot, "rev-list", "--reverse", "--ancestry-path", rangeSpec)
	if err != nil {
		return "", false, err
	}

	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		candidate := strings.TrimSpace(line)
		if candidate != "" {
			return candidate, true, nil
		}
	}
	return "", false, nil
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
