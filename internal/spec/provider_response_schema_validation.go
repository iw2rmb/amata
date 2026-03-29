package spec

import (
	"fmt"
	"sort"

	"github.com/iw2rmb/amata/internal/schema"
)

func validateProviderResponseSchemas(document Document, specPath string) error {
	flowNames := make([]string, 0, len(document.Flows))
	for name := range document.Flows {
		flowNames = append(flowNames, name)
	}
	sort.Strings(flowNames)

	for _, flowName := range flowNames {
		if err := validateFlowProviderResponseSchemas(document, specPath, flowName, document.Flows[flowName].Steps); err != nil {
			return err
		}
	}
	return nil
}

func validateFlowProviderResponseSchemas(document Document, specPath string, flowName string, steps []Step) error {
	for stepIndex, step := range steps {
		location := fmt.Sprintf("flow %q step %d", flowName, stepIndex)
		if err := validateStepProviderResponseSchemas(document, specPath, location, step); err != nil {
			return err
		}
	}
	return nil
}

func validateStepProviderResponseSchemas(document Document, specPath string, location string, step Step) error {
	if err := validateStepResponseSchemaForProvider(document, specPath, location, step); err != nil {
		return err
	}

	switch step.ExecutorType() {
	case "switch":
		branches, err := decodeSwitchBranches(step)
		if err != nil {
			return fmt.Errorf("%s: %w", location, err)
		}
		for caseIndex, branch := range branches {
			for nestedStepIndex, nestedStep := range branch {
				nestedLocation := fmt.Sprintf("%s case %d step %d", location, caseIndex, nestedStepIndex)
				if err := validateStepProviderResponseSchemas(document, specPath, nestedLocation, nestedStep); err != nil {
					return err
				}
			}
		}
	case "for_each":
		bodySteps, err := decodeNestedSteps(step.Fields["steps"])
		if err != nil {
			return fmt.Errorf("%s: %w", location, err)
		}
		for nestedStepIndex, nestedStep := range bodySteps {
			nestedLocation := fmt.Sprintf("%s body step %d", location, nestedStepIndex)
			if err := validateStepProviderResponseSchemas(document, specPath, nestedLocation, nestedStep); err != nil {
				return err
			}
		}
	}

	return nil
}

func validateStepResponseSchemaForProvider(document Document, specPath string, location string, step Step) error {
	stepType := step.ExecutorType()
	if stepType != "codex" {
		return nil
	}

	rawSchema, hasSchema, err := resolveResponseSchemaField(step)
	if err != nil {
		return fmt.Errorf("%s (%s): %w", location, stepType, err)
	}
	if !hasSchema {
		return nil
	}

	schemaDocument, err := loadResponseSchemaDocument(rawSchema, document.Schemas, specPath)
	if err != nil {
		return fmt.Errorf("%s (%s): response.schema is invalid: %w", location, stepType, err)
	}

	if _, err := schema.ValidateProviderDocument(schemaDocument); err != nil {
		return fmt.Errorf("%s (%s): response.schema is invalid: %w", location, stepType, err)
	}
	return nil
}

func resolveResponseSchemaField(step Step) (any, bool, error) {
	rawResponse, ok := step.Fields["response"]
	if !ok {
		return nil, false, nil
	}

	responseFields, ok := rawResponse.(map[string]any)
	if !ok {
		return nil, false, fmt.Errorf("response must be a map")
	}

	rawSchema, ok := responseFields["schema"]
	if !ok {
		return nil, false, nil
	}
	return rawSchema, true, nil
}

func loadResponseSchemaDocument(rawSchema any, workflowSchemas map[string]any, specPath string) (any, error) {
	if sourcePath, ok, err := schema.ResolveResponseSchemaPath(rawSchema, specPath); err != nil {
		return nil, err
	} else if ok {
		document, _, err := schema.LoadResponseSchemaFile(sourcePath)
		if err != nil {
			return nil, err
		}
		return document, nil
	}

	document, err := schema.ExpandedDocument(rawSchema, workflowSchemas)
	if err != nil {
		return nil, err
	}
	return document, nil
}
