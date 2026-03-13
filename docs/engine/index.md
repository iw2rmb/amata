# Engine

This document describes the shipped `amata` engine behavior in this repository.

See [Documentation](../index.md).

## Summary

`amata` is a local CLI runner with two commands:

```text
amata run <spec.yaml> [--workspace <dir>] [--run-id <id>]
amata resume <run-id>
```

The current implementation is limited to one sequential entry flow, durable on-disk run state, one shared expression runtime context, and three built-in executors: `shell`, `expr`, and `assert`.

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
- `params` are exposed to expressions and templates under `ctx.params`.
- `defaults` are parsed and persisted, but the runtime does not interpret them yet.
- `schemas` provides workflow-local JSON Schema definitions for `response.schema`.
- A step may declare `type`, or omit it when one of these shorthands is present:
  - `command` -> `shell`
  - `expr` -> `expr`
  - `assert` -> `assert`
- `when` resolves through the shared expression runtime and must evaluate to a boolean. `false` skips the step before executor dispatch.

## Expression Runtime

The shared runtime context is exposed at `ctx`:
- `ctx.workspace.root`
- `ctx.workspace.state_dir`
- `ctx.params`
- `ctx.prev`

`ctx.prev` contains the last succeeded step result:
- `index`
- `id`
- `type`
- `status`
- `value`
- `error`
- `artifacts.stdout`
- `artifacts.stderr`
- `artifacts.files`

Expression-bearing scalar positions also support two whole-scalar shorthands:
- A value starting with `$.` resolves as a Starlark expression rooted at `ctx`.
- A value starting with `$$` escapes that shorthand and becomes a literal string with one leading `$`.

Strings containing `{{ ... }}` use the same evaluator for template interpolation. A template consisting of one expression returns that raw JSON-like value instead of stringifying it.

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
- `command`, `cwd`, and `files` values resolve through the shared expression/template runtime before execution.
- `cwd` defaults to `workspace.root`. Relative values resolve from `workspace.root`.
- `stdout` and `stderr` are always captured under the run artifact directory.
- `files` copies named files into the run artifact directory after the command exits.
- The step succeeds with `value.exitCode`.
- Command failure or artifact-capture failure marks the step failed.

### `expr`

Supported fields:
- `expr`: required

Behavior:
- The executor resolves `expr` through the shared Starlark/template runtime.
- Successful results are returned as JSON-like values (`nil`, booleans, strings, numbers, arrays, and objects).

### `assert`

Supported fields:
- `assert`: required expression-bearing value that must resolve to a boolean
- `message`: optional string

Behavior:
- `assert` resolves through the shared Starlark/template runtime.
- `true` succeeds with `value: true`.
- `false` fails with code `assertion_failed`.
- `message`, when present, resolves through the shared runtime and becomes the failure message.

## Response Resolution and Schema Validation

Steps may declare:

```yaml
response:
  from: value | stdout | stderr | artifact:<name>
  schema: <json-schema-object-or-ref>
```

Current behavior:
- `response.from` defaults to `value`, which preserves the executor-native structured result.
- `stdout`, `stderr`, and named artifact sources read the captured artifact contents and publish them as step `value`.
- Artifact-backed values JSON-decode when the artifact contains valid JSON; otherwise they stay plain strings.
- `response.schema` validates the resolved `value` before `expect` runs or later steps can consume `ctx.prev.value`.
- `response.schema` may use workflow-owned refs such as `#/schemas/review_result`.
- Named schemas support the local shorthand forms used in the design examples, such as `approved: boolean`.
- Invalid response schemas fail the step with `invalid_response_schema`.
- Schema mismatches fail the step with `response_schema_mismatch`.
- Raw stdout, stderr, and named file paths remain available under `artifacts`.

## Step Conditions and Expectations

- `when` runs before executor dispatch in the normal runtime context.
- `expect` runs only after a succeeded step.
- `expect` extends the normal runtime context with the current step result at `ctx.status`, `ctx.value`, `ctx.error`, and `ctx.artifacts`.
- `expect` must resolve to a boolean. `false` fails the step with code `expectation_failed`.

## Current Limits

Not implemented yet:
- workflow-wide defaults and params evaluation
- subflows, branching, and other control blocks
- agent executors
- Git executors
- provider-session continuation
- pause and continue
- parallel execution
