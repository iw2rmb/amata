package spec

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	embeddedschemas "github.com/iw2rmb/amata/schemas"
	"gopkg.in/yaml.v3"
)

type stepShorthand struct {
	DefaultField  string
	DefaultSchema any
}

var (
	embeddedSchemaDocsOnce sync.Once
	embeddedSchemaDocs     map[string]map[string]any
	embeddedSchemaDocsErr  error

	stepShorthandOnce   sync.Once
	stepShorthandByType map[string]stepShorthand
	stepShorthandErr    error
)

var responseSchemaKeywords = map[string]struct{}{
	"$defs":                 {},
	"$ref":                  {},
	"additionalItems":       {},
	"additionalProperties":  {},
	"allOf":                 {},
	"anyOf":                 {},
	"const":                 {},
	"contains":              {},
	"definitions":           {},
	"dependentSchemas":      {},
	"else":                  {},
	"enum":                  {},
	"if":                    {},
	"items":                 {},
	"not":                   {},
	"oneOf":                 {},
	"patternProperties":     {},
	"prefixItems":           {},
	"properties":            {},
	"propertyNames":         {},
	"required":              {},
	"then":                  {},
	"type":                  {},
	"unevaluatedItems":      {},
	"unevaluatedProperties": {},
}

func (s *Step) UnmarshalYAML(node *yaml.Node) error {
	step, err := decodeStepNode(node)
	if err != nil {
		return err
	}
	*s = step
	return nil
}

func decodeStepNode(node *yaml.Node) (Step, error) {
	if node.Kind != yaml.MappingNode {
		return Step{}, stepDecodeError(node, "step must be an object")
	}

	shorthandByType, err := shorthandMetadata()
	if err != nil {
		return Step{}, err
	}

	selectedType := ""
	selectedField := ""
	if typeNode := mappingValueNode(node, "type"); typeNode != nil {
		if err := typeNode.Decode(&selectedType); err != nil {
			return Step{}, err
		}
	} else {
		for index := 0; index < len(node.Content); index += 2 {
			keyNode := node.Content[index]
			valueNode := node.Content[index+1]
			if keyNode.Kind != yaml.ScalarNode {
				continue
			}

			meta, ok := shorthandByType[keyNode.Value]
			if !ok || !yamlNodeMatchesSchema(valueNode, meta.DefaultSchema) {
				continue
			}

			selectedType = keyNode.Value
			selectedField = meta.DefaultField
			break
		}
	}

	if selectedType != "" && selectedField != "" {
		if valueNode := mappingValueNode(node, selectedField); valueNode != nil {
			return Step{}, stepDecodeError(node, "step shorthand %q conflicts with %q", selectedType, selectedField)
		}
	}

	step := Step{
		Type:   selectedType,
		Fields: map[string]any{},
	}

	for index := 0; index < len(node.Content); index += 2 {
		keyNode := node.Content[index]
		valueNode := node.Content[index+1]
		if keyNode.Kind != yaml.ScalarNode {
			return Step{}, stepDecodeError(keyNode, "step field names must be strings")
		}

		key := keyNode.Value
		switch key {
		case "id":
			if err := valueNode.Decode(&step.ID); err != nil {
				return Step{}, err
			}
			continue
		case "type":
			continue
		}

		if key == selectedType && selectedField != "" {
			key = selectedField
		}

		value, err := decodeStepFieldValue(key, valueNode)
		if err != nil {
			return Step{}, err
		}
		step.Fields[key] = value
	}

	return step, nil
}

func decodeStepFieldValue(key string, node *yaml.Node) (any, error) {
	switch key {
	case "response":
		return decodeResponseValue(node)
	case "cases":
		return decodeSwitchCasesValue(node)
	case "when":
		return decodeNamedScalarShorthand(node, "when")
	default:
		var value any
		if err := node.Decode(&value); err != nil {
			return nil, err
		}
		return value, nil
	}
}

func decodeResponseValue(node *yaml.Node) (any, error) {
	if responseLooksLikeConfig(node) || !responseLooksLikeSchema(node) {
		var value any
		if err := node.Decode(&value); err != nil {
			return nil, err
		}
		return value, nil
	}

	var value any
	if err := node.Decode(&value); err != nil {
		return nil, err
	}
	meta, ok, err := namedSchemaShorthand("common.response-config")
	if err != nil {
		return nil, err
	}
	if !ok {
		return value, nil
	}
	return map[string]any{meta.DefaultField: value}, nil
}

func decodeSwitchCasesValue(node *yaml.Node) (any, error) {
	if node.Kind != yaml.SequenceNode {
		var value any
		if err := node.Decode(&value); err != nil {
			return nil, err
		}
		return value, nil
	}

	cases := make([]any, 0, len(node.Content))
	for _, itemNode := range node.Content {
		value, err := decodeSwitchCaseValue(itemNode)
		if err != nil {
			return nil, err
		}
		cases = append(cases, value)
	}
	return cases, nil
}

func decodeSwitchCaseValue(node *yaml.Node) (any, error) {
	if node.Kind != yaml.MappingNode {
		var value any
		if err := node.Decode(&value); err != nil {
			return nil, err
		}
		return value, nil
	}

	if mappingValueNode(node, "when") != nil && mappingValueNode(node, "default") != nil {
		return nil, stepDecodeError(node, "switch case cannot declare both %q and %q", "when", "default")
	}

	fields := make(map[string]any, len(node.Content)/2)
	for index := 0; index < len(node.Content); index += 2 {
		keyNode := node.Content[index]
		valueNode := node.Content[index+1]
		if keyNode.Kind != yaml.ScalarNode {
			return nil, stepDecodeError(keyNode, "switch case field names must be strings")
		}

		key := keyNode.Value
		switch key {
		case "when", "default":
			value, err := decodeNamedScalarShorthand(valueNode, "when")
			if err != nil {
				return nil, err
			}
			fields["when"] = value
		default:
			var value any
			if err := valueNode.Decode(&value); err != nil {
				return nil, err
			}
			fields[key] = value
		}
	}
	return fields, nil
}

func decodeNamedScalarShorthand(node *yaml.Node, schemaName string) (any, error) {
	var value any
	if err := node.Decode(&value); err != nil {
		return nil, err
	}

	meta, ok, err := namedSchemaShorthand(schemaName)
	if err != nil {
		return nil, err
	}
	if !ok || node.Kind == yaml.MappingNode || !yamlNodeMatchesSchema(node, meta.DefaultSchema) {
		return value, nil
	}
	return map[string]any{meta.DefaultField: value}, nil
}

func responseLooksLikeConfig(node *yaml.Node) bool {
	return node.Kind == yaml.MappingNode && (mappingValueNode(node, "from") != nil || mappingValueNode(node, "schema") != nil)
}

func responseLooksLikeSchema(node *yaml.Node) bool {
	if node.Kind != yaml.MappingNode {
		return true
	}

	for index := 0; index < len(node.Content); index += 2 {
		keyNode := node.Content[index]
		if keyNode.Kind != yaml.ScalarNode {
			continue
		}
		if _, ok := responseSchemaKeywords[keyNode.Value]; ok {
			return true
		}
	}
	return false
}

func yamlNodeMatchesSchema(node *yaml.Node, schema any) bool {
	switch typed := schema.(type) {
	case bool:
		return typed
	case map[string]any:
		if ref, ok := typed["$ref"].(string); ok {
			resolved, ok := resolveEmbeddedSchemaRef(ref)
			if !ok {
				return false
			}
			return yamlNodeMatchesSchema(node, resolved)
		}
		if anyOf, ok := typed["anyOf"].([]any); ok {
			for _, candidate := range anyOf {
				if yamlNodeMatchesSchema(node, candidate) {
					return true
				}
			}
			return false
		}
		if oneOf, ok := typed["oneOf"].([]any); ok {
			for _, candidate := range oneOf {
				if yamlNodeMatchesSchema(node, candidate) {
					return true
				}
			}
			return false
		}
		if typeValue, ok := typed["type"]; ok {
			return yamlNodeMatchesType(node, typeValue)
		}
		return true
	default:
		return true
	}
}

func yamlNodeMatchesType(node *yaml.Node, rawType any) bool {
	switch typed := rawType.(type) {
	case string:
		return yamlNodeHasType(node, typed)
	case []any:
		for _, candidate := range typed {
			text, ok := candidate.(string)
			if !ok {
				continue
			}
			if yamlNodeHasType(node, text) {
				return true
			}
		}
		return false
	default:
		return true
	}
}

func yamlNodeHasType(node *yaml.Node, schemaType string) bool {
	switch schemaType {
	case "array":
		return node.Kind == yaml.SequenceNode
	case "object":
		return node.Kind == yaml.MappingNode
	case "string":
		return node.Kind == yaml.ScalarNode && node.Tag == "!!str"
	case "boolean":
		return node.Kind == yaml.ScalarNode && node.Tag == "!!bool"
	case "integer":
		return node.Kind == yaml.ScalarNode && node.Tag == "!!int"
	case "number":
		return node.Kind == yaml.ScalarNode && (node.Tag == "!!int" || node.Tag == "!!float")
	case "null":
		return node.Kind == yaml.ScalarNode && node.Tag == "!!null"
	default:
		return true
	}
}

func resolveEmbeddedSchemaRef(ref string) (any, bool) {
	name := strings.TrimSuffix(ref, embeddedschemas.Suffix)
	docs, err := embeddedSchemaDocuments()
	if err != nil {
		return nil, false
	}

	document, ok := docs[name]
	if !ok {
		return nil, false
	}
	return document, true
}

func mappingValueNode(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}

	for index := 0; index < len(node.Content); index += 2 {
		keyNode := node.Content[index]
		if keyNode.Kind == yaml.ScalarNode && keyNode.Value == key {
			return node.Content[index+1]
		}
	}
	return nil
}

func stepDecodeError(node *yaml.Node, format string, args ...any) error {
	message := fmt.Sprintf(format, args...)
	if node != nil && node.Line > 0 {
		return fmt.Errorf("line %d: %s", node.Line, message)
	}
	return fmt.Errorf("%s", message)
}

func embeddedSchemaDocuments() (map[string]map[string]any, error) {
	embeddedSchemaDocsOnce.Do(func() {
		names, err := embeddedschemas.Names()
		if err != nil {
			embeddedSchemaDocsErr = fmt.Errorf("load embedded step schemas: %w", err)
			return
		}
		sort.Strings(names)

		documents := make(map[string]map[string]any, len(names))
		for _, name := range names {
			data, err := embeddedschemas.Read(name)
			if err != nil {
				embeddedSchemaDocsErr = fmt.Errorf("read embedded step schema %q: %w", name, err)
				return
			}

			var document map[string]any
			if err := json.Unmarshal(data, &document); err != nil {
				embeddedSchemaDocsErr = fmt.Errorf("decode embedded step schema %q: %w", name, err)
				return
			}
			documents[name] = document
		}
		embeddedSchemaDocs = documents
	})

	return embeddedSchemaDocs, embeddedSchemaDocsErr
}

func shorthandMetadata() (map[string]stepShorthand, error) {
	stepShorthandOnce.Do(func() {
		metadata := make(map[string]stepShorthand, len(builtinStepSchemaNames))
		for _, name := range builtinStepSchemaNames {
			current, ok, err := namedSchemaShorthand(name)
			if err != nil {
				stepShorthandErr = err
				return
			}
			if !ok {
				continue
			}
			metadata[name] = current
		}
		stepShorthandByType = metadata
	})

	return stepShorthandByType, stepShorthandErr
}

func namedSchemaShorthand(name string) (stepShorthand, bool, error) {
	docs, err := embeddedSchemaDocuments()
	if err != nil {
		return stepShorthand{}, false, err
	}

	document, ok := docs[name]
	if !ok {
		return stepShorthand{}, false, nil
	}

	defaultField, _ := document["$default"].(string)
	if defaultField == "" {
		return stepShorthand{}, false, nil
	}

	properties, ok := document["properties"].(map[string]any)
	if !ok {
		return stepShorthand{}, false, fmt.Errorf("embedded step schema %q is missing properties", name)
	}

	propertySchema, ok := properties[defaultField]
	if !ok {
		return stepShorthand{}, false, fmt.Errorf("embedded step schema %q $default %q is not declared in properties", name, defaultField)
	}

	return stepShorthand{
		DefaultField:  defaultField,
		DefaultSchema: propertySchema,
	}, true, nil
}
