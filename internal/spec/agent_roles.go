package spec

import (
	"fmt"
	"sort"
	"strings"

	"github.com/iw2rmb/amata/internal/jsonutil"
	"gopkg.in/yaml.v3"
)

type AgentRoles map[string][]Step

func ParseAgentRoles(defaults map[string]any) (AgentRoles, error) {
	roles := AgentRoles{}
	raw, ok := defaults["agents"]
	if !ok {
		return roles, nil
	}
	agents, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("defaults.agents must be an object")
	}
	raw, ok = agents["roles"]
	if !ok {
		return roles, nil
	}
	mapping, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("defaults.agents.roles must be an object")
	}
	schemaDocs, err := embeddedSchemaDocuments()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(mapping))
	for name := range mapping {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("agent role name must not be empty")
		}
		for _, builtin := range builtinStepSchemaNames {
			properties, _ := schemaDocs[builtin]["properties"].(map[string]any)
			_, isField := properties[name]
			if name == builtin || isField {
				return nil, fmt.Errorf("agent role %q conflicts with a built-in step or field", name)
			}
		}
		entries, ok := mapping[name].([]any)
		if !ok || len(entries) == 0 {
			return nil, fmt.Errorf("agent role %q must be a non-empty array", name)
		}
		for _, entry := range entries {
			agentName, settings := "", any(nil)
			switch value := entry.(type) {
			case string:
				agentName = value
			case map[string]any:
				if len(value) != 1 {
					return nil, fmt.Errorf("agent role %q entry must name exactly one agent", name)
				}
				for key, value := range value {
					agentName, settings = key, value
				}
			default:
				return nil, fmt.Errorf("agent role %q has an invalid entry", name)
			}
			switch agentName {
			case "codex", "claude", "crush", "nessy":
			default:
				return nil, fmt.Errorf("agent role %q has unknown agent %q", name, agentName)
			}
			variants, ok := settings.([]any)
			if !ok {
				variants = []any{settings}
			}
			if len(variants) == 0 {
				return nil, fmt.Errorf("agent role %q agent %q has no settings", name, agentName)
			}
			for _, variant := range variants {
				fields := map[string]any{}
				switch value := variant.(type) {
				case nil:
				case string:
					fields["model"] = value
				case map[string]any:
					fields = jsonutil.CloneMap(value)
				default:
					return nil, fmt.Errorf("agent role %q agent %q settings must be an object or model string", name, agentName)
				}
				if _, ok := fields["type"]; ok {
					return nil, fmt.Errorf("agent role %q settings cannot override type", name)
				}
				if _, ok := fields["id"]; ok {
					return nil, fmt.Errorf("agent role %q settings cannot override id", name)
				}
				filterRoleFields(fields, schemaDocs[agentName])
				roles[name] = append(roles[name], Step{Type: agentName, Fields: fields})
			}
		}
	}
	return roles, nil
}

func (roles AgentRoles) Candidates(step Step) ([]Step, error) {
	variants := roles[step.ExecutorType()]
	if len(variants) == 0 {
		return nil, nil
	}
	schemaDocs, err := embeddedSchemaDocuments()
	if err != nil {
		return nil, err
	}
	result := make([]Step, 0, len(variants))
	for _, variant := range variants {
		fields := jsonutil.CloneMap(variant.Fields)
		for key, value := range step.Fields {
			if key == "env" {
				base, baseOK := fields[key].(map[string]any)
				override, overrideOK := value.(map[string]any)
				if baseOK && overrideOK {
					for name, entry := range override {
						base[name] = jsonutil.CloneValue(entry)
					}
					continue
				}
			}
			fields[key] = jsonutil.CloneValue(value)
		}
		filterRoleFields(fields, schemaDocs[variant.Type])
		result = append(result, Step{ID: step.ID, Type: variant.Type, Fields: fields})
	}
	return result, nil
}

// Shared role fields must be filtered before validation and dispatch so each
// candidate receives only the options owned by its executor schema.
func filterRoleFields(fields map[string]any, schemaDoc map[string]any) {
	if schemaDoc["additionalProperties"] != false {
		return
	}
	properties, _ := schemaDoc["properties"].(map[string]any)
	for name := range fields {
		if _, allowed := properties[name]; !allowed {
			delete(fields, name)
		}
	}
}

// Role shorthand depends on document defaults, so normalize it before the
// schema-driven Step decoder runs. Persisted steps then carry an explicit type.
func normalizeRoleShorthand(data []byte) ([]byte, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	if len(root.Content) == 0 {
		return data, nil
	}
	document := root.Content[0]
	var defaults map[string]any
	if node := mappingValueNode(document, "defaults"); node != nil {
		if err := node.Decode(&defaults); err != nil {
			return nil, err
		}
	}
	roles, err := ParseAgentRoles(defaults)
	if err != nil {
		return nil, err
	}
	if len(roles) == 0 {
		return data, nil
	}
	visited := map[*yaml.Node]bool{}
	var normalizeSteps func(*yaml.Node) error
	normalizeSteps = func(steps *yaml.Node) error {
		if steps == nil {
			return nil
		}
		if steps.Kind == yaml.AliasNode {
			steps = steps.Alias
		}
		if visited[steps] {
			return nil
		}
		visited[steps] = true
		for _, step := range steps.Content {
			if step.Kind == yaml.AliasNode {
				step = step.Alias
			}
			if step.Kind != yaml.MappingNode {
				continue
			}
			if mappingValueNode(step, "type") == nil {
				selected := ""
				for i := 0; i < len(step.Content); i += 2 {
					key := step.Content[i]
					if _, ok := roles[key.Value]; !ok {
						continue
					}
					if selected != "" || mappingValueNode(step, "prompt") != nil {
						return stepDecodeError(step, "ambiguous agent role shorthand")
					}
					selected = key.Value
					key.Value = "prompt"
				}
				if selected != "" {
					step.Content = append(step.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "type"}, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: selected})
				}
			}
			if err := normalizeSteps(mappingValueNode(step, "steps")); err != nil {
				return err
			}
			cases := mappingValueNode(step, "cases")
			if cases == nil {
				cases = mappingValueNode(step, "switch")
			}
			if cases != nil {
				if cases.Kind == yaml.AliasNode {
					cases = cases.Alias
				}
				for _, branch := range cases.Content {
					if branch.Kind == yaml.AliasNode {
						branch = branch.Alias
					}
					if err := normalizeSteps(mappingValueNode(branch, "steps")); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	if flows := mappingValueNode(document, "flows"); flows != nil {
		for i := 1; i < len(flows.Content); i += 2 {
			flow := flows.Content[i]
			if flow.Kind == yaml.AliasNode {
				flow = flow.Alias
			}
			if err := normalizeSteps(mappingValueNode(flow, "steps")); err != nil {
				return nil, err
			}
		}
	}
	return yaml.Marshal(document)
}
