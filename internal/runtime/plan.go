package runtime

import (
	"fmt"

	"auto/internal/spec"
	"gopkg.in/yaml.v3"
)

const switchFlowPrefix = "@switch:"

type flowPlan struct {
	flows       map[string]spec.Flow
	switchFlows map[switchStepKey][]string
}

type switchStepKey struct {
	flowName  string
	stepIndex int
}

type switchCase struct {
	When  any         `yaml:"when,omitempty"`
	Steps []spec.Step `yaml:"steps,omitempty"`
}

func buildFlowPlan(document spec.Document) (*flowPlan, error) {
	plan := &flowPlan{
		flows:       make(map[string]spec.Flow, len(document.Flows)),
		switchFlows: map[switchStepKey][]string{},
	}

	for name, flow := range document.Flows {
		plan.flows[name] = flow
	}
	for name, flow := range document.Flows {
		if err := plan.addSwitchFlows(name, flow); err != nil {
			return nil, err
		}
	}

	return plan, nil
}

func (p *flowPlan) Lookup(name string) (spec.Flow, bool) {
	flow, ok := p.flows[name]
	return flow, ok
}

func (p *flowPlan) SwitchBranchFlow(parentFlow string, stepIndex int, caseIndex int) (string, bool) {
	flows, ok := p.switchFlows[switchStepKey{flowName: parentFlow, stepIndex: stepIndex}]
	if !ok || caseIndex < 0 || caseIndex >= len(flows) {
		return "", false
	}
	return flows[caseIndex], true
}

func (p *flowPlan) addSwitchFlows(flowName string, flow spec.Flow) error {
	for stepIndex, step := range flow.Steps {
		if step.ExecutorType() != "switch" {
			continue
		}

		cases, err := decodeSwitchCases(step)
		if err != nil {
			return fmt.Errorf("flow %q step %d: %w", flowName, stepIndex, err)
		}
		branchFlows := make([]string, len(cases))
		for caseIndex, branch := range cases {
			name := fmt.Sprintf("%s%s:%d:%d", switchFlowPrefix, flowName, stepIndex, caseIndex)
			if _, exists := p.flows[name]; exists {
				return fmt.Errorf("synthetic flow %q already exists", name)
			}

			branchFlow := spec.Flow{Steps: branch.Steps}
			p.flows[name] = branchFlow
			branchFlows[caseIndex] = name
			if err := p.addSwitchFlows(name, branchFlow); err != nil {
				return err
			}
		}
		p.switchFlows[switchStepKey{flowName: flowName, stepIndex: stepIndex}] = branchFlows
	}

	return nil
}

func decodeSwitchCases(step spec.Step) ([]switchCase, error) {
	rawCases, ok := step.Fields["cases"]
	if !ok {
		return nil, fmt.Errorf("switch step is missing cases")
	}

	data, err := yaml.Marshal(rawCases)
	if err != nil {
		return nil, fmt.Errorf("marshal switch cases: %w", err)
	}

	var cases []switchCase
	if err := yaml.Unmarshal(data, &cases); err != nil {
		return nil, fmt.Errorf("decode switch cases: %w", err)
	}
	if len(cases) == 0 {
		return nil, fmt.Errorf("switch step must declare at least one case")
	}

	return cases, nil
}
