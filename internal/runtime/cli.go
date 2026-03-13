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
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func RunCLI(args []string, stdout io.Writer, stderr io.Writer) error {
	command := newRootCommand(stdout, stderr)
	command.SetArgs(args)

	if err := command.Execute(); err != nil {
		if len(args) > 0 && isUnknownCommandError(err) {
			return fmt.Errorf("unknown command %q\n\n%s", args[0], usageText())
		}
		return err
	}

	return nil
}

func newRootCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	command := &cobra.Command{
		Use:           "amata",
		SilenceErrors: true,
		SilenceUsage:  true,
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return usageError()
			}
			return fmt.Errorf("unknown command %q\n\n%s", args[0], usageText())
		},
	}

	command.SetOut(writerOrDiscard(stdout))
	command.SetErr(writerOrDiscard(stderr))
	command.SetFlagErrorFunc(flagErrorFunc)
	command.AddCommand(newRunCommand(), newResumeCommand())
	return command
}

func newRunCommand() *cobra.Command {
	var workspaceOverride string
	var runID string
	var rawOverrides []string

	command := &cobra.Command{
		Use:                "run <spec.yaml>",
		Short:              "Run a workflow spec",
		SilenceErrors:      true,
		SilenceUsage:       true,
		DisableFlagParsing: false,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("run requires exactly one spec path\n\n%s", usageText())
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			paramOverrides, err := buildParamOverrides(rawOverrides)
			if err != nil {
				return err
			}
			return runCommand(
				cmd.Context(),
				args[0],
				LaunchOptions{
					WorkspaceOverride: workspaceOverride,
					ParamOverrides:    paramOverrides,
					RunID:             runID,
				},
				cmd.OutOrStdout(),
			)
		},
	}

	command.Flags().StringVar(&workspaceOverride, "workspace", "", "Override workspace root")
	command.Flags().StringVar(&runID, "run-id", "", "Explicit run id")
	command.Flags().StringArrayVar(&rawOverrides, "set", nil, "Override declared params with key=value")
	command.SetFlagErrorFunc(flagErrorFunc)
	return command
}

func newResumeCommand() *cobra.Command {
	command := &cobra.Command{
		Use:           "resume <run-id>",
		Short:         "Resume a stored run",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("resume requires exactly one run id\n\n%s", usageText())
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return resumeCommand(cmd.Context(), args[0], cmd.OutOrStdout())
		},
	}

	command.SetFlagErrorFunc(flagErrorFunc)
	return command
}

func writerOrDiscard(writer io.Writer) io.Writer {
	if writer != nil {
		return writer
	}
	return io.Discard
}

func flagErrorFunc(cmd *cobra.Command, err error) error {
	if err == nil {
		return nil
	}

	message := err.Error()
	switch {
	case strings.HasPrefix(message, "unknown flag: "):
		flag := strings.TrimPrefix(message, "unknown flag: ")
		return fmt.Errorf("unknown flag %q", flag)
	case strings.HasPrefix(message, "flag needs an argument: "):
		flag := strings.TrimPrefix(message, "flag needs an argument: ")
		if flag == "--set" {
			return fmt.Errorf("--set requires key=value")
		}
		return fmt.Errorf("%s requires a value", flag)
	default:
		return err
	}
}

func isUnknownCommandError(err error) bool {
	return strings.HasPrefix(err.Error(), "unknown command ")
}

func buildParamOverrides(rawOverrides []string) (map[string]any, error) {
	paramOverrides := map[string]any{}
	for _, rawOverride := range rawOverrides {
		override, err := parseParamOverride(rawOverride)
		if err != nil {
			return nil, err
		}
		paramOverrides[override.key] = override.value
	}
	return paramOverrides, nil
}

func runCommand(ctx context.Context, specPath string, options LaunchOptions, stdout io.Writer) error {
	loaded, err := spec.Load(specPath)
	if err != nil {
		return err
	}

	config, err := BuildRunConfig(loaded, options)
	if err != nil {
		return err
	}

	if err := PersistRunSpec(config); err != nil {
		return err
	}

	if stdout != nil {
		_, _ = fmt.Fprintln(stdout, config.RunID)
	}

	_, err = NewRunner(nil).Run(ctx, config)
	return err
}

type paramOverride struct {
	key   string
	value any
}

func parseParamOverride(value string) (paramOverride, error) {
	key, rawValue, ok := strings.Cut(value, "=")
	if !ok {
		return paramOverride{}, fmt.Errorf("--set requires key=value")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return paramOverride{}, fmt.Errorf("--set requires a non-empty key")
	}
	if rawValue == "" {
		return paramOverride{key: key, value: ""}, nil
	}

	var decoded any
	if err := yaml.Unmarshal([]byte(rawValue), &decoded); err != nil {
		return paramOverride{}, fmt.Errorf("--set %q is invalid: %w", key, err)
	}
	return paramOverride{key: key, value: decoded}, nil
}

func resumeCommand(ctx context.Context, runID string, stdout io.Writer) error {
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

	_, err = NewRunner(nil).Resume(ctx, config)
	return err
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
  amata run <spec.yaml> [--workspace <dir>] [--set key=value ...] [--run-id <id>]
  amata resume <run-id>
`)
}
