[builtins.go](builtins.go) Registers the built-in executor factory set and panics on duplicate or invalid registration.
[cli.go](cli.go) Defines `amata run` and `amata resume` commands, argument parsing, workspace/run resolution, and progress sink wiring.
[cli_internal_test.go](cli_internal_test.go) Ensures invalid CLI invocations fail before any progress controller is initialized.
[cli_test.go](cli_test.go) Validates CLI behavior for usage/errors, workspace normalization, run persistence, resume, and progress output paths.
[config.go](config.go) Builds normalized run configuration, validates parameter overrides, and persists/loads per-run spec metadata.
[context.go](context.go) Constructs expression runtime context with spec/workspace/params plus normalized `ctx.prev` step chaining data.
[control.go](control.go) Prepares control-flow step actions for `call`, `switch`, and `for_each`, including branch selection and loop continuation.
[crush_wiring_test.go](crush_wiring_test.go) Verifies crush executor process wiring, CLI arguments, and prompt delivery over stdin.
[execute.go](execute.go) Executes step attempts, resolves stall policies, handles stall actions, and finalizes executor outcomes.
[execute_test.go](execute_test.go) Tests stall policy parsing, timeout/cancellation handling, and rerun or fallback-call behavior.
[plan.go](plan.go) Builds a flow plan that indexes entry flows and synthesizes nested switch/for_each branch flow names.
[plan_test.go](plan_test.go) Confirms flow-plan expansion for nested switch cases and for-each generated body flows.
[reference_workflow_test.go](reference_workflow_test.go) Runs end-to-end reference workflow smoke and resume tests against a temporary git-backed fixture repo.
[registry.go](registry.go) Provides executor registry creation, validation, lookup, and duplicate-name protection.
[response_resolver.go](response_resolver.go) Resolves step `response` configs, extracts sources, and validates/coerces published values against schemas.
[result.go](result.go) Finalizes step statuses and composes structured return payloads for call/switch/for_each control results.
[runner.go](runner.go) Implements runner lifecycle for run/resume, event-store coordination, and top-level failure shaping.
[runner_builtins_control_test.go](runner_builtins_control_test.go) Exercises built-in control executors for switch/for-each/call semantics and nested context propagation.
[runner_builtins_expr_test.go](runner_builtins_expr_test.go) Verifies expression executor behavior, `when`/`expect` semantics, and `ctx.prev` resolution rules.
[runner_builtins_shell_test.go](runner_builtins_shell_test.go) Tests shell executor artifact capture, templating resolution, and failure handling for invalid file outputs.
[runner_helpers_test.go](runner_helpers_test.go) Supplies shared test executors, fixture builders, and artifact/result normalization helpers.
[runner_progress.go](runner_progress.go) Emits progress events from runner state transitions and reconstructs active-step snapshots for resume.
[runner_progress_test.go](runner_progress_test.go) Validates live progress event ordering, descriptors, and git-commit completion line summaries.
[runner_response_test.go](runner_response_test.go) Covers response publication from values/artifacts, schema validation errors, and context typing behavior.
[runner_resume_test.go](runner_resume_test.go) Tests resume semantics for incomplete runs, frame unwinding, durable failures, and resumed progress events.
[runner_streaming_test.go](runner_streaming_test.go) Verifies artifact streaming visibility during execution and end-to-end agent output capture behavior.
[runner_test.go](runner_test.go) Covers core runner persistence, builtin registration, artifact directory layout, and general run invariants.
[step.go](step.go) Provides step-level helpers for skip/expect evaluation and normalized failure extraction from snapshots or step results.
