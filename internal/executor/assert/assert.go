package assert

import (
	"context"
	"fmt"

	"github.com/iw2rmb/amata/internal/executor"
	"github.com/iw2rmb/amata/internal/state"
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

	resolved, err := ctx.Runtime.Resolve(value)
	if err != nil {
		return executor.Failed("invalid_assert", fmt.Sprintf("step %d: %v", ctx.StepIndex, err))
	}

	passed, ok := resolved.(bool)
	if !ok {
		return executor.Failed("invalid_assert", fmt.Sprintf("step %d assert must be a boolean", ctx.StepIndex))
	}
	if passed {
		return executor.Succeeded(true)
	}

	message := fmt.Sprintf("step %d assertion failed", ctx.StepIndex)
	if rawMessage, ok := ctx.Step.Fields["message"]; ok {
		text, err := ctx.Runtime.ResolveString(rawMessage)
		if err != nil {
			return executor.Failed("invalid_assert_message", fmt.Sprintf("step %d: %v", ctx.StepIndex, err))
		}
		message = text
	}

	return executor.Failed("assertion_failed", message)
}
