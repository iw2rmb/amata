package schema_test

import (
	"testing"

	"auto/internal/schema"
)

func TestRegistryCompileResolvesWorkflowOwnedRefs(t *testing.T) {
	t.Parallel()

	registry := schema.NewRegistry(map[string]any{
		"selected_value": map[string]any{
			"type":                 "object",
			"required":             []any{"selected"},
			"additionalProperties": false,
			"properties": map[string]any{
				"selected": map[string]any{"$ref": "#/schemas/selection_name"},
			},
		},
		"selection_name": "string",
	})

	compiled, err := registry.Compile(map[string]any{
		"$ref": "#/schemas/selected_value",
	})
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}

	if err := compiled.Validate(map[string]any{"selected": "stdout"}); err != nil {
		t.Fatalf("validate value: %v", err)
	}
	if err := compiled.Validate(map[string]any{"selected": true}); err == nil {
		t.Fatalf("expected validation error")
	}
}

func TestRegistryCompileFailsForMissingWorkflowOwnedRef(t *testing.T) {
	t.Parallel()

	registry := schema.NewRegistry(nil)

	_, err := registry.Compile(map[string]any{
		"$ref": "#/schemas/missing",
	})
	if err == nil {
		t.Fatalf("expected compile error")
	}
}

func TestRegistryCompileIgnoresUnusedInvalidWorkflowSchemas(t *testing.T) {
	t.Parallel()

	registry := schema.NewRegistry(map[string]any{
		"used": map[string]any{
			"type": "string",
		},
		"unused": map[string]any{
			"properties": []any{"bad"},
		},
	})

	compiled, err := registry.Compile(map[string]any{
		"$ref": "#/schemas/used",
	})
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}
	if err := compiled.Validate("ok"); err != nil {
		t.Fatalf("validate value: %v", err)
	}
}
