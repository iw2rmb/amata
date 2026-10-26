package gitadapter

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RepositoryBaseline keeps worktree content separately from the real index so
// fallback preserves changes made before the role step, including staged work.
type RepositoryBaseline struct {
	root        string
	tree        string
	head        string
	indexPath   string
	index       []byte
	indexExists bool
	temporary   string
	excludes    []string
}

func CaptureBaseline(ctx context.Context, cwd string, excludedPaths ...string) (*RepositoryBaseline, error) {
	b := &RepositoryBaseline{root: cwd}
	root, err := b.git(ctx, false, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	b.root = strings.TrimSpace(root)
	dir, err := os.MkdirTemp("", "amata-repo-baseline-")
	if err != nil {
		return nil, err
	}
	b.temporary = dir
	success := false
	defer func() {
		if !success {
			b.Close()
		}
	}()
	for _, excluded := range excludedPaths {
		if resolved, err := filepath.EvalSymlinks(excluded); err == nil {
			excluded = resolved
		}
		relative, err := filepath.Rel(b.root, excluded)
		if err != nil {
			return nil, err
		}
		if relative == "." {
			return nil, fmt.Errorf("cannot exclude the entire repository from fallback reset")
		}
		if relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			b.excludes = append(b.excludes, ":(top,exclude,literal)"+filepath.ToSlash(relative))
		}
	}
	indexPath, err := b.git(ctx, false, "rev-parse", "--git-path", "index")
	if err != nil {
		return nil, err
	}
	b.indexPath = strings.TrimSpace(indexPath)
	if !filepath.IsAbs(b.indexPath) {
		b.indexPath = filepath.Join(b.root, b.indexPath)
	}
	b.index, err = os.ReadFile(b.indexPath)
	b.indexExists = err == nil
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	// An unborn repository has no HEAD, but its files can still be snapshotted.
	head, headErr := b.git(ctx, false, "rev-parse", "--verify", "HEAD")
	if headErr == nil {
		b.head = strings.TrimSpace(head)
	}
	if err := b.stageWorktree(ctx); err != nil {
		return nil, err
	}
	tree, err := b.git(ctx, true, "write-tree")
	if err != nil {
		return nil, err
	}
	b.tree = strings.TrimSpace(tree)
	success = true
	return b, nil
}

func (b *RepositoryBaseline) stageWorktree(ctx context.Context) error {
	// Start with the real index so newly staged ignored files remain tracked.
	index, err := os.ReadFile(b.indexPath)
	if err == nil {
		if err := os.WriteFile(filepath.Join(b.temporary, "index"), index, 0o600); err != nil {
			return err
		}
	} else if os.IsNotExist(err) {
		args := []string{"read-tree", "--empty"}
		if b.head != "" {
			args = []string{"read-tree", b.head}
		}
		if _, err := b.git(ctx, true, args...); err != nil {
			return err
		}
	} else {
		return err
	}
	args := append([]string{"add", "-A", "--", "."}, b.excludes...)
	if _, err := b.git(ctx, true, args...); err != nil {
		return err
	}
	// State may have been staged by an agent. Keep it outside both trees so
	// resetting repository files cannot remove the attempt evidence.
	if len(b.excludes) > 0 {
		args = []string{"rm", "-r", "-f", "--cached", "--ignore-unmatch", "--"}
		for _, excluded := range b.excludes {
			args = append(args, strings.Replace(excluded, "top,exclude,literal", "top,literal", 1))
		}
		if _, err := b.git(ctx, true, args...); err != nil {
			return err
		}
	}
	return nil
}

func (b *RepositoryBaseline) Restore(ctx context.Context) error {
	if err := b.stageWorktree(ctx); err != nil {
		return err
	}
	if _, err := b.git(ctx, true, "read-tree", "--reset", "-u", b.tree); err != nil {
		return err
	}
	if b.head != "" {
		if _, err := b.git(ctx, false, "reset", "--soft", b.head); err != nil {
			return err
		}
	}
	if b.head == "" {
		if _, err := b.git(ctx, false, "update-ref", "-d", "HEAD"); err != nil {
			return err
		}
	}
	if b.indexExists {
		return os.WriteFile(b.indexPath, b.index, 0o644)
	}
	err := os.Remove(b.indexPath)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (b *RepositoryBaseline) Close() { _ = os.RemoveAll(b.temporary) }

func (b *RepositoryBaseline) git(ctx context.Context, temporaryIndex bool, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = b.root
	cmd.Env = os.Environ()
	if temporaryIndex {
		cmd.Env = append(cmd.Env, "GIT_INDEX_FILE="+filepath.Join(b.temporary, "index"))
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}
