# Crush Executor Support

Scope: Add first-class `crush` workflow-step support alongside `codex` and `claude` for non-interactive agent execution, prompt rendering, schema-backed response parsing, and docs/test coverage.

Documentation: docs/engine/index.md

- [x] 1.1 Lock `crush` execution contract and edge-case behavior
  - Repository: amata
    1. Define the `crush run` invocation contract in `internal/executor/crush/provider.Execute` using `--yolo`, `--quiet`, `--model`, and stdin prompt input.
    2. Define `reasoning` handling for `crush` in `internal/executor/crush/provider.Execute` so unsupported values do not fail silently.
    3. Define structured-output behavior for `crush` in `internal/executor/crush/provider.Execute` using `agent.StructuredPrompt` and `agent.ParseStructuredOutput`.
  - Verification:
    1. `go test ./internal/executor/crush -run TestProvider` covers model, prompt, structured-output, and unsupported-input behavior.
    2. `go test ./internal/executor/agent -run TestExecutor` confirms `agent.Executor` error normalization still applies to `crush`.
  - Reasoning: high

- [x] 1.2 Implement `crush` as a built-in executor and spec step type
  - Repository: amata
    1. Add `internal/executor/crush/crush.go` with `provider.Name` and `provider.Execute` wired through `agent.New`.
    2. Register `crush` in `internal/runtime/builtins.go` so runtime dispatch can instantiate the new executor.
    3. Add `schemas/crush.amata.schema.json` and include `crush` in `internal/spec/step_schemas.go` builtin schema registration.
    4. Keep shorthand parsing aligned via `internal/spec/step_unmarshal.go` schema-driven metadata so `crush: <prompt>` resolves to `type: crush` plus `prompt`.
  - Verification:
    1. `go test ./internal/spec -run TestLoadAcceptsStepAndResponseShorthand` covers `crush` shorthand parsing.
    2. `go test ./internal/runtime -run TestBuiltin` confirms `crush` registration does not regress executor wiring.
  - Reasoning: high

- [x] 1.3 Extend progress rendering to treat `crush` as an agent step
  - Repository: amata
    1. Update `internal/progress/descriptor.go` agent-step branching so `crush` resolves model and prompt through `agent.ResolveStep`.
    2. Update `internal/progress/stream.go` detail rendering so `crush` uses agent prompt markdown rendering.
    3. Consolidate repeated provider checks in progress rendering into one shared helper to keep `codex`/`claude`/`crush` behavior deterministic.
  - Verification:
    1. `go test ./internal/progress -run TestStepDescriptorShapes` covers `crush` primary/detail text rendering.
    2. `go test ./internal/progress -run TestBlockForEventFormatsPlainText` confirms `crush` step output formatting.
  - Reasoning: medium

- [x] 1.4 Add focused tests for `crush` provider and integration surfaces
  - Repository: amata
    1. Add table-driven tests in `internal/executor/crush/crush_test.go` for args, env, cwd, streaming capture, structured parse, and failure paths.
    2. Extend `internal/executor/agent/agent_test.go` with a `crush`-named fake provider case for defaults resolution and metadata artifacts.
    3. Extend `internal/spec/spec_test.go` and `internal/progress/*_test.go` with one canonical `crush` case per behavior path.
    4. Add a runtime-level fake binary path in `internal/runtime/reference_workflow_test.go` (or a new targeted runtime test) to prove `crush` process invocation wiring.
  - Verification:
    1. `go test ./internal/executor/crush ./internal/executor/agent ./internal/spec ./internal/progress` passes.
    2. `go test ./internal/runtime -run TestReferenceWorkflow` (or the new targeted runtime test) passes with fake agent binaries.
  - Reasoning: high

- [x] 1.5 Update durable docs for shipped `crush` behavior
  - Repository: amata
    1. Update `docs/engine/index.md` summary and built-in executor list to include `crush`.
    2. Update `docs/engine/index.md` defaults and shorthand sections to include `defaults.executors.crush` and `crush: <prompt>`.
    3. Add a dedicated `crush` executor behavior section in `docs/engine/index.md` that documents model requirements, structured-output mode, and artifacts.
  - Verification:
    1. `rg -n "codex\|claude" docs/engine/index.md` no longer reports outdated two-provider-only statements.
    2. `~/@iw2rmb/amata/scripts/check_docs_links.sh` passes from repo root.
  - Reasoning: medium
