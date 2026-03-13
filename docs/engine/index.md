# Engine

This document describes the shipped `amata` engine behavior in this repository.

See [Documentation](../index.md), [Engine example bundle](../../design/engine/example/README.md), and [Deferred engine research](../../research/hardcore.md).

## Summary

`amata` is a local CLI runner with two commands:

```text
amata run <spec.yaml> [--workspace <dir>] [--set key=value ...] [--run-id <id>]
amata resume <run-id>
```

The current implementation includes durable on-disk run state, one shared expression/template runtime, response value and schema handling, seven built-in executors (`shell`, `expr`, `assert`, `codex`, `claude`, `git.inspect`, and `git.commit`), and first-version control blocks (`switch` and `call`) over a deterministically planned resumable flow stack.

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
- `flows` may include named subflows that are reachable through `type: call` and synthetic `switch` branch frames.
- `workspace.root` and `workspace.state_dir` are accepted and normalized before execution.
- `params` are exposed to expressions and templates under `ctx.params`.
- Repeated `--set key=value` flags override declared `spec.params` entries for the launched run and persist inside the stored normalized spec.
- `defaults` are parsed and persisted. Agent executors currently interpret `defaults.cwd`, `defaults.env`, and `defaults.executors.codex|claude`.
- `schemas` provides workflow-local JSON Schema definitions for `response.schema`.
- A step may declare `type`, or omit it when one of these shorthands is present:
  - `command` -> `shell`
  - `expr` -> `expr`
  - `assert` -> `assert`
- `when` resolves through the shared expression runtime and must evaluate to a boolean. `false` skips the step before executor dispatch.
- `switch` and `call` currently require an explicit `type`.

## Expression Runtime

The shared runtime context is exposed at `ctx`:
- `ctx.spec.path`
- `ctx.spec.dir`
- `ctx.workspace.root`
- `ctx.workspace.state_dir`
- `ctx.params`
- `ctx.prev`

`ctx.prev` contains the last succeeded step result in the current flow frame:
- `index`
- `type`
- `status`
- `value`
- `error`
- `artifacts.stdout`
- `artifacts.stderr`
- `artifacts.files`

Declared step `id` values remain diagnostics-only. Expressions cannot read `ctx.prev.id` or use step ids as data-flow references.

The same resolver is used for:
- `expr`
- `when`
- `expect`
- shell `command`, `cwd`, and `files`
- `call.flow`

Expression-bearing scalar positions also support two whole-scalar shorthands:
- A value starting with `$.` resolves as a Starlark expression rooted at `ctx`.
- A value starting with `$$` escapes that shorthand and becomes a literal string with one leading `$`.

Strings containing `{{ ... }}` use the same evaluator for template interpolation. A template consisting of one expression returns that raw JSON-like value instead of stringifying it, so array and object values can flow through templated scalar fields unchanged.

## Workspace and Run Layout

Workspace resolution rules:
- `run` resolves the spec path to an absolute path first.
- If `--workspace` is not set, `workspace.root` resolves relative to the spec directory. When omitted, it defaults to the spec directory itself.
- If `--workspace` is set, the override resolves relative to the CLI process working directory and replaces the spec value.
- `--set key=value` overrides only declared `params` keys and decodes each value through YAML scalar or collection parsing before persistence.
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
- The runner starts from the entry flow and may push child frames for `switch` branches and `call` subflows.
- Steps run strictly in order within each flow frame.
- `switch` branch frames are planned before execution and keep stable synthetic flow names across nested branches and `resume`.
- A new child frame starts with the caller's current `ctx.prev`, then updates only within that frame until it returns.
- Every durable state transition is appended to `events.ndjson`.
- `snapshot.json` is rewritten after each appended event.

The event log uses six event kinds:
- `run_initialized`
- `run_resumed`
- `frame_pushed`
- `control_returned`
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

## Control Blocks

### `switch`

Supported fields:
- `type: switch`
- `cases`: required ordered list of branches

Behavior:
- Each case may declare `when` and `steps`.
- Cases are evaluated in order and only the first matching branch runs.
- The selected branch runs in a child flow frame.
- The step succeeds with a structured `value` containing `matched`, `case`, `status`, `value`, `error`, and `artifacts` from the nested branch result.
- `case` is the zero-based index of the matched branch.
- When no case matches, the step still succeeds with `matched: false` and `case: null`.

### `call`

Supported fields:
- `type: call`
- `flow`: required expression-bearing string naming the target flow

Behavior:
- The target flow runs in a child frame that starts with the caller's current `ctx.prev`.
- The child frame returns one structured `value` containing `flow`, `status`, `value`, `error`, and `artifacts` from the nested flow result.
- The returned value becomes the caller frame's new `ctx.prev.value` for downstream steps.

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

### `codex`

Supported fields:
- `type: codex`
- `prompt`: required expression-bearing string
- `model`: required string after applying `defaults.executors.codex`
- `reasoning`: optional string
- `cwd`: optional string
- `env`: optional map of environment variable names to string values

Behavior:
- `prompt`, `model`, `reasoning`, `cwd`, and `env` resolve through the shared expression/template runtime before execution.
- `cwd` falls back to `defaults.cwd`, then `workspace.root`.
- When `response.schema` targets `value`, the executor writes a schema artifact and requests structured JSON output through `codex exec --output-schema`.
- Raw provider stdout, stderr, the rendered prompt, the final transcript, and provider metadata persist as step artifacts.
- Without `response.schema`, the step `value` is the raw final transcript text.

### `claude`

Supported fields:
- `type: claude`
- `prompt`: required expression-bearing string
- `model`: required string after applying `defaults.executors.claude`
- `reasoning`: optional string
- `cwd`: optional string
- `env`: optional map of environment variable names to string values

Behavior:
- `prompt`, `model`, `reasoning`, `cwd`, and `env` resolve through the shared expression/template runtime before execution.
- `cwd` falls back to `defaults.cwd`, then `workspace.root`.
- When `response.schema` targets `value`, the executor uses Claude structured output support when available and otherwise appends engine-owned JSON instructions before normalizing the returned JSON into `value`.
- Raw provider stdout, stderr, the rendered prompt, the final transcript, and provider metadata persist as step artifacts.
- Without `response.schema`, the step `value` is the raw final transcript text.

### `git.inspect`

Supported fields:
- `type: git.inspect`
- `cwd`: optional string

Behavior:
- `cwd` resolves through the shared expression/template runtime before execution.
- `cwd` defaults to `workspace.root`.
- Relative `cwd` values resolve from `workspace.root`.
- The executor returns `value.isRepo`, `value.hasDiff`, and `value.files` from one repository status snapshot.
- `value.files` is a sorted array of repo-relative paths and includes untracked files.
- When `cwd` is not inside a Git work tree, the step still succeeds with `value.isRepo: false`, `value.hasDiff: false`, and `value.files: []`.

### `git.commit`

Supported fields:
- `type: git.commit`
- `message`: required string
- `cwd`: optional string
- `exclude_paths`: optional array of repo-relative path prefixes

Behavior:
- `message`, `cwd`, and `exclude_paths` items resolve through the shared expression/template runtime before execution.
- `cwd` defaults to `workspace.root`.
- Relative `cwd` values resolve from `workspace.root`.
- The executor fails with `not_git_repo` when `cwd` is not inside a Git work tree.
- Candidate paths come from one repository snapshot that includes untracked files.
- `exclude_paths` use normalized repo-relative directory-prefix matching instead of raw Git pathspec behavior.
- `workspace.state_dir` is excluded by default when it sits inside the target repository tree.
- Only the included candidate path set is staged and committed, so unrelated staged changes outside that set are not absorbed into the engine commit.
- The typed Git adapter uses `go-git` for repo discovery and status inspection, with the Git CLI limited to the internal mutation path needed for staged path-scoped commits.
- The step returns `value.committed`, `value.commit`, and `value.paths`.
- When no included changed paths remain after filtering, the step succeeds with `value.committed: false`, `value.commit: null`, and `value.paths: []`.
- When included paths remain but staging them produces no staged diff, the step succeeds with `value.committed: false`, `value.commit: null`, and `value.paths` set to the included repo-relative paths.

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
- Those temporary bindings exist only during `expect`; downstream steps still read prior results through `ctx.prev`.
- `expect` must resolve to a boolean. `false` fails the step with code `expectation_failed`.

## Current Limits

Not implemented yet:
- workflow-wide executor defaults beyond current agent-step support
- provider-session continuation
- pause and continue
- parallel execution
