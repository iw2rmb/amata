package schema

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/iw2rmb/amata/internal/jsonutil"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

const responseResourceURL = "workflow:///response"
const schemaResourcePrefix = "workflow:///schemas/"
const providerDefsPrefix = "workflow:"

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

func Normalize(value any) (any, error) {
	return normalizeSchema(value)
}

func ProviderDocument(responseSchema any, workflowSchemas map[string]any) (map[string]any, error) {
	document, err := ExpandedDocument(responseSchema, workflowSchemas)
	if err != nil {
		return nil, err
	}
	return ValidateProviderDocument(document)
}

func ExpandedDocument(responseSchema any, workflowSchemas map[string]any) (map[string]any, error) {
	normalizedResponse, err := normalizeSchema(responseSchema)
	if err != nil {
		return nil, err
	}
	rewrittenResponse := rewriteProviderRefs(normalizedResponse)

	document, ok := rewrittenResponse.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("response schema must resolve to a top-level object schema")
	}

	referencedNames := collectWorkflowSchemaRefs(normalizedResponse)
	normalizedDefs := make(map[string]any, len(referencedNames))
	queue := make([]string, 0, len(referencedNames))
	for name := range referencedNames {
		queue = append(queue, name)
	}
	sort.Strings(queue)

	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if _, ok := normalizedDefs[providerDefinitionName(name)]; ok {
			continue
		}

		rawSchema, ok := workflowSchemas[name]
		if !ok {
			return nil, fmt.Errorf("response schema ref %q is not defined", "#/schemas/"+encodeJSONPointerSegment(name))
		}

		normalized, err := normalizeSchema(rawSchema)
		if err != nil {
			return nil, fmt.Errorf("schemas.%s: %w", name, err)
		}

		for refName := range collectWorkflowSchemaRefs(normalized) {
			if _, ok := normalizedDefs[providerDefinitionName(refName)]; ok {
				continue
			}
			queue = append(queue, refName)
		}
		normalizedDefs[providerDefinitionName(name)] = rewriteProviderRefs(normalized)
	}

	if ref, ok := document["$ref"].(string); ok && len(document) == 1 {
		resolved, found := resolveProviderRef(ref, normalizedDefs)
		if !found {
			return nil, fmt.Errorf("response schema ref %q is not defined", ref)
		}

		resolvedDocument, ok := resolved.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("response schema must resolve to a top-level object schema")
		}
		document = jsonutil.CloneMap(resolvedDocument)
	} else {
		document = jsonutil.CloneMap(document)
	}

	if len(normalizedDefs) > 0 {
		defs := map[string]any{}
		if existingDefs, ok := document["$defs"].(map[string]any); ok {
			defs = jsonutil.CloneMap(existingDefs)
		}
		for name, definition := range normalizedDefs {
			defs[name] = jsonutil.CloneValue(definition)
		}
		document["$defs"] = defs
	}

	if document["type"] != "object" {
		return nil, fmt.Errorf("response schema must resolve to a top-level object schema")
	}

	return document, nil
}

func ValidateProviderDocument(document any) (map[string]any, error) {
	if err := validateProviderSchemaKeywords(document, "#"); err != nil {
		return nil, err
	}

	objectDocument, ok := document.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("response schema must resolve to a top-level object schema")
	}
	if objectDocument["type"] != "object" {
		return nil, fmt.Errorf("response schema must resolve to a top-level object schema")
	}
	return jsonutil.CloneMap(objectDocument), nil
}

func EnsureCodexThinkingField(document map[string]any) map[string]any {
	if document == nil {
		return map[string]any{}
	}

	cloned := jsonutil.CloneMap(document)
	properties, _ := cloned["properties"].(map[string]any)
	if properties == nil {
		properties = map[string]any{}
	} else {
		properties = jsonutil.CloneMap(properties)
	}
	properties["$thinking"] = map[string]any{
		"type":     "string",
		"$comment": "Thinking (reasoning) notes",
	}
	cloned["properties"] = properties
	return cloned
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

func rewriteProviderRefs(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		rewritten := make(map[string]any, len(typed))
		for key, child := range typed {
			if key == "$ref" {
				if ref, ok := child.(string); ok {
					rewritten[key] = rewriteProviderRef(ref)
					continue
				}
			}
			rewritten[key] = rewriteProviderRefs(child)
		}
		return rewritten
	case []any:
		rewritten := make([]any, len(typed))
		for index, child := range typed {
			rewritten[index] = rewriteProviderRefs(child)
		}
		return rewritten
	default:
		return typed
	}
}

func rewriteProviderRef(ref string) string {
	if !strings.HasPrefix(ref, "#/schemas/") {
		return ref
	}

	segments := strings.Split(strings.TrimPrefix(ref, "#/schemas/"), "/")
	if len(segments) == 0 || segments[0] == "" {
		return ref
	}

	name := providerDefinitionName(decodeJSONPointerSegment(segments[0]))
	target := "#/$defs/" + encodeJSONPointerSegment(name)
	if len(segments) == 1 {
		return target
	}

	var fragment strings.Builder
	for _, segment := range segments[1:] {
		fragment.WriteByte('/')
		fragment.WriteString(segment)
	}
	return target + fragment.String()
}

func validateProviderSchemaKeywords(value any, path string) error {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			childPath := jsonPointerPath(path, key)
			switch key {
			case "allOf", "not", "dependentRequired", "dependentSchemas", "if", "then", "else":
				return fmt.Errorf("codex structured output schema does not support %q at %s", key, childPath)
			}
			if err := validateProviderSchemaKeywords(typed[key], childPath); err != nil {
				return err
			}
		}
	case []any:
		for index, child := range typed {
			if err := validateProviderSchemaKeywords(child, fmt.Sprintf("%s/%d", path, index)); err != nil {
				return err
			}
		}
	}
	return nil
}

func collectWorkflowSchemaRefs(value any) map[string]struct{} {
	refs := map[string]struct{}{}
	collectWorkflowSchemaRefsInto(value, refs)
	return refs
}

func collectWorkflowSchemaRefsInto(value any, refs map[string]struct{}) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == "$ref" {
				ref, ok := child.(string)
				if ok {
					if name, ok := workflowSchemaRefName(ref); ok {
						refs[name] = struct{}{}
					}
				}
			}
			collectWorkflowSchemaRefsInto(child, refs)
		}
	case []any:
		for _, child := range typed {
			collectWorkflowSchemaRefsInto(child, refs)
		}
	}
}

func workflowSchemaRefName(ref string) (string, bool) {
	if !strings.HasPrefix(ref, "#/schemas/") {
		return "", false
	}

	segments := strings.Split(strings.TrimPrefix(ref, "#/schemas/"), "/")
	if len(segments) == 0 || segments[0] == "" {
		return "", false
	}
	return decodeJSONPointerSegment(segments[0]), true
}

func providerDefinitionName(name string) string {
	return providerDefsPrefix + name
}

func resolveProviderRef(ref string, defs map[string]any) (any, bool) {
	if !strings.HasPrefix(ref, "#/$defs/") {
		return nil, false
	}

	segments := strings.Split(strings.TrimPrefix(ref, "#/"), "/")
	return resolveJSONPointer(map[string]any{"$defs": defs}, segments)
}

func resolveJSONPointer(value any, segments []string) (any, bool) {
	current := value
	for _, rawSegment := range segments {
		segment := decodeJSONPointerSegment(rawSegment)
		switch typed := current.(type) {
		case map[string]any:
			next, ok := typed[segment]
			if !ok {
				return nil, false
			}
			current = next
		case []any:
			index, err := strconv.Atoi(segment)
			if err != nil || index < 0 || index >= len(typed) {
				return nil, false
			}
			current = typed[index]
		default:
			return nil, false
		}
	}
	return current, true
}

func decodeJSONPointerSegment(value string) string {
	value = strings.ReplaceAll(value, "~1", "/")
	return strings.ReplaceAll(value, "~0", "~")
}

func encodeJSONPointerSegment(value string) string {
	value = strings.ReplaceAll(value, "~", "~0")
	return strings.ReplaceAll(value, "/", "~1")
}

func jsonPointerPath(base string, segment string) string {
	return base + "/" + encodeJSONPointerSegment(segment)
}
