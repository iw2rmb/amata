package expr

import (
	"reflect"
	"testing"
)

func TestRuntimeResolveShorthandAndEscape(t *testing.T) {
	t.Parallel()

	runtime := NewRuntime(map[string]any{
		"ctx": map[string]any{
			"workspace": map[string]any{
				"root": "/tmp/workspace",
			},
			"params": map[string]any{
				"count": int64(3),
			},
			"prev": map[string]any{
				"value": map[string]any{
					"approved": true,
				},
			},
		},
	})

	testCases := []struct {
		name  string
		value any
		want  any
	}{
		{
			name:  "root shorthand uses ctx",
			value: "$.workspace.root",
			want:  "/tmp/workspace",
		},
		{
			name:  "expression object evaluates",
			value: map[string]any{"expr": `ctx.prev.value["approved"]`},
			want:  true,
		},
		{
			name:  "expression object supports root shorthand",
			value: map[string]any{"expr": `$.prev.value["approved"]`},
			want:  true,
		},
		{
			name:  "whole template returns raw value",
			value: "{{ ctx.prev.value }}",
			want: map[string]any{
				"approved": true,
			},
		},
		{
			name:  "template interpolates into strings",
			value: "count={{ ctx.params.count }}",
			want:  "count=3",
		},
		{
			name:  "double dollar escapes",
			value: "$$.workspace.root",
			want:  "$.workspace.root",
		},
		{
			name:  "nested values resolve recursively",
			value: []any{"$.params.count", "$$.params.count"},
			want:  []any{int64(3), "$.params.count"},
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := runtime.Resolve(testCase.value)
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			if !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("Resolve() = %#v, want %#v", got, testCase.want)
			}
		})
	}
}
