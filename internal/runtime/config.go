package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"auto/internal/spec"
	"auto/internal/workspace"
	"gopkg.in/yaml.v3"
)

type LaunchOptions struct {
	WorkspaceOverride string
	RunID             string
}

type Config struct {
	RunID     string
	RunDir    string
	SpecPath  string
	Workspace workspace.Config
	Spec      spec.Document
}

type PersistedRunSpec struct {
	Launch LaunchSettings `yaml:"launch"`
	Spec   spec.Document  `yaml:"spec"`
}

type LaunchSettings struct {
	Command   string           `yaml:"command"`
	RunID     string           `yaml:"run_id"`
	RunDir    string           `yaml:"run_dir"`
	SpecPath  string           `yaml:"spec_path"`
	Workspace workspace.Config `yaml:"workspace"`
}

func BuildRunConfig(loaded spec.Loaded, options LaunchOptions) (Config, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return Config{}, fmt.Errorf("resolve cwd: %w", err)
	}

	resolvedWorkspace, err := workspace.Resolve(
		loaded.Path,
		cwd,
		workspace.Input{
			Root:     loaded.Spec.Workspace.Root,
			StateDir: loaded.Spec.Workspace.StateDir,
		},
		options.WorkspaceOverride,
	)
	if err != nil {
		return Config{}, err
	}

	runID := options.RunID
	if runID == "" {
		runID = time.Now().UTC().Format("20060102T150405.000000000Z0700")
	}

	normalizedSpec := loaded.Spec
	normalizedSpec.Workspace.Root = resolvedWorkspace.Root
	normalizedSpec.Workspace.StateDir = resolvedWorkspace.StateDir

	runDir := filepath.Join(resolvedWorkspace.StateDir, "runs", runID)

	return Config{
		RunID:     runID,
		RunDir:    runDir,
		SpecPath:  loaded.Path,
		Workspace: resolvedWorkspace,
		Spec:      normalizedSpec,
	}, nil
}

func PersistRunSpec(config Config) error {
	if err := os.MkdirAll(config.RunDir, 0o755); err != nil {
		return fmt.Errorf("create run directory: %w", err)
	}

	payload := PersistedRunSpec{
		Launch: LaunchSettings{
			Command:   "run",
			RunID:     config.RunID,
			RunDir:    config.RunDir,
			SpecPath:  config.SpecPath,
			Workspace: config.Workspace,
		},
		Spec: config.Spec,
	}

	data, err := yaml.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal run spec: %w", err)
	}

	path := filepath.Join(config.RunDir, "spec.yaml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write run spec: %w", err)
	}

	return nil
}
