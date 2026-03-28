package gitadapter

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"

	gogit "github.com/go-git/go-git/v5"
)

var ErrNotRepository = errors.New("current working directory is not inside a git work tree")

type Service interface {
	Inspect(context.Context, string) (Snapshot, error)
	Commit(context.Context, Snapshot, CommitOptions) (CommitResult, error)
}

type Snapshot struct {
	IsRepo  bool
	Root    string
	HasDiff bool
	Files   []string
}

type CommitOptions struct {
	Message      string
	Body         string
	ExcludePaths []string
}

type CommitFileStat struct {
	Path       string
	Insertions int
	Deletions  int
}

type CommitMetadata struct {
	ShortCommit      string
	ChangedFileCount int
	Insertions       int
	Deletions        int
	FileStats        []CommitFileStat
}

type CommitResult struct {
	Committed bool
	Commit    string
	Paths     []string
	Metadata  *CommitMetadata
}

type Client struct {
	cli gitCLI
}

func New() *Client {
	return &Client{cli: gitCLI{}}
}

func (c *Client) Inspect(_ context.Context, cwd string) (Snapshot, error) {
	repo, repoRoot, err := openRepository(cwd)
	if errors.Is(err, gogit.ErrRepositoryNotExists) {
		return Snapshot{
			IsRepo:  false,
			HasDiff: false,
			Files:   []string{},
		}, nil
	}
	if err != nil {
		return Snapshot{}, err
	}

	worktree, err := repo.Worktree()
	if err != nil {
		return Snapshot{}, fmt.Errorf("open worktree: %w", err)
	}

	status, err := worktree.Status()
	if err != nil {
		return Snapshot{}, fmt.Errorf("load repository status: %w", err)
	}

	files := changedPaths(status)
	return Snapshot{
		IsRepo:  true,
		Root:    repoRoot,
		HasDiff: len(files) > 0,
		Files:   files,
	}, nil
}

func (c *Client) Commit(ctx context.Context, snapshot Snapshot, opts CommitOptions) (CommitResult, error) {
	if !snapshot.IsRepo || snapshot.Root == "" {
		return CommitResult{}, ErrNotRepository
	}

	includedPaths, err := filterPaths(snapshot.Files, snapshot.Root, opts.ExcludePaths)
	if err != nil {
		return CommitResult{}, err
	}

	result := CommitResult{
		Committed: false,
		Paths:     []string{},
	}
	if len(includedPaths) == 0 {
		return result, nil
	}

	stageablePaths, err := c.cli.stagePaths(ctx, snapshot.Root, includedPaths)
	if err != nil {
		return CommitResult{}, err
	}
	result.Paths = stageablePaths
	if len(stageablePaths) == 0 {
		return result, nil
	}

	hasCachedDiff, err := c.cli.hasCachedDiff(ctx, snapshot.Root, stageablePaths)
	if err != nil {
		return CommitResult{}, err
	}
	if !hasCachedDiff {
		return result, nil
	}

	commit, err := c.cli.commitPaths(ctx, snapshot.Root, opts.Message, opts.Body, stageablePaths)
	if err != nil {
		return CommitResult{}, err
	}
	metadata, err := c.cli.commitMetadata(ctx, snapshot.Root, commit)
	if err != nil {
		return CommitResult{}, err
	}

	result.Committed = true
	result.Commit = commit
	result.Metadata = metadata
	return result, nil
}

func openRepository(cwd string) (*gogit.Repository, string, error) {
	repo, err := gogit.PlainOpenWithOptions(cwd, &gogit.PlainOpenOptions{
		DetectDotGit:          true,
		EnableDotGitCommonDir: true,
	})
	if err != nil {
		return nil, "", err
	}

	worktree, err := repo.Worktree()
	if err != nil {
		return nil, "", err
	}

	return repo, filepath.Clean(worktree.Filesystem.Root()), nil
}

func changedPaths(status gogit.Status) []string {
	files := make([]string, 0, len(status))
	for file, fileStatus := range status {
		if fileStatus == nil {
			continue
		}
		if fileStatus.Staging == gogit.Unmodified && fileStatus.Worktree == gogit.Unmodified {
			continue
		}
		files = append(files, path.Clean(filepath.ToSlash(file)))
	}
	sort.Strings(files)
	return files
}

func filterPaths(paths []string, repoRoot string, rawExcludes []string) ([]string, error) {
	excludes, err := normalizeExcludePrefixes(repoRoot, rawExcludes)
	if err != nil {
		return nil, err
	}

	filtered := make([]string, 0, len(paths))
	for _, file := range paths {
		if isExcludedPath(file, excludes) {
			continue
		}
		filtered = append(filtered, file)
	}

	return filtered, nil
}

func normalizeExcludePrefixes(repoRoot string, rawExcludes []string) ([]string, error) {
	seen := make(map[string]struct{}, len(rawExcludes))
	prefixes := make([]string, 0, len(rawExcludes))
	for _, raw := range rawExcludes {
		prefix, ok, err := normalizeExcludePrefix(repoRoot, raw)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if _, exists := seen[prefix]; exists {
			continue
		}
		seen[prefix] = struct{}{}
		prefixes = append(prefixes, prefix)
	}

	sort.Strings(prefixes)
	return prefixes, nil
}

func normalizeExcludePrefix(repoRoot string, raw string) (string, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false, nil
	}

	if filepath.IsAbs(raw) {
		relative, err := filepath.Rel(repoRoot, raw)
		if err != nil {
			return "", false, fmt.Errorf("normalize exclude path %q: %w", raw, err)
		}
		relative = filepath.ToSlash(relative)
		if relative == "." {
			return ".", true, nil
		}
		if relative == ".." || strings.HasPrefix(relative, "../") {
			return "", false, nil
		}
		return normalizeRepoRelativePath(raw, relative)
	}

	return normalizeRepoRelativePath(raw, filepath.ToSlash(raw))
}

func normalizeRepoRelativePath(raw string, value string) (string, bool, error) {
	cleaned := path.Clean(value)
	switch {
	case cleaned == "." || cleaned == "":
		return ".", true, nil
	case cleaned == ".." || strings.HasPrefix(cleaned, "../"):
		return "", false, fmt.Errorf("exclude path %q escapes repository root", raw)
	default:
		return strings.TrimPrefix(cleaned, "./"), true, nil
	}
}

func isExcludedPath(file string, excludes []string) bool {
	for _, exclude := range excludes {
		if exclude == "." || file == exclude || strings.HasPrefix(file, exclude+"/") {
			return true
		}
	}
	return false
}
