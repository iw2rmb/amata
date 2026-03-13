package schema

import (
	"fmt"
	"net/url"
	"strings"

	"auto/internal/jsonutil"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

const responseResourceURL = "workflow:///response"
const schemaResourcePrefix = "workflow:///schemas/"

type Registry struct {
	schemas map[string]any
}

type Compiled struct {
	schema *jsonschema.Schema
}

func NewRegistry(workflowSchemas map[string]any) *Registry {
	cloned := make(map[string]any, len(workflowSchemas))
	for name, schema := range workflowSchemas {
		cloned[name] = jsonutil.CloneValue(schema)
	}
	return &Registry{schemas: cloned}
}

func (r *Registry) Compile(responseSchema any) (*Compiled, error) {
	normalizedResponse, err := normalizeSchema(responseSchema)
	if err != nil {
		return nil, err
	}

	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.UseLoader(jsonschema.SchemeURLLoader{
		"workflow": workflowLoader{schemas: r.schemas},
	})

	if err := compiler.AddResource(responseResourceURL, rewriteLocalRefs(normalizedResponse)); err != nil {
		return nil, fmt.Errorf("add response schema: %w", err)
	}

	compiled, err := compiler.Compile(responseResourceURL)
	if err != nil {
		return nil, err
	}
	return &Compiled{schema: compiled}, nil
}

func (c *Compiled) Validate(value any) error {
	if c == nil || c.schema == nil {
		return fmt.Errorf("compiled schema is required")
	}
	return c.schema.Validate(jsonutil.CloneValue(value))
}

func schemaResourceURL(name string) string {
	return schemaResourcePrefix + url.PathEscape(name)
}

type workflowLoader struct {
	schemas map[string]any
}

func (l workflowLoader) Load(rawURL string) (any, error) {
	if !strings.HasPrefix(rawURL, schemaResourcePrefix) {
		return nil, fmt.Errorf("unsupported workflow schema url %q", rawURL)
	}

	name, err := url.PathUnescape(strings.TrimPrefix(rawURL, schemaResourcePrefix))
	if err != nil {
		return nil, fmt.Errorf("decode workflow schema url %q: %w", rawURL, err)
	}
	if name == "" {
		return nil, fmt.Errorf("workflow schema name is required")
	}

	schema, ok := l.schemas[name]
	if !ok {
		return nil, fmt.Errorf("workflow schema %q is not defined", name)
	}

	normalized, err := normalizeSchema(schema)
	if err != nil {
		return nil, fmt.Errorf("normalize #/schemas/%s: %w", name, err)
	}
	return rewriteLocalRefs(normalized), nil
}

func normalizeSchema(value any) (any, error) {
	switch typed := value.(type) {
	case nil:
		return nil, fmt.Errorf("schema is required")
	case bool:
		return typed, nil
	case string:
		switch typed {
		case "string", "number", "boolean", "object":
			return map[string]any{"type": typed}, nil
		default:
			return map[string]any{"$ref": typed}, nil
		}
	case map[string]any:
		normalized := make(map[string]any, len(typed))
		for key, child := range typed {
			switch key {
			case "$defs", "definitions", "dependentSchemas", "patternProperties", "properties":
				items, err := normalizeSchemaMap(child)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", key, err)
				}
				normalized[key] = items
			case "additionalItems", "additionalProperties", "contains", "contentSchema", "else", "if", "items", "not", "propertyNames", "then", "unevaluatedItems", "unevaluatedProperties":
				item, err := normalizeSchema(child)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", key, err)
				}
				normalized[key] = item
			case "allOf", "anyOf", "oneOf", "prefixItems":
				items, err := normalizeSchemaSlice(child)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", key, err)
				}
				normalized[key] = items
			default:
				normalized[key] = jsonutil.CloneValue(child)
			}
		}
		return normalized, nil
	case []any:
		return normalizeSchemaSlice(typed)
	default:
		return jsonutil.CloneValue(value), nil
	}
}

func normalizeSchemaMap(value any) (map[string]any, error) {
	raw, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("must be a map")
	}

	normalized := make(map[string]any, len(raw))
	for key, child := range raw {
		item, err := normalizeSchema(child)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		normalized[key] = item
	}
	return normalized, nil
}

func normalizeSchemaSlice(value any) ([]any, error) {
	raw, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("must be a list")
	}

	normalized := make([]any, len(raw))
	for index, child := range raw {
		item, err := normalizeSchema(child)
		if err != nil {
			return nil, fmt.Errorf("%d: %w", index, err)
		}
		normalized[index] = item
	}
	return normalized, nil
}

func rewriteLocalRefs(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		rewritten := make(map[string]any, len(typed))
		for key, child := range typed {
			if key == "$ref" {
				if ref, ok := child.(string); ok {
					rewritten[key] = rewriteLocalRef(ref)
					continue
				}
			}
			rewritten[key] = rewriteLocalRefs(child)
		}
		return rewritten
	case []any:
		rewritten := make([]any, len(typed))
		for index, child := range typed {
			rewritten[index] = rewriteLocalRefs(child)
		}
		return rewritten
	default:
		return typed
	}
}

func rewriteLocalRef(ref string) string {
	if !strings.HasPrefix(ref, "#/schemas/") {
		return ref
	}

	segments := strings.Split(strings.TrimPrefix(ref, "#/schemas/"), "/")
	if len(segments) == 0 || segments[0] == "" {
		return ref
	}

	name := decodeJSONPointerSegment(segments[0])
	target := schemaResourceURL(name)
	if len(segments) == 1 {
		return target
	}

	var fragment strings.Builder
	for _, segment := range segments[1:] {
		fragment.WriteByte('/')
		fragment.WriteString(segment)
	}
	return target + "#" + fragment.String()
}

func decodeJSONPointerSegment(value string) string {
	value = strings.ReplaceAll(value, "~1", "/")
	return strings.ReplaceAll(value, "~0", "~")
}
