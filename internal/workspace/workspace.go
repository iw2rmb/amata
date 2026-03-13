package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const DefaultStateDir = ".amata"

type Input struct {
	Root     string
	StateDir string
}

type Config struct {
	Root     string `yaml:"root"`
	StateDir string `yaml:"state_dir"`
}

func Resolve(specPath string, cwd string, input Input, overrideRoot string) (Config, error) {
	if cwd == "" {
		return Config{}, fmt.Errorf("cwd is required")
	}

	specAbs, err := absoluteFrom(cwd, specPath)
	if err != nil {
		return Config{}, fmt.Errorf("resolve spec path: %w", err)
	}

	specDir := filepath.Dir(specAbs)
	rootBase := specDir
	rootValue := input.Root
	if overrideRoot != "" {
		rootBase = cwd
		rootValue = overrideRoot
	}
	if rootValue == "" {
		rootValue = "."
	}

	root, err := absoluteFrom(rootBase, rootValue)
	if err != nil {
		return Config{}, fmt.Errorf("resolve workspace root: %w", err)
	}

	stateDirValue := input.StateDir
	if stateDirValue == "" {
		stateDirValue = DefaultStateDir
	}

	stateDir, err := absoluteFrom(root, stateDirValue)
	if err != nil {
		return Config{}, fmt.Errorf("resolve workspace state_dir: %w", err)
	}

	return Config{
		Root:     root,
		StateDir: stateDir,
	}, nil
}

func absoluteFrom(base string, value string) (string, error) {
	expanded, err := expandHome(value)
	if err != nil {
		return "", err
	}

	if filepath.IsAbs(expanded) {
		return filepath.Clean(expanded), nil
	}

	return filepath.Abs(filepath.Join(base, expanded))
}

func expandHome(value string) (string, error) {
	switch {
	case value == "~":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		return home, nil
	case strings.HasPrefix(value, "~/"):
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		return filepath.Join(home, strings.TrimPrefix(value, "~/")), nil
	default:
		return value, nil
	}
}
