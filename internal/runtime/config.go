package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/iw2rmb/amata/internal/jsonutil"
	"github.com/iw2rmb/amata/internal/spec"
	"github.com/iw2rmb/amata/internal/workspace"
	"gopkg.in/yaml.v3"
)

type LaunchOptions struct {
	WorkspaceOverride string
	ParamOverrides    map[string]any
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
	if err := validateParamOverrides(loaded.Spec.Params, options.ParamOverrides); err != nil {
		return Config{}, err
	}

	runID := options.RunID
	if runID == "" {
		runID = time.Now().UTC().Format("20060102T150405.000000000Z0700")
	}

	normalizedSpec := loaded.Spec
	normalizedSpec.Params = jsonutil.CloneMap(loaded.Spec.Params)
	if len(options.ParamOverrides) > 0 {
		if normalizedSpec.Params == nil {
			normalizedSpec.Params = map[string]any{}
		}
		for key, value := range options.ParamOverrides {
			normalizedSpec.Params[key] = jsonutil.CloneValue(value)
		}
	}
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

func validateParamOverrides(declared map[string]any, overrides map[string]any) error {
	if len(overrides) == 0 {
		return nil
	}

	for key := range overrides {
		if _, ok := declared[key]; ok {
			continue
		}
		return fmt.Errorf("param %q is not declared in spec.params", key)
	}
	return nil
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

func LoadRunConfig(runDir string) (Config, error) {
	absRunDir, err := filepath.Abs(runDir)
	if err != nil {
		return Config{}, fmt.Errorf("resolve run directory: %w", err)
	}

	data, err := os.ReadFile(filepath.Join(absRunDir, "spec.yaml"))
	if err != nil {
		return Config{}, fmt.Errorf("read persisted run spec: %w", err)
	}

	var persisted PersistedRunSpec
	if err := yaml.Unmarshal(data, &persisted); err != nil {
		return Config{}, fmt.Errorf("decode persisted run spec: %w", err)
	}

	return Config{
		RunID:     persisted.Launch.RunID,
		RunDir:    persisted.Launch.RunDir,
		SpecPath:  persisted.Launch.SpecPath,
		Workspace: persisted.Launch.Workspace,
		Spec:      persisted.Spec,
	}, nil
}
