package spec

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	embeddedschemas "auto/schemas"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

var (
	stepSchemasOnce sync.Once
	stepSchemas     map[string]*jsonschema.Schema
	stepSchemasErr  error
)

var builtinStepSchemaNames = []string{
	"assert",
	"call",
	"claude",
	"codex",
	"expr",
	"for_each",
	"git.commit",
	"git.inspect",
	"shell",
	"switch",
}

func validateBuiltInSteps(document Document) error {
	schemas, err := compiledStepSchemas()
	if err != nil {
		return err
	}

	flowNames := make([]string, 0, len(document.Flows))
	for name := range document.Flows {
		flowNames = append(flowNames, name)
	}
	sort.Strings(flowNames)

	for _, flowName := range flowNames {
		if err := validateTopLevelFlowSteps(schemas, flowName, document.Flows[flowName].Steps); err != nil {
			return err
		}
	}

	return nil
}

func compiledStepSchemas() (map[string]*jsonschema.Schema, error) {
	stepSchemasOnce.Do(func() {
		documents, err := embeddedSchemaDocuments()
		if err != nil {
			stepSchemasErr = err
			return
		}

		compiler := jsonschema.NewCompiler()
		compiler.DefaultDraft(jsonschema.Draft2020)

		names := make([]string, 0, len(documents))
		for name := range documents {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			resource := "embedded:///" + name + embeddedschemas.Suffix
			if err := compiler.AddResource(resource, documents[name]); err != nil {
				stepSchemasErr = fmt.Errorf("add embedded step schema %q: %w", name, err)
				return
			}
		}

		compiled := make(map[string]*jsonschema.Schema, len(builtinStepSchemaNames))
		for _, name := range builtinStepSchemaNames {
			resource := "embedded:///" + name + embeddedschemas.Suffix
			schema, err := compiler.Compile(resource)
			if err != nil {
				stepSchemasErr = fmt.Errorf("compile embedded step schema %q: %w", name, err)
				return
			}
			compiled[name] = schema
		}
		stepSchemas = compiled
	})

	return stepSchemas, stepSchemasErr
}

func validateTopLevelFlowSteps(schemas map[string]*jsonschema.Schema, flowName string, steps []Step) error {
	for stepIndex, step := range steps {
		location := fmt.Sprintf("flow %q step %d", flowName, stepIndex)
		if err := validateStepAndNested(schemas, location, step); err != nil {
			return err
		}
	}

	return nil
}

func validateNestedSteps(schemas map[string]*jsonschema.Schema, prefix string, steps []Step) error {
	for stepIndex, step := range steps {
		location := fmt.Sprintf("%s step %d", prefix, stepIndex)
		if err := validateStepAndNested(schemas, location, step); err != nil {
			return err
		}
	}

	return nil
}

func validateStepAndNested(schemas map[string]*jsonschema.Schema, location string, step Step) error {
	if err := validateStepSchema(schemas, location, step); err != nil {
		return err
	}

	switch step.ExecutorType() {
	case "switch":
		branches, err := decodeSwitchBranches(step)
		if err != nil {
			return fmt.Errorf("%s: %w", location, err)
		}
		for caseIndex, branch := range branches {
			if err := validateNestedSteps(schemas, fmt.Sprintf("%s case %d", location, caseIndex), branch); err != nil {
				return err
			}
		}
	case "for_each":
		bodySteps, err := decodeNestedSteps(step.Fields["steps"])
		if err != nil {
			return fmt.Errorf("%s: %w", location, err)
		}
		if err := validateNestedSteps(schemas, location+" body", bodySteps); err != nil {
			return err
		}
	}

	return nil
}

func validateStepSchema(schemas map[string]*jsonschema.Schema, location string, step Step) error {
	stepType := step.ExecutorType()
	if stepType == "" {
		return fmt.Errorf("%s: step does not declare an executor", location)
	}

	schema, ok := schemas[stepType]
	if !ok {
		return fmt.Errorf("%s: unknown step type %q", location, stepType)
	}

	document := make(map[string]any, len(step.Fields)+2)
	for key, value := range step.Fields {
		document[key] = value
	}
	if step.ID != "" {
		document["id"] = step.ID
	}
	if step.Type != "" {
		document["type"] = step.Type
	}

	if err := schema.Validate(document); err != nil {
		return fmt.Errorf("%s (%s): %w", location, stepType, trimEmbeddedSchemaPrefix(err))
	}

	return nil
}

func decodeSwitchBranches(step Step) ([][]Step, error) {
	rawCases, ok := step.Fields["cases"]
	if !ok {
		return nil, fmt.Errorf("switch step is missing cases")
	}

	cases, ok := rawCases.([]any)
	if !ok {
		return nil, fmt.Errorf("switch cases must be an array")
	}

	branches := make([][]Step, 0, len(cases))
	for caseIndex, rawCase := range cases {
		fields, ok := rawCase.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("switch case %d must be an object", caseIndex)
		}

		steps, err := decodeNestedSteps(fields["steps"])
		if err != nil {
			return nil, fmt.Errorf("switch case %d: %w", caseIndex, err)
		}
		branches = append(branches, steps)
	}

	return branches, nil
}

func decodeNestedSteps(raw any) ([]Step, error) {
	values, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("steps must be an array")
	}

	data, err := yaml.Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("marshal steps: %w", err)
	}

	var steps []Step
	if err := yaml.Unmarshal(data, &steps); err != nil {
		return nil, fmt.Errorf("decode steps: %w", err)
	}
	return steps, nil
}

func trimEmbeddedSchemaPrefix(err error) error {
	text := err.Error()
	text = strings.TrimPrefix(text, "jsonschema validation failed with 'embedded:///")
	text = strings.Replace(text, embeddedschemas.Suffix+"#'", "#'", 1)
	return fmt.Errorf("%s", text)
}
