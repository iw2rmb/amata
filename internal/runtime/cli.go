package runtime

import (
	"errors"
	"fmt"
	"io"
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
		return fmt.Errorf("resume is not implemented yet")
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

	return nil
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
