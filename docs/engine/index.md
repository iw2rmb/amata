# Engine

This document describes the shipped `amata` engine behavior in this repository.

See [Documentation](../index.md).

## Summary

`amata` is a local CLI runner with two commands:

```text
amata run <spec.yaml> [--workspace <dir>] [--set key=value ...] [--run-id <id>]
amata resume <run-id>
```

The current implementation includes durable on-disk run state, one shared expression/template runtime, response value and schema handling, seven built-in executors (`shell`, `expr`, `assert`, `codex`, `claude`, `git.inspect`, and `git.commit`), and first-version control blocks (`switch`, `call`, and `for_each`) over a deterministically planned resumable flow stack.

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
- `flows` may include named subflows that are reachable through `type: call`, `call: <flow>`, and synthetic `switch` and `for_each` child frames.
- `workspace.root` and `workspace.state_dir` are accepted and normalized before execution.
- `params` are exposed to expressions and templates under `ctx.params`.
- Repeated `--set key=value` flags override declared `spec.params` entries for the launched run and persist inside the stored normalized spec.
- `defaults` are parsed and persisted. Agent executors currently interpret `defaults.cwd`, `defaults.env`, and `defaults.executors.codex|claude`.
- `schemas` provides workflow-local JSON Schema definitions for inline `response.schema` refs.
- Built-in step definitions are validated at spec load time against embedded JSON Schema files shipped under `schemas/*.amata.schema.json`.
- Shared step-schema fragments such as stall-policy and string-or-expression shapes are factored into separate embedded schema files under `schemas/`.
- Built-in step schemas may also carry embedded references to their default step `value` schema when the executor output shape is fixed, such as `shell`, `git.inspect`, and `git.commit`.
- A step may declare `type`, or omit it when one of these shorthands is present:
  - `command` -> `shell`
  - `expr` -> `expr`
  - `assert` -> `assert`
- Additional built-in shorthands expand to their canonical field form:
  - `call: <flow>` -> `type: call` plus `flow: <flow>`
  - `shell: <command>` -> `type: shell` plus `command: <command>`
  - `switch: <cases>` -> `type: switch` plus `cases: <cases>`
  - `codex: <prompt>` -> `type: codex` plus `prompt: <prompt>`
  - `claude: <prompt>` -> `type: claude` plus `prompt: <prompt>`
- `when` resolves through the shared expression runtime and must evaluate to a boolean. `false` skips the step before executor dispatch.
- `switch` and `for_each` still require an explicit `type`.

## Expression Runtime

The shared runtime context is exposed at `ctx`:
- `ctx.spec.path`
- `ctx.spec.dir`
- `ctx.workspace.root`
- `ctx.workspace.state_dir`
- `ctx.params`
- `ctx.prev`
- `ctx.item`
- `ctx.index`

`ctx.prev` contains the last succeeded step result in the current flow frame:
- `index`
- `type`
- `status`
- `value`
- `error`
- `artifacts.stdout`
- `artifacts.stderr`
- `artifacts.files`
- `prev`

`ctx.prev.prev...` walks prior succeeded steps visible in the current frame. Child frames inherit the caller's full current chain and extend it locally. Skipped and failed steps are not linked into the chain.

Declared step `id` values remain diagnostics-only. Expressions cannot read `ctx.prev.id`, `ctx.prev.prev.id`, or use step ids as data-flow references.

The same resolver is used for:
- `expr`
- `when`
- `expect`
- shell `command`, `cwd`, and `files`
- `call.flow`
- `for_each.items`

Expression-bearing scalar positions also support two whole-scalar shorthands:
- A value starting with `$.` resolves as a Starlark expression rooted at `ctx`.
- A value starting with `$$` escapes that shorthand and becomes a literal string with one leading `$`.

Strings containing `{{ ... }}` use the same evaluator for template interpolation. A template consisting of one expression returns that raw JSON-like value instead of stringifying it, so array and object values can flow through templated scalar fields unchanged.

## Workspace and Run Layout

Workspace resolution rules:
- `run` resolves the spec path to an absolute path first.
- `--workspace` defaults to `.`.
- The workspace root therefore resolves relative to the CLI process working directory unless an explicit `--workspace` value is provided.
- `workspace.root` from the spec is still parsed and persisted, but CLI launches normalize the effective root from `--workspace`.
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
- The runner starts from the entry flow and may push child frames for `switch` branches, `call` subflows, and `for_each` body iterations.
- Steps run strictly in order within each flow frame.
- `switch` branch frames are planned before execution and keep stable synthetic flow names across nested branches and `resume`.
- A new child frame starts with the caller's current `ctx.prev`, then updates only within that frame until it returns.
- Every durable state transition is appended to `events.ndjson`.
- `snapshot.json` is rewritten after each appended event.
- Durable frame state uses stable frame ids plus flat previous-step refs; recursive `ctx.prev` chains are rebuilt from those refs at runtime and on `resume`.

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

## Live Progress Stream

`amata run` and `amata resume` emit a live in-memory progress stream in parallel with durable state writes.

Stream contract:
- The live stream is best-effort UI data only. `events.ndjson` and `snapshot.json` remain the only durable resume source of truth.
- `run` emits `run_started`, then paired `step_started` and `step_finished` events for each executed control or executor step, then one terminal `run_finished`.
- `resume` emits `run_resumed` instead of `run_started`, seeds `snapshot.active` with unfinished parent control steps reconstructed from durable frames, then continues with normal `step_finished`, `step_started`, and terminal `run_finished` events.
- Every live event carries a full `snapshot` with `run_id`, current run `status`, `active` steps, completed `steps`, and terminal `failure` when present.
- Nested `switch`, `call`, and `for_each` execution is represented as stacked active steps in event snapshots. Child finishes arrive before the enclosing control step finish.

CLI stream split:
- `stdout` stays machine-readable and prints only the run id for both `run` and `resume`.
- Live progress rendering writes to `stderr`.
- The default CLI renderer uses Bubble Tea only when `stderr` is a TTY. Non-TTY `stderr` falls back to a plain line renderer with the same event order and descriptor data.
- Both renderers suppress nested control-step scaffolding in the user-facing output. Recursive `call`/`switch`/`for_each` frames nested under another control step are omitted from rendered history, and nested descriptor-less `expr` steps are omitted alongside them.
- For completed `git.commit` steps, the default renderer places `<shortCommit> <message>` on the headline and renders `+<ins> -<del> files: <n>` as the first detail line before per-file stats.
- For `codex` and `claude` prompt details, the default renderer uses `glamour/v2` markdown rendering, adds one blank line of top padding plus one character of left padding inside the prompt block, caps prompt wrapping at 80 columns, uses dim white body text, and keeps code blocks white.

Renderer metadata guarantees:
- Every `step_started` and `step_finished` event includes `flow`, `index`, `type`, `status`, and step artifacts/value/error fields that match the live transition being reported.
- Renderers may rely on `descriptor.primary_text`, `descriptor.detail_text`, and `descriptor.final_summary_details` when present, but must tolerate any field being absent for unsupported or failed descriptor resolution paths.
- `call` guarantees the resolved target flow in `primary_text` and in the completed-line summary.
- `switch` guarantees the case count while running, then either `case <n>` or `no match` in the completed-line summary.
- `for_each` guarantees the resolved item count in `primary_text` and in the completed-line summary.
- `shell` guarantees the resolved command in `primary_text` and `exit <code>` in the completed-line summary.
- `assert` guarantees the resolved assertion text in `primary_text`, optional resolved message lines in `detail_text`, and `passed` or `failed` in the completed-line summary.
- `codex` and `claude` guarantee the resolved model in `primary_text`, optional resolved reasoning alongside it when configured, and resolved prompt text in `detail_text`.
- `git.inspect` guarantees the resolved `cwd` while running, then a completed-line summary of `clean`, `not repo`, or `<n> files`, with changed file paths in `detail_text` when applicable.
- `git.commit` guarantees the resolved commit message in `detail_text` while running. After a commit it guarantees `shortCommit` plus `files <n> +<ins> -<del>` in the completed-line summary, with per-file `+<ins> -<del> path` lines in `detail_text`. When no commit is created it guarantees `no changes`.

## Control Blocks

### `switch`

Supported fields:
- `switch`: shorthand for `cases`
- `type: switch`
- `cases`: required ordered list of branches

Behavior:
- Each case may declare `when` and `steps`.
- `when: <expr>` is shorthand for `when: {expr: <expr>}`.
- `default: <expr>` is an alias for `when: {expr: <expr>}` on a branch object. An unconditional fallback branch still omits both `when` and `default`.
- Cases are evaluated in order and only the first matching branch runs.
- The selected branch runs in a child flow frame.
- The step succeeds with a structured `value` containing `matched`, `case`, `status`, `value`, `error`, and `artifacts` from the nested branch result.
- `case` is the zero-based index of the matched branch.
- When no case matches, the step still succeeds with `matched: false` and `case: null`.

### `call`

Supported fields:
- `type: call`
- `call`: shorthand for `flow`
- `flow`: required expression-bearing string naming the target flow

Behavior:
- The target flow runs in a child frame that starts with the caller's current `ctx.prev`.
- The child frame returns one structured `value` containing `flow`, `status`, `value`, `error`, and `artifacts` from the nested flow result.
- The returned value becomes the caller frame's new `ctx.prev.value` for downstream steps.

### `for_each`

Supported fields:
- `type: for_each`
- `items`: required expression-bearing value that must resolve to an array
- `steps`: required ordered list of loop-body steps
- `as`: optional string alias for the current item

Behavior:
- The loop evaluates `items` once before the first iteration.
- Each iteration runs the loop body in a child frame with the caller's current `ctx.prev`.
- Inside the loop body, the current item is exposed as `ctx.item`, the zero-based position as `ctx.index`, and when `as` is provided the same item is also exposed as `ctx.<as>`.
- The step succeeds with a structured `value` containing `count`, `index`, `item`, `as`, `status`, `value`, `error`, and `artifacts` from the final iteration body result.
- When `items` resolves to an empty array, the step still succeeds with `count: 0`, `index: null`, and `item: null`.

## Built-in Executors

### `shell`

Supported fields:
- `shell`: shorthand for `command`
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
- `codex`: shorthand for `prompt`
- `prompt`: required expression-bearing string
- `model`: required string after applying `defaults.executors.codex`
- `reasoning`: optional string
- `cwd`: optional string
- `env`: optional map of environment variable names to string values

Behavior:
- `prompt`, `model`, `reasoning`, `cwd`, and `env` resolve through the shared expression/template runtime before execution.
- `cwd` falls back to `defaults.cwd`, then `workspace.root`.
- When `response.schema` targets `value`, the executor accepts either an inline schema/ref or a path-like string to a `.json` schema file relative to the workflow file.
- Inline Codex schemas are expanded into a provider-safe object schema artifact before `codex exec --output-schema`.
- File-backed Codex schemas are passed through to `codex exec --output-schema` by absolute path instead of being copied into the prompt.
- Raw provider stdout, stderr, the rendered prompt, the final transcript, and provider metadata persist as step artifacts.
- Without `response.schema`, the step `value` is the raw final transcript text.

### `claude`

Supported fields:
- `type: claude`
- `claude`: shorthand for `prompt`
- `prompt`: required expression-bearing string
- `model`: required string after applying `defaults.executors.claude`
- `reasoning`: optional string
- `cwd`: optional string
- `env`: optional map of environment variable names to string values

Behavior:
- `prompt`, `model`, `reasoning`, `cwd`, and `env` resolve through the shared expression/template runtime before execution.
- `cwd` falls back to `defaults.cwd`, then `workspace.root`.
- When `response.schema` targets `value`, the executor uses Claude structured output support when available by passing `--output-format json` and `--json-schema`, and otherwise appends engine-owned JSON instructions before normalizing the returned JSON into `value`.
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
  from: value | stdout | stderr | stdout_lines | stderr_lines | artifact:<name>
  schema: <json-schema-object-or-ref-or-path>
```

or the schema-only shorthand:

```yaml
response: <json-schema-object-or-ref-or-path>
```

Current behavior:
- `response.from` defaults to `value`, which preserves the executor-native structured result.
- `stdout`, `stderr`, `stdout_lines`, `stderr_lines`, and named artifact sources read the captured artifact contents and publish them as step `value`.
- `stdout_lines` and `stderr_lines` split the captured text into newline-delimited string arrays.
- Artifact-backed values JSON-decode when the artifact contains valid JSON; otherwise they stay plain strings.
- `response.schema` validates the resolved `value` before `expect` runs or later steps can consume `ctx.prev.value`.
- `response.schema` may use workflow-owned refs such as `#/schemas/review_result`.
- `response.schema` may also be a path-like string to a `.json` schema file, resolved relative to `ctx.spec.dir`.
- `response: <schema>` is equivalent to `response: {schema: <schema>}`.
- Named schemas support the local shorthand forms used in the design examples, such as `approved: boolean`.
- For Codex structured output, the resolved top-level response schema must be an object schema. Provider-unsupported schema keywords fail the step with `invalid_response_schema` before `codex exec` runs.
- Invalid response schemas fail the step with `invalid_response_schema`.
- Schema mismatches fail the step with `response_schema_mismatch`.
- Raw stdout, stderr, and named file paths remain available under `artifacts`.

## Step Conditions and Expectations

- `when` runs before executor dispatch in the normal runtime context.
- `when: <expr>` is shorthand for `when: {expr: <expr>}`.
- `expect` runs only after a succeeded step.
- `expect` extends the normal runtime context with the current step result at `ctx.status`, `ctx.value`, `ctx.error`, and `ctx.artifacts`.
- Those temporary bindings exist only during `expect`; downstream steps still read prior results through `ctx.prev`.
- `expect` must resolve to a boolean. `false` fails the step with code `expectation_failed`.

## Stall Policy

Steps may declare:

```yaml
stall: rerun | error
```

or:

```yaml
stall:
  after: <minutes-or-duration>
  type: rerun | error | call
  flow: <flow-name> # required for type: call
```

Current behavior:
- `stall` is optional and applies to normal executor steps such as `shell`, `codex`, `claude`, `git.inspect`, and `git.commit`.
- String form defaults to `after: 15` minutes.
- Object form defaults to `type: rerun` and `after: 15` minutes when omitted.
- `after` accepts either a numeric minute value such as `15` or a duration string such as `30s` or `5m`.
- `rerun` cancels the stalled executor attempt and starts the same step again with the same resolved inputs.
- `error` cancels the stalled executor attempt and fails the run with code `step_stalled`.
- `call` cancels the stalled executor attempt and runs the named flow instead; the fallback flow's terminal result becomes the stalled step's result.
- Each execution attempt uses a distinct artifact directory, so repeated loop iterations and reruns do not overwrite prior stdout, stderr, prompt, transcript, or file artifacts.

## Current Limits

Not implemented yet:
- workflow-wide executor defaults beyond current agent-step support
- provider-session continuation
- pause and continue
- parallel execution
