package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ReadFile reads the file at path and returns its contents as a string.
// It fails the test on error.
func ReadFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return string(data)
}

// WriteFile writes contents to path, creating parent directories as needed.
// It fails the test on error.
func WriteFile(t *testing.T, path string, contents string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create parent directory for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// RunGit executes a git command in the given directory and returns its
// combined output. It fails the test on error.
func RunGit(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=core.hooksPath",
		"GIT_CONFIG_VALUE_0=/dev/null",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, string(output))
	}
	return string(output)
}

// InitGitRepo initializes a git repository in dir with a test user config.
func InitGitRepo(t *testing.T, dir string) {
	t.Helper()

	RunGit(t, dir, "init")
	RunGit(t, dir, "config", "user.name", "Test User")
	RunGit(t, dir, "config", "user.email", "test@example.com")
	if err := os.MkdirAll(filepath.Join(dir, ".githooks"), 0o755); err != nil {
		t.Fatalf("create hooks dir: %v", err)
	}
	RunGit(t, dir, "config", "core.hooksPath", ".githooks")
}

// ContainsArgPair returns true if args contains name immediately followed
// by value (e.g. "--flag", "val").
func ContainsArgPair(args []string, name string, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == name && args[i+1] == value {
			return true
		}
	}
	return false
}

// ContainsEnv returns true if any element in values has want as a prefix
// (e.g. "KEY=" matches "KEY=value").
func ContainsEnv(values []string, want string) bool {
	for _, v := range values {
		if strings.HasPrefix(v, want) {
			return true
		}
	}
	return false
}

// AssertFileContents reads the file at path and asserts its contents equal want.
func AssertFileContents(t *testing.T, path string, want string) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(data) != want {
		t.Fatalf("%s = %q, want %q", path, string(data), want)
	}
}

// AssertPathPrefix asserts that path starts with wantPrefix followed by a
// path separator (or equals wantPrefix exactly).
func AssertPathPrefix(t *testing.T, path string, wantPrefix string) {
	t.Helper()

	if !strings.HasPrefix(path, wantPrefix+string(os.PathSeparator)) && path != wantPrefix {
		t.Fatalf("path %q does not start with %q", path, wantPrefix)
	}
}
