package dataget_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/iw2rmb/amata/internal/executor"
	"github.com/iw2rmb/amata/internal/executor/dataget"
	exprruntime "github.com/iw2rmb/amata/internal/expr"
	"github.com/iw2rmb/amata/internal/spec"
	"github.com/iw2rmb/amata/internal/state"
	"github.com/iw2rmb/amata/internal/workspace"
)

func TestExecutor(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		fileName   string
		fileBody   string
		fields     map[string]any
		wantStatus state.StepStatus
		check      func(t *testing.T, result state.StepResult)
	}{
		{
			name:     "reads YAML with query",
			fileName: "roadmap.yaml",
			fileBody: `
items:
  - label: "1.1"
    done: true
  - label: "1.2"
    done: false
    commit: ""
`,
			fields: map[string]any{
				"file":  "roadmap.yaml",
				"query": `.items | to_entries | map(select(.value.done != true and (.value.commit // "") == "")) | .[0]`,
			},
			wantStatus: state.StepStatusSucceeded,
			check: func(t *testing.T, result state.StepResult) {
				t.Helper()
				value, ok := result.Value.(map[string]any)
				if !ok {
					t.Fatalf("value type = %T, want map[string]any", result.Value)
				}
				if got := value["key"]; got != 1 {
					t.Fatalf("value[key] = %#v, want 1", got)
				}
				item, ok := value["value"].(map[string]any)
				if !ok {
					t.Fatalf("value[value] type = %T, want map[string]any", value["value"])
				}
				if got := item["label"]; got != "1.2" {
					t.Fatalf("label = %#v, want 1.2", got)
				}
			},
		},
		{
			name:     "reads JSON by extension",
			fileName: "input.json",
			fileBody: `{"name":"amata","nested":{"enabled":true}}`,
			fields: map[string]any{
				"file":  "input.json",
				"query": ".nested.enabled",
			},
			wantStatus: state.StepStatusSucceeded,
			check: func(t *testing.T, result state.StepResult) {
				t.Helper()
				if got, ok := result.Value.(bool); !ok || !got {
					t.Fatalf("value = %#v, want true", result.Value)
				}
			},
		},
		{
			name:     "uses default when query returns no results",
			fileName: "doc.yaml",
			fileBody: "items: []\n",
			fields: map[string]any{
				"file":    "doc.yaml",
				"query":   `.items[]`,
				"default": map[string]any{"hasItem": false},
			},
			wantStatus: state.StepStatusSucceeded,
			check: func(t *testing.T, result state.StepResult) {
				t.Helper()
				value, ok := result.Value.(map[string]any)
				if !ok {
					t.Fatalf("value type = %T, want map[string]any", result.Value)
				}
				if got := value["hasItem"]; got != false {
					t.Fatalf("value[hasItem] = %#v, want false", got)
				}
			},
		},
		{
			name:     "fails on multiple results",
			fileName: "doc.yaml",
			fileBody: "items: [1, 2]\n",
			fields: map[string]any{
				"file":  "doc.yaml",
				"query": `.items[]`,
			},
			wantStatus: state.StepStatusFailed,
			check: func(t *testing.T, result state.StepResult) {
				t.Helper()
				if result.Error == nil || result.Error.Code != "query_failed" {
					t.Fatalf("error = %#v, want query_failed", result.Error)
				}
			},
		},
	}

	for i, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, tc.fileName), []byte(tc.fileBody), 0o644); err != nil {
				t.Fatalf("write %s: %v", tc.fileName, err)
			}

			result := dataget.New().Execute(context.Background(), executor.StepContext{
				Workspace: workspace.Config{Root: root},
				StepIndex: i,
				Step: spec.Step{
					Type:   "data.get",
					Fields: tc.fields,
				},
				Runtime: exprruntime.NewRuntime(map[string]any{}),
			})

			if result.Status != tc.wantStatus {
				t.Fatalf("status = %q, want %q: %#v", result.Status, tc.wantStatus, result.Error)
			}
			tc.check(t, result)
		})
	}
}
