package schema_test

import (
	"testing"

	"github.com/iw2rmb/amata/internal/schema"
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

func TestProviderDocumentExpandsTopLevelWorkflowRef(t *testing.T) {
	t.Parallel()

	document, err := schema.ProviderDocument(map[string]any{
		"$ref": "#/schemas/selected_value",
	}, map[string]any{
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
	if err != nil {
		t.Fatalf("provider document: %v", err)
	}

	if document["type"] != "object" {
		t.Fatalf("document type = %#v, want object", document["type"])
	}
	if _, ok := document["$ref"]; ok {
		t.Fatalf("document unexpectedly kept top-level $ref: %#v", document["$ref"])
	}

	properties, ok := document["properties"].(map[string]any)
	if !ok {
		t.Fatalf("document properties missing: %#v", document["properties"])
	}
	selected, ok := properties["selected"].(map[string]any)
	if !ok {
		t.Fatalf("selected property missing: %#v", properties["selected"])
	}
	if selected["$ref"] != "#/$defs/workflow:selection_name" {
		t.Fatalf("selected $ref = %#v, want #/$defs/workflow:selection_name", selected["$ref"])
	}

	defs, ok := document["$defs"].(map[string]any)
	if !ok {
		t.Fatalf("document $defs missing: %#v", document["$defs"])
	}
	if _, ok := defs["workflow:selected_value"]; !ok {
		t.Fatalf("document $defs missing workflow:selected_value: %#v", defs)
	}
	if _, ok := defs["workflow:selection_name"]; !ok {
		t.Fatalf("document $defs missing workflow:selection_name: %#v", defs)
	}
}

func TestProviderDocumentRejectsUnsupportedProviderKeywords(t *testing.T) {
	t.Parallel()

	_, err := schema.ProviderDocument(map[string]any{
		"$ref": "#/schemas/selected_value",
	}, map[string]any{
		"selected_value": map[string]any{
			"type":                 "object",
			"required":             []any{"selected"},
			"additionalProperties": false,
			"allOf": []any{
				map[string]any{
					"if": map[string]any{
						"properties": map[string]any{
							"selected": map[string]any{"const": "x"},
						},
					},
					"then": map[string]any{
						"required": []any{"selected"},
					},
				},
			},
			"properties": map[string]any{
				"selected": map[string]any{"$ref": "#/schemas/selection_name"},
			},
		},
		"selection_name": "string",
	})
	if err == nil {
		t.Fatalf("expected provider document error")
	}
	if got := err.Error(); got != `codex structured output schema does not support "allOf" at #/$defs/workflow:selected_value/allOf` {
		t.Fatalf("provider document error = %q", got)
	}
}

func TestProviderDocumentRejectsNonObjectTopLevelSchema(t *testing.T) {
	t.Parallel()

	_, err := schema.ProviderDocument("string", nil)
	if err == nil {
		t.Fatalf("expected provider document error")
	}
}

func TestProviderDocumentIgnoresUnusedUnsupportedWorkflowSchemas(t *testing.T) {
	t.Parallel()

	document, err := schema.ProviderDocument(map[string]any{
		"$ref": "#/schemas/used",
	}, map[string]any{
		"used": map[string]any{
			"type":                 "object",
			"required":             []any{"approved"},
			"additionalProperties": false,
			"properties": map[string]any{
				"approved": "boolean",
			},
		},
		"unused": map[string]any{
			"type": "object",
			"allOf": []any{
				map[string]any{
					"required": []any{"bad"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("provider document: %v", err)
	}

	defs, ok := document["$defs"].(map[string]any)
	if !ok {
		t.Fatalf("document $defs missing: %#v", document["$defs"])
	}
	if _, ok := defs["workflow:unused"]; ok {
		t.Fatalf("document unexpectedly included unused schema: %#v", defs)
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
