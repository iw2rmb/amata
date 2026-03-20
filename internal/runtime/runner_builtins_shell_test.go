package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iw2rmb/amata/internal/spec"
	"github.com/iw2rmb/amata/internal/state"
)

func TestRunnerBuiltinsShellCapturesArtifactsAndNormalizesCWD(t *testing.T) {
	t.Parallel()

	config := testConfig(t, sampleDoc(map[string]spec.Flow{
		"main": {
			Steps: []spec.Step{
				{
					ID: "shell-step",
					Fields: map[string]any{
						"command": "printf 'hello'; printf 'warn' >&2; pwd > report.txt",
						"cwd":     "nested",
						"files": map[string]any{
							"report": "nested/report.txt",
						},
					},
				},
			},
		},
	}))

	if err := os.MkdirAll(filepath.Join(config.Workspace.Root, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir nested cwd: %v", err)
	}
	mustPersist(t, config)

	snapshot, err := NewRunner(nil).Run(context.Background(), config)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	result := snapshot.Steps[0]
	if result.Status != state.StepStatusSucceeded {
		t.Fatalf("step status = %q, want succeeded", result.Status)
	}
	if got := result.Value.(map[string]any)["exitCode"].(float64); got != 0 {
		t.Fatalf("exitCode = %#v, want 0", got)
	}
	if got := strings.TrimSpace(readFile(t, result.Artifacts.Stdout)); got != "hello" {
		t.Fatalf("stdout = %q, want hello", got)
	}
	if got := strings.TrimSpace(readFile(t, result.Artifacts.Stderr)); got != "warn" {
		t.Fatalf("stderr = %q, want warn", got)
	}

	reportPath := result.Artifacts.Files["report"]
	if reportPath == "" {
		t.Fatalf("named report artifact missing")
	}
	if got := strings.TrimSpace(readFile(t, reportPath)); got != filepath.Join(config.Workspace.Root, "nested") {
		t.Fatalf("captured cwd = %q, want %q", got, filepath.Join(config.Workspace.Root, "nested"))
	}
}

func TestRunnerBuiltinsShellResolveTemplatedScalars(t *testing.T) {
	t.Parallel()

	config := testConfig(t, sampleDocWithParams(map[string]any{
		"filename": "report",
		"content":  "templated",
	}, map[string]spec.Flow{
		"main": {
			Steps: []spec.Step{
				{
					ID: "shell-step",
					Fields: map[string]any{
						"command": []any{
							"sh",
							"-lc",
							"printf '{{ ctx.params.content }}' > {{ ctx.params.filename }}.txt",
						},
						"cwd": "{{ ctx.workspace.root }}/nested",
						"files": map[string]any{
							"report": "{{ ctx.workspace.root }}/nested/{{ ctx.params.filename }}.txt",
						},
					},
				},
			},
		},
	}))

	if err := os.MkdirAll(filepath.Join(config.Workspace.Root, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir nested cwd: %v", err)
	}
	mustPersist(t, config)

	snapshot, err := NewRunner(nil).Run(context.Background(), config)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	reportPath := snapshot.Steps[0].Artifacts.Files["report"]
	if reportPath == "" {
		t.Fatalf("named report artifact missing")
	}
	if got := strings.TrimSpace(readFile(t, reportPath)); got != "templated" {
		t.Fatalf("captured report = %q, want templated", got)
	}
}

func TestRunnerBuiltinShellRejectsInvalidFilesBeforeCommandRuns(t *testing.T) {
	t.Parallel()

	config := testConfig(t, sampleDoc(map[string]spec.Flow{
		"main": {
			Steps: []spec.Step{
				{
					ID: "shell-step",
					Fields: map[string]any{
						"command": "touch should-not-exist.txt",
						"files":   []any{"bad"},
					},
				},
			},
		},
	}))

	mustPersist(t, config)

	_, err := NewRunner(nil).Run(context.Background(), config)
	assertRunFailed(t, err, "invalid_files")
	if _, err := os.Stat(filepath.Join(config.Workspace.Root, "should-not-exist.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("command side effect err = %v, want not exists", err)
	}
}

func TestRunnerBuiltinShellKeepsStdIOArtifactsWhenNamedFileCaptureFails(t *testing.T) {
	t.Parallel()

	config := testConfig(t, sampleDoc(map[string]spec.Flow{
		"main": {
			Steps: []spec.Step{
				{
					ID: "shell-step",
					Fields: map[string]any{
						"command": "printf 'hello'; printf 'warn' >&2",
						"files": map[string]any{
							"missing": "missing.txt",
						},
					},
				},
			},
		},
	}))

	mustPersist(t, config)

	_, err := NewRunner(nil).Run(context.Background(), config)
	assertRunFailed(t, err, "artifact_capture_failed")

	snapshot, err := state.NewStore(config.RunDir).LoadSnapshot()
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	result := snapshot.Steps[0]
	if result.Artifacts.Stdout == "" {
		t.Fatalf("stdout artifact path missing")
	}
	if result.Artifacts.Stderr == "" {
		t.Fatalf("stderr artifact path missing")
	}
	if got := strings.TrimSpace(readFile(t, result.Artifacts.Stdout)); got != "hello" {
		t.Fatalf("stdout = %q, want hello", got)
	}
	if got := strings.TrimSpace(readFile(t, result.Artifacts.Stderr)); got != "warn" {
		t.Fatalf("stderr = %q, want warn", got)
	}
}
