package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"auto/internal/spec"
)

func RunCLI(args []string, stdout io.Writer, stderr io.Writer) error {
	if len(args) == 0 {
		return usageError()
	}

	switch args[0] {
	case "run":
		return runCommand(args[1:], stdout, stderr)
	case "resume":
		return resumeCommand(args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown command %q\n\n%s", args[0], usageText())
	}
}

func runCommand(args []string, stdout io.Writer, stderr io.Writer) error {
	workspaceOverride, runID, specPath, err := parseRunArgs(args)
	if err != nil {
		return err
	}

	loaded, err := spec.Load(specPath)
	if err != nil {
		return err
	}

	config, err := BuildRunConfig(loaded, LaunchOptions{
		WorkspaceOverride: workspaceOverride,
		RunID:             runID,
	})
	if err != nil {
		return err
	}

	if err := PersistRunSpec(config); err != nil {
		return err
	}

	if stdout != nil {
		_, _ = fmt.Fprintln(stdout, config.RunID)
	}

	_, err = NewRunner(nil).Run(context.Background(), config)
	return err
}

func parseRunArgs(args []string) (string, string, string, error) {
	var workspaceOverride string
	var runID string
	var positionals []string

	for index := 0; index < len(args); index++ {
		value := args[index]

		switch {
		case value == "--workspace":
			if index+1 >= len(args) {
				return "", "", "", fmt.Errorf("--workspace requires a value")
			}
			workspaceOverride = args[index+1]
			index++
		case strings.HasPrefix(value, "--workspace="):
			workspaceOverride = strings.TrimPrefix(value, "--workspace=")
		case value == "--run-id":
			if index+1 >= len(args) {
				return "", "", "", fmt.Errorf("--run-id requires a value")
			}
			runID = args[index+1]
			index++
		case strings.HasPrefix(value, "--run-id="):
			runID = strings.TrimPrefix(value, "--run-id=")
		case strings.HasPrefix(value, "-"):
			return "", "", "", fmt.Errorf("unknown flag %q", value)
		default:
			positionals = append(positionals, value)
		}
	}

	if len(positionals) != 1 {
		return "", "", "", fmt.Errorf("run requires exactly one spec path\n\n%s", usageText())
	}

	return workspaceOverride, runID, positionals[0], nil
}

func resumeCommand(args []string, stdout io.Writer, stderr io.Writer) error {
	runID, err := parseResumeArgs(args)
	if err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve cwd: %w", err)
	}

	runDir, err := locateRunDir(cwd, runID)
	if err != nil {
		return err
	}

	config, err := LoadRunConfig(runDir)
	if err != nil {
		return err
	}

	if stdout != nil {
		_, _ = fmt.Fprintln(stdout, config.RunID)
	}

	_, err = NewRunner(nil).Resume(context.Background(), config)
	return err
}

func parseResumeArgs(args []string) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("resume requires exactly one run id\n\n%s", usageText())
	}
	if strings.HasPrefix(args[0], "-") {
		return "", fmt.Errorf("unknown flag %q", args[0])
	}

	return args[0], nil
}

func locateRunDir(cwd string, runID string) (string, error) {
	var matches []string
	walkErr := filepath.WalkDir(cwd, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrPermission) {
				return filepath.SkipDir
			}
			return err
		}
		if entry.IsDir() && entry.Name() == ".git" {
			return filepath.SkipDir
		}
		if !entry.IsDir() {
			return nil
		}
		if entry.Name() != runID {
			return nil
		}
		if filepath.Base(filepath.Dir(path)) != "runs" {
			return nil
		}
		if _, err := os.Stat(filepath.Join(path, "spec.yaml")); err != nil {
			return nil
		}
		matches = append(matches, path)
		if len(matches) > 1 {
			return fmt.Errorf("run %q is ambiguous under %s", runID, cwd)
		}
		return filepath.SkipDir
	})
	if walkErr != nil {
		return "", walkErr
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("run %q was not found under %s", runID, cwd)
	}

	return matches[0], nil
}

func usageError() error {
	return errors.New(usageText())
}

func usageText() string {
	return strings.TrimSpace(`
usage:
  amata run <spec.yaml> [--workspace <dir>] [--run-id <id>]
  amata resume <run-id>
`)
}
