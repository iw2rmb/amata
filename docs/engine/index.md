# Engine

This document describes the shipped `amata` engine behavior in this repository.

See [Documentation](../index.md).

## Summary

`amata` is a local CLI runner with two commands:

```text
amata run <spec.yaml> [--workspace <dir>] [--run-id <id>]
amata resume <run-id>
```

The current implementation is limited to one sequential entry flow, durable on-disk run state, and three built-in executors: `shell`, `expr`, and `assert`.

## Workflow Spec

Supported top-level fields:

```yaml
version: amata/v1
name: sample
entry: main
workspace:
  root: .
  state_dir: .amata
params: {}
defaults: {}
schemas: {}
flows:
  main:
    steps: []
```

Current behavior:
- `version`, `name`, `entry`, and `flows` are required.
- `entry` must name a flow present in `flows`.
- `workspace.root` and `workspace.state_dir` are accepted and normalized before execution.
- `params`, `defaults`, and `schemas` are parsed and persisted, but the runtime does not interpret them yet.
- A step may declare `type`, or omit it when one of these shorthands is present:
  - `command` -> `shell`
  - `expr` -> `expr`
  - `assert` -> `assert`
- `when` is supported only as a literal boolean. `false` skips the step before executor dispatch.

## Workspace and Run Layout

Workspace resolution rules:
- `run` resolves the spec path to an absolute path first.
- If `--workspace` is not set, `workspace.root` resolves relative to the spec directory. When omitted, it defaults to the spec directory itself.
- If `--workspace` is set, the override resolves relative to the CLI process working directory and replaces the spec value.
- `workspace.state_dir` defaults to `.amata`. Relative values resolve from the normalized workspace root.

Each run persists under:

```text
<workspace.state_dir>/runs/<run-id>/
  spec.yaml
  events.ndjson
  snapshot.json
  artifacts/
```

`spec.yaml` stores:
- the launch command (`run`)
- the run id
- the run directory
- the original spec path
- the normalized workspace config
- the normalized workflow spec

## Execution and Durable State

Execution rules:
- The runner executes only the entry flow.
- Steps run strictly in order.
- Every durable state transition is appended to `events.ndjson`.
- `snapshot.json` is rewritten after each appended event.

The event log uses four event kinds:
- `run_initialized`
- `run_resumed`
- `step_recorded`
- `run_finished`

Snapshot rebuild rules:
- If `snapshot.json` is missing, the runner rebuilds it from `events.ndjson`.
- If `snapshot.json` is corrupt, the runner rebuilds it from `events.ndjson` and rewrites the snapshot.
- Out-of-order `step_recorded` events are rejected.

Resume rules:
- `resume` scans the current working directory subtree for `runs/<run-id>/spec.yaml`.
- No match returns a not-found error.
- Multiple matches return an ambiguity error.
- A succeeded run returns the stored terminal snapshot without appending new events.
- A failed run returns the stored failure without appending new events.
- If the latest durable step is failed but no terminal run failure was recorded yet, `resume` records `run_finished` as failed and stops without executing later steps.
- Otherwise `resume` appends `run_resumed` and continues from the first missing step.

Step context rules:
- Executors receive the normalized workspace, run directory, spec path, current step, and the last succeeded step result.
- Skipped and failed steps are never exposed as the previous step context.

## Built-in Executors

### `shell`

Supported fields:
- `command`: required string or string array
- `cwd`: optional string
- `files`: optional map of artifact name to file path

Behavior:
- A string `command` runs as `sh -lc <command>`.
- An array `command` runs as an argv-style process.
- `cwd` defaults to `workspace.root`. Relative values resolve from `workspace.root`.
- `stdout` and `stderr` are always captured under the run artifact directory.
- `files` copies named files into the run artifact directory after the command exits.
- The step succeeds with `value.exitCode`.
- Command failure or artifact-capture failure marks the step failed.

### `expr`

Supported fields:
- `expr`: required

Behavior:
- The executor returns the literal `expr` value as the step `value`.
- No expression language or context evaluation is implemented yet.

### `assert`

Supported fields:
- `assert`: required boolean
- `message`: optional string

Behavior:
- `true` succeeds with `value: true`.
- `false` fails with code `assertion_failed`.
- `message`, when present, becomes the failure message.

## Current Limits

Not implemented yet:
- expression evaluation
- schema validation
- template interpolation
- workflow-wide defaults and params evaluation
- subflows, branching, and other control blocks
- agent executors
- Git executors
- provider-session continuation
- pause and continue
- parallel execution
