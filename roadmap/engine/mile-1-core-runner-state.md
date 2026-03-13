# Engine Milestone 1 Core Runner, Workspace, and Durable State

Scope: Build the Go engine skeleton that loads workflow specs, normalizes workspace paths, executes minimal sequential flows, and persists completed-step state for `resume`.

Documentation: [Engine design](../../design/engine/engine.md)

Legend: [ ] todo, [x] done.

- [ ] 1.1 Bootstrap CLI, spec model, and normalized runtime config
  - Repository: auto
  - Component: `cmd/amata`, `internal/spec`, `internal/workspace`, `internal/runtime`
  - Verification: valid specs load, invalid workspace shapes fail fast, `--workspace` and `--run-id` override normalized runtime settings
  - Reasoning: high
  1. Create the Go module layout for CLI entrypoints, spec decoding, workspace resolution, runtime config, and executor registration.
  2. Implement spec decoding for `version`, `name`, `entry`, `workspace`, `params`, `defaults`, `schemas`, and `flows`.
  3. Normalize `workspace.root` from the spec file location or `--workspace`, then normalize `workspace.state_dir` from the resolved root.
  4. Persist the normalized spec and launch settings under `<workspace.state_dir>/runs/<run-id>/spec.yaml`.

- [ ] 1.2 Add append-only run state and sequential execution records
  - Repository: auto
  - Component: `internal/state`, `internal/runtime`, run snapshot model
  - Verification: events append immutably, snapshots reconstruct flow progress, failed and skipped steps persist before stop or advance
  - Reasoning: high
  1. Define run event and snapshot structs for flow frames, step results, artifacts, run status, and failure metadata.
  2. Implement append-only `events.ndjson` writing and deterministic `snapshot.json` updates in the run directory.
  3. Build a sequential planner that executes `steps:` in order and stops on the first failed step.
  4. Reconstruct durable progress from stored state so `resume` can reopen an existing run without reloading transient shell state.

- [ ] 1.3 Implement minimal step contract with `shell`, `expr`, `assert`, and `resume`
  - Repository: auto
  - Component: `internal/executor/shell`, `internal/executor/expr`, `internal/executor/assert`, response/result contract
  - Verification: `shell` captures artifacts, `expr` returns typed values, `assert` fails structurally, interrupted runs continue from the first incomplete step
  - Reasoning: high
  1. Implement the common step-result shape with `status`, `value`, `error`, and artifact paths for `stdout`, `stderr`, and named files.
  2. Implement the `shell` executor with normalized `cwd`, artifact capture, and executor-native value handling.
  3. Implement `expr` and `assert` executors with `when` skip handling and structured failure codes.
  4. Make `resume` start from the first step that lacks a durable result and never recompute completed steps.

- [ ] 1.4 Add tests for workspace resolution, state durability, and interruption boundaries
  - Repository: auto
  - Component: CLI integration tests, runtime tests, state-store tests
  - Verification: repo-facing paths resolve from `workspace.root`, interrupted runs resume cleanly, failed steps keep their recorded error and artifacts
  - Reasoning: medium
  1. Add table-driven tests for `workspace.root` and `workspace.state_dir` normalization across spec-relative and CLI override cases.
  2. Add runtime tests for sequential success, skip, failure, and snapshot reload on `resume`.
  3. Add an integration test that interrupts after one completed step and proves the next `resume` run starts from the following step.
  4. Add assertions that persisted artifacts and run metadata live under the configured run directory layout.
