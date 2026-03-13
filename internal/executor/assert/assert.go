package assert

import (
	"context"
	"fmt"

	"auto/internal/executor"
	"auto/internal/state"
)

type Executor struct{}

func New() executor.Executor {
	return &Executor{}
}

func (e *Executor) Execute(_ context.Context, ctx executor.StepContext) state.StepResult {
	value, ok := ctx.Step.Fields["assert"]
	if !ok {
		return executor.Failed("invalid_assert", fmt.Sprintf("step %d is missing assert", ctx.StepIndex))
	}

	passed, ok := value.(bool)
	if !ok {
		return executor.Failed("invalid_assert", fmt.Sprintf("step %d assert must be a boolean", ctx.StepIndex))
	}
	if passed {
		return executor.Succeeded(true)
	}

	message := fmt.Sprintf("step %d assertion failed", ctx.StepIndex)
	if rawMessage, ok := ctx.Step.Fields["message"]; ok {
		text, isString := rawMessage.(string)
		if !isString {
			return executor.Failed("invalid_assert_message", fmt.Sprintf("step %d message must be a string", ctx.StepIndex))
		}
		message = text
	}

	return executor.Failed("assertion_failed", message)
}
