package runtime

import (
	"fmt"

	"auto/internal/spec"
	"gopkg.in/yaml.v3"
)

const switchFlowPrefix = "@switch:"
const forEachFlowPrefix = "@for_each:"

type flowPlan struct {
	flows        map[string]spec.Flow
	switchFlows  map[switchStepKey][]string
	forEachFlows map[switchStepKey]string
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
		flows:        make(map[string]spec.Flow, len(document.Flows)),
		switchFlows:  map[switchStepKey][]string{},
		forEachFlows: map[switchStepKey]string{},
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

func (p *flowPlan) ForEachBodyFlow(parentFlow string, stepIndex int) (string, bool) {
	flow, ok := p.forEachFlows[switchStepKey{flowName: parentFlow, stepIndex: stepIndex}]
	return flow, ok
}

func (p *flowPlan) addSwitchFlows(flowName string, flow spec.Flow) error {
	for stepIndex, step := range flow.Steps {
		if step.ExecutorType() != "switch" {
			if step.ExecutorType() == "for_each" {
				body, err := decodeForEach(step)
				if err != nil {
					return fmt.Errorf("flow %q step %d: %w", flowName, stepIndex, err)
				}
				name := fmt.Sprintf("%s%s:%d", forEachFlowPrefix, flowName, stepIndex)
				if _, exists := p.flows[name]; exists {
					return fmt.Errorf("synthetic flow %q already exists", name)
				}

				bodyFlow := spec.Flow{Steps: body.Steps}
				p.flows[name] = bodyFlow
				p.forEachFlows[switchStepKey{flowName: flowName, stepIndex: stepIndex}] = name
				if err := p.addSwitchFlows(name, bodyFlow); err != nil {
					return err
				}
			}
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

type forEachSpec struct {
	Items any         `yaml:"items"`
	As    string      `yaml:"as,omitempty"`
	Steps []spec.Step `yaml:"steps,omitempty"`
}

func decodeForEach(step spec.Step) (forEachSpec, error) {
	rawItems, ok := step.Fields["items"]
	if !ok {
		return forEachSpec{}, fmt.Errorf("for_each step is missing items")
	}

	data, err := yaml.Marshal(step.Fields)
	if err != nil {
		return forEachSpec{}, fmt.Errorf("marshal for_each: %w", err)
	}

	var resolved forEachSpec
	if err := yaml.Unmarshal(data, &resolved); err != nil {
		return forEachSpec{}, fmt.Errorf("decode for_each: %w", err)
	}
	if rawItems == nil {
		return forEachSpec{}, fmt.Errorf("for_each step is missing items")
	}
	if len(resolved.Steps) == 0 {
		return forEachSpec{}, fmt.Errorf("for_each step must declare at least one step")
	}
	return resolved, nil
}
