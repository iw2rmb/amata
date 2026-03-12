# Engine Design

## Summary

Define a local-first workflow engine for coding-agent-driven development flows.

`amata/v1` is intentionally small. It should replace stringly typed shell orchestration with typed step results, simple built-in control flow, straightforward folder handling, durable on-disk run state, and leaf executors for shell, Codex, Claude, git, and small domain helpers.

The implementation stack for the engine itself is Go. The engine should use `go-git` as the typed default Git layer and fall back to the `git` CLI only through narrow internal adapters when exact CLI parity or unsupported behavior is required.

The reference outcome is that the current `implement-roadmap` workflow can be expressed mostly in YAML, with Codex selecting the next open roadmap item directly from the roadmap file and shell used only for true leaf commands or small plugins such as `git.commit` and `git.inspect`.

See the reference example bundle in `design/engine/example/`, especially `example/implement-roadmap.yaml`, `example/plugins.yaml`, and `example/README.md`.

## Scope

In scope:
- A single-machine CLI runner for local development workflows.
- A Go implementation for the core engine runtime and CLI.
- A YAML workflow spec format.
- Explicit workspace and state-directory handling.
- Persistent run state and basic `resume <run-id>` semantics for completed steps.
- Typed step outputs with schema validation.
- Built-in control-flow blocks for sequence, branching, iteration, subflow calls, and assertions.
- Built-in `shell`, `codex`, and `claude` executors.
- Pluggable step executors.
- A reference shape for the `implement-roadmap` workflow where Codex picks the next open item directly from the roadmap file.

Out of scope:
- Distributed scheduling, worker pools, or remote queues.
- Automatic retry or attempt policy.
- Supporting multiple implementation-language stacks for the core engine.
- Provider-session recovery such as `codex exec resume` or `claude` resume.
- Parallel execution.
- Human-in-the-loop approval gates.
- Pause and continue semantics.
- Full sandboxing or container orchestration as a mandatory runtime.
- UI work beyond a CLI.

Deferred design work for those topics lives in [research/hardcore.md](../../research/hardcore.md).

## Why This Is Needed

The current Dagu-based flow works only after pushing most orchestration into shell helpers, which defeats the purpose of a declarative workflow layer.

Concrete pressures:
- `dagu/implement-roadmap/implement-roadmap.yaml` sequences only coarse phases; real iteration and routing live elsewhere.
- `dagu/implement-roadmap/scripts/implement-open-items-loop.sh` owns the roadmap loop, item selection, JSON validation, and commit boundaries.
- `dagu/implement-roadmap/scripts/common.sh` carries path expansion and path resolution helpers because the workflow layer does not own folder semantics directly.
- `dagu/implement-roadmap/scripts/run-codex-prompt.sh` persists prompt/output artifacts and session metadata because agent outputs are not first-class workflow values.
- `dagu/implement-roadmap/scripts/commit-if-changed.sh` has to manually exclude workflow state from commits because operational state is not part of the engine contract.
- The current repo already contains a smoke test for the Dagu workflow because correctness depends on behavior that the orchestrator itself does not enforce: `dagu/implement-roadmap/test/smoke.sh`.

The first engine version should fix the core failure mode, not solve every advanced orchestration problem at once. The core failure to avoid is silent misrouting caused by raw stdout strings, ad hoc shell loops, and ambiguous folder resolution.

## Goals

- Make YAML the owner of orchestration logic.
- Keep shell as a leaf executor, not the control plane.
- Make folder resolution boring and explicit.
- Use adjacent data flow through `ctx.prev` and `ctx.next`, not global step-id lookup.
- Make every step produce a typed `value` validated against a declared schema when requested.
- Keep raw process and agent artifacts available without making them the main data model.
- Persist completed-step results so interrupted runs can continue without rerunning already completed work.
- Let agents operate directly on roadmap files in `amata/v1`.
- Keep the first version small enough to implement and verify quickly.

## Non-goals

- Becoming a general-purpose distributed workflow platform.
- Designing a full recovery framework in `amata/v1`.
- Supporting provider-session continuation in `amata/v1`.
- Supporting parallel fan-out in `amata/v1`.
- Supporting human approval checkpoints in `amata/v1`.
- Supporting pause and continue in `amata/v1`.
- Replacing normal shell access when shell is the right tool.

## Current Baseline (Observed)

- The current top-level workflow file is small because orchestration moved out of YAML and into shell: `dagu/implement-roadmap/implement-roadmap.yaml`.
- Open roadmap item handling is procedural bash:
  - select next item
  - invoke agent
  - validate JSON
  - invoke review agent
  - commit
  - re-read roadmap
  This logic lives in `dagu/implement-roadmap/scripts/implement-open-items-loop.sh`.
- Markdown roadmap parsing is implemented as shell helpers with regex matching in `dagu/implement-roadmap/scripts/common.sh`.
- Folder handling is also implemented as shell helpers:
  - `expand_home_path`
  - `resolve_path_from`
  That logic currently lives in `dagu/implement-roadmap/scripts/common.sh`.
- Workflow runtime state is managed by shell wrappers under `.amata/`, and commit exclusion is still manual.
- Agent execution is shell-wrapped in `dagu/implement-roadmap/scripts/run-codex-prompt.sh`.
- The current workflow already uses the simpler prompt shape that the engine should preserve for v1: `implement next open item from the <roadmap-file-path>`. See `dagu/implement-roadmap/README.md`.

## Target Contract or Target Architecture

### 1. Workspace and Runtime Model

Every run has one workspace root and one state directory.

Required runtime invariants:
- `workspace.root` is the base directory for repo-facing relative paths.
- `workspace.state_dir` defaults to `.amata`.
- If `workspace.state_dir` is relative, it resolves from `workspace.root`.
- Relative paths in imported specs or plugin registries resolve from the declaring file.
- The engine normalizes declared filesystem paths before step execution and passes absolute paths to plugins.
- Every completed, failed, or skipped step execution produces a structured result object.
- Completed step results are durably recorded before later steps can consume them.

Suggested run-state layout:
- `<workspace.root>/<workspace.state_dir>/runs/<run-id>/spec.yaml`
- `<workspace.root>/<workspace.state_dir>/runs/<run-id>/events.ndjson`
- `<workspace.root>/<workspace.state_dir>/runs/<run-id>/snapshot.json`
- `<workspace.root>/<workspace.state_dir>/runs/<run-id>/artifacts/...`

Rules:
- Repo-facing step paths such as roadmap files, output files, and plugin config paths resolve from `workspace.root` unless they are already absolute.
- Registry-facing paths such as `plugins.<type>.exec` resolve from the registry file that declared them.
- `git.commit` should exclude `workspace.state_dir` by default when that directory is inside the target repository tree.
- `git.inspect` should report repo state as typed data instead of requiring workflows to scrape git stdout.
- `amata/v1` does not attempt provider-session continuation. If the process stops during an in-flight step, `resume` reruns that step from its last durable boundary.

### 2. CLI

Minimum CLI surface:

```text
amata run <spec.yaml> [--workspace <dir>] [--set key=value ...] [--run-id <id>]
amata resume <run-id>
```

Optional but useful:

```text
amata show <run-id> [--json]
```

Contract:
- `run` copies the resolved spec into the run directory and records the normalized workspace settings used for the run.
- `--workspace` overrides `workspace.root` for the launched run.
- `resume` always uses the stored spec and stored workspace from the existing run.
- `amata/v1` does not support resuming a run against a different spec.

### 3. Implementation Stack

The core engine is implemented in Go.

Rules:
- The CLI, spec loader, schema validator, execution runtime, built-in executors, plugin host, and durable run-state store are Go packages in one engine codebase.
- Engine-owned Git operations should use `go-git` as the typed default layer for repository inspection and basic local mutations such as status, add, and commit.
- Engine-facing workflow contracts for Git state should be derived from typed Go models rather than by scraping porcelain text.
- The engine may invoke the `git` CLI only behind a narrow internal adapter for operations that `go-git` does not support well enough or where exact Git CLI behavior is explicitly required.
- The fallback adapter must return typed engine data. Raw `git` stdout must not become the workflow contract.
- The polyglot scripts in `design/engine/example/` are illustrative only. They are not the implementation-language contract for the engine.

### 4. Execution Context

`ctx.path` is the append-only execution history. It is available for inspection and debugging, not as the primary data-flow mechanism.

Each `ctx.path` entry contains at least:
- `ref`
- `flow`
- `status`
- `started_at`
- `finished_at`
- `inputs`
- `value`
- `error`
- `artifacts`

Primary runtime references:
- `ctx.prev`
  - the previous completed step result in the current flow frame, if any
- `ctx.next`
  - the next input item in the current repeated scope, if any
- `ctx.this`
  - the current loop item or current subflow input
- `ctx.inputs`
  - the explicit inputs for the current called flow
- `ctx.path`
  - global execution history for diagnostics

Rules:
- `ctx.prev` is the primary way to consume upstream data in `amata/v1`.
- Expressions must not reference earlier steps by declared step `id`.
- If a later step needs older data, the previous step should carry that data forward in its own `value`.
- `ctx.next` never points to a future step result. It only exposes the next input item in collection-aware scopes.
- `ctx.path` is for inspection, not normal orchestration.

### 5. Expressions

Default expression language: Starlark.

Expression engine contract:
- The engine receives an immutable JSON-like `ctx`.
- The engine returns a JSON-serializable value.
- Expressions perform computation only; they do not perform IO.

Default-language expression object:

```yaml
when:
  expr: ctx.prev.value["hasOpenItem"]
```

Explicit engine selection:

```yaml
when:
  lang: js
  expr: |
    return ctx.prev.value["hasOpenItem"];
```

Rules:
- Starlark is built in and must be available in every run.
- Other engines are registered by plugin name.
- Template interpolation uses the same expression registry.
- Fields that may hold either a literal value or a computed value use the object form to distinguish expressions from literals by default.
- In `amata/v1`, any expression-accepting scalar position may also use whole-scalar root-context shorthand. A scalar whose entire value starts with `$.` desugars to `{ expr: <same expression with `$` replaced by `ctx`> }`.
- Root-context shorthand is valid only when the entire YAML scalar is the expression. It does not perform string interpolation inside larger strings.
- Templates keep `{{ ... }}` syntax.
- In literal-or-expression positions, a whole scalar starting with `$$` escapes the shorthand and becomes a literal string with one leading `$`.
- Expression-only positions may define shorthand syntax. In `amata/v1`, the required shorthand is the `expr` step form described below.
- Executor-specific shorthand may omit `type` when exactly one built-in executor is implied by the step's fields. In `amata/v1`, this applies to `expr` via `expr:`, to `assert` via `assert:`, and to `shell` via `command:`.

### 6. Step Result Contract

Every step execution returns:

```yaml
status: succeeded | failed | skipped
value: <typed JSON-like value or null>
error:
  code: <string>
  message: <string>
artifacts:
  stdout: <path or null>
  stderr: <path or null>
  files: <named artifact map>
```

Schema handling:
- A step may declare `response.schema`.
- When declared, the engine validates `value` before making it available downstream.
- Validation failure marks the step as failed with a structured error.

Artifact rules:
- Shell and agent raw outputs are artifacts first.
- Downstream expressions should normally use `value`, not parse `stdout`.

### 7. YAML Building Blocks

#### Top-level spec

```yaml
version: amata/v1
name: <workflow-name>
entry: <flow-name>
workspace:
  root: .
  state_dir: .amata
params: {}
defaults: {}
schemas: {}
flows: {}
```

#### Workspace

`workspace` declares the run's working root and the engine-owned state directory.

Example:

```yaml
workspace:
  root: design/engine/example/fixture-repo
  state_dir: .amata
```

Rules:
- `workspace.root` is required after normalization.
- `workspace.state_dir` defaults to `.amata` when omitted.
- Both values may be absolute or relative before normalization.
- Relative `workspace.root` resolves from the spec file's directory or from `--workspace` when the CLI override is used.

#### Params

`params` declares workflow inputs and optional defaults.

Scalar shorthand is allowed for simple typed defaults:

```yaml
params:
  roadmap_file: "roadmap/index.md"
  codex_model: "gpt-5.4"
  dry_run: false
```

This normalizes to:

```yaml
params:
  roadmap_file:
    type: string
    default: "roadmap/index.md"
  codex_model:
    type: string
    default: "gpt-5.4"
  dry_run:
    type: boolean
    default: false
```

Rules:
- Scalar shorthand is valid only for `string`, `number`, and `boolean` defaults.
- Object form is required when the param needs metadata or validation such as `description`, `enum`, `required`, `secret`, or non-literal defaults.
- Object and array defaults must use the full object form.

#### Defaults

`defaults` carries workflow-wide runtime defaults and per-executor default config.

Example:

```yaml
defaults:
  cwd: $.workspace.root
  expr_lang: starlark
  executors:
    codex:
      model: $.params.codex_model
    claude:
      model: $.params.claude_model
    git.commit:
      exclude_paths:
        - $.workspace.state_dir
```

Rules:
- `defaults.executors.<type>` applies to steps whose declared `type` is `<type>`.
- Mapping values are deep-merged. Scalar values and arrays are replaced by the step-local value.
- Executor defaults are resolved after workspace and param defaults and before step execution.
- The `$.` shorthand applies after YAML parsing, so quoted and unquoted whole scalars behave the same.

#### Schemas

`schemas` holds named reusable schemas for `response.schema`, plugin `config_schema`, and local `$ref` targets.

Schema shorthand is allowed in schema-valued positions when the schema node is only a built-in scalar or object type:

```yaml
schemas:
  review_result:
    type: object
    required: [approved, notes]
    additionalProperties: false
    properties:
      approved: boolean
      notes: string
```

This normalizes to:

```yaml
schemas:
  review_result:
    type: object
    required: [approved, notes]
    additionalProperties: false
    properties:
      approved:
        type: boolean
      notes:
        type: string
```

Rules:
- Schema shorthand is valid only for `string`, `number`, `boolean`, and `object`.
- Schema shorthand may appear anywhere a schema object is expected, including named schemas, `properties.<name>`, and `items`.
- The shorthand normalizes to `{ type: <keyword> }` before validation and `$ref` resolution.
- Full object form is required when the schema node uses any other keywords such as `$ref`, `enum`, `format`, `items`, `properties`, `required`, or `additionalProperties`.
- `object` shorthand means an unconstrained object schema. Constrained object schemas must use the full object form.

#### Flow

```yaml
flows:
  main:
    inputs: {}
    steps: []
```

#### Common step fields

```yaml
- id: <optional-diagnostic-label>
  type: <built-in-or-plugin-type>
  when: <expression-or-null>
  expect: <expression-or-null>
  timeout: 10m
  response:
    from: value | stdout | stderr | artifact:<name>
    schema: <json-schema-object-or-ref>
```

Sequential order is implicit in the `steps:` list.

Rules:
- `id` exists for diagnostics and artifacts. It is not a data-flow reference target.
- `when` is evaluated before step execution. When it evaluates to false, the engine records a `skipped` result and continues.
- `expect` is evaluated after the step produces its result and after `response.schema` validation succeeds.
- `expect` runs in the normal runtime context extended with the current step result at `ctx.status`, `ctx.value`, `ctx.error`, and `ctx.artifacts`.
- If `expect` evaluates to false, the step itself fails with an expectation error.
- Use `expect` for direct postconditions on the same step. Use a standalone `assert` step for broader invariants.
- Failure handling in `amata/v1` is stop-on-failure. Richer retry and recovery semantics are deferred to [research/hardcore.md](../../research/hardcore.md).

#### Built-in step types

- `shell`
  - Runs a command.
  - Can capture `stdout`, `stderr`, exit code, and parsed `value`.
  - A step may omit `type: shell` when `command` is the only executor-specific field.
  - Example:

```yaml
- command:
    - mkdir
    - -p
    - $.workspace.state_dir
```

- `codex`
  - Runs a Codex step.
  - Required fields: `prompt`, `model`.
  - Optional fields: `reasoning`, `cwd`, `env`.
  - When `response.schema` is declared and `response.from` is omitted or set to `value`, the executor must request structured JSON output for `value`.
  - Prompts should describe task semantics, not restate JSON wrappers already implied by `response.schema`.

- `claude`
  - Runs a Claude step.
  - Required fields: `prompt`, `model`.
  - Optional fields: `reasoning`, `cwd`, `env`.
  - When `response.schema` is declared and `response.from` is omitted or set to `value`, the executor should request structured JSON output when the provider supports it, or normalize the provider output before schema validation.

- `expr`
  - Evaluates an expression and returns its value.
  - A step may omit `type: expr` when `expr` is the only executor-specific field.
  - Example:

```yaml
- expr: $.prev.value
```

- `assert`
  - Fails the run when an expression is false.
  - A step may omit `type: assert` when `assert` is the only executor-specific field.
  - Prefer `expect` when the check is only about the result of the same step.
  - Example:

```yaml
- assert: $.prev.value["approved"]
```

- `foreach`
  - Iterates over an input collection.
  - `items` may be supplied as a literal collection, an expression object, or whole-scalar `$.` shorthand.
  - Child steps execute with `ctx.this`, `ctx.prev`, and `ctx.next`.
  - Returns an array of child flow results.

- `switch`
  - Evaluates cases in order and executes the first matching branch.

- `call`
  - Invokes a named subflow with explicit inputs.

#### Plugin step types

Any non-built-in `type` is resolved through the executor registry.

The plugin contract is:
- receive validated step config
- receive engine-managed execution metadata appropriate to the runtime boundary
- return a standard step result object

In the Go implementation, the standard Git executors `git.inspect` and `git.commit` are engine-owned and backed by the typed Git layer. They are not the preferred use case for the external plugin protocol.

The example bundle in [example/README.md](example/README.md) includes a concrete, non-normative plugin registry file at [example/plugins.yaml](example/plugins.yaml). That registry demonstrates the external plugin process contract and may still show the standard Git step types wired through scripts as a transitional example, but the engine should treat them as first-class built-in capabilities.

Plugin registry entries may declare `config_schema` so the engine can validate plugin config before launching the external process.

Example:

```yaml
plugins:
  git.inspect:
    exec:
      - sh
      - scripts/git_inspect.sh
    config_schema:
      type: object
      additionalProperties: false
      properties:
        include_untracked: boolean
  git.commit:
    exec:
      - python3
      - scripts/git_commit.py
    config_schema:
      type: object
      required: [message]
      additionalProperties: false
      properties:
        message: string
        exclude_paths:
          type: array
          items:
            type: string
            format: path
```

Rules:
- The engine evaluates expressions and applies executor defaults before validating plugin config.
- `config_schema` uses JSON Schema plus engine-specific annotations such as `format: path` for filesystem-path normalization.
- The engine should reject invalid plugin config before process spawn rather than asking the plugin script to repeat structural validation.
- External plugins may assume `request.config` already conforms to the declared schema.
- External plugins are for non-core executor types and repo-specific extensions. Standard Git executors remain engine-owned even when the example bundle shows them through the plugin protocol.
- Check-oriented plugins such as `git.inspect` should succeed for normal negative cases and return typed booleans instead of failing for states like "not a git repo".
- Related repo facts such as "is repo", "has diff", and "files" should come from one plugin result so workflows do not race across several independent git probes.

#### Plugin process request contract

When a plugin runs as an external process, the engine should send a single JSON request on stdin.

Minimum request shape:

```json
{
  "run": {
    "id": "run-20260312-001",
    "dir": "/abs/repo/.amata/runs/run-20260312-001"
  },
  "step": {
    "id": "commit_item",
    "ref": "step-7",
    "artifacts_dir": "/abs/repo/.amata/runs/run-20260312-001/artifacts/step-7"
  },
  "workspace": {
    "root": "/abs/repo",
    "cwd": "/abs/repo",
    "state_dir": "/abs/repo/.amata"
  },
  "config": {
    "message": "feat: implement sample feature"
  }
}
```

Rules:
- `config` contains the plugin-specific step config after expression evaluation, default application, schema validation, and path normalization.
- The engine, not the plugin, owns process setup such as working directory, run metadata, and artifact-directory allocation.
- Filesystem path fields should be normalized before plugin invocation when the plugin contract declares them as filesystem paths.
- Plugins should not recover core execution metadata from process state when the engine can provide it explicitly.
- The process result still uses the standard step result object on stdout.

SDK guidance:
- The engine may ship small language-specific SDK helpers for this protocol.
- Those helpers should focus on request parsing and step-result encoding, not domain-specific behavior.
- The example bundle includes a Python SDK sketch at [example/sdk/python.py](example/sdk/python.py).

### 8. Templates

Fields such as prompts may be templates. Template expressions use the same engine registry as normal expressions.

Example:

```yaml
prompt: |
  Implement next open item from the {{ ctx.params.roadmap_file }}.
```

### 9. Failure and Resume Semantics

Rules:
- A failed or skipped step records its structured result and artifacts before the run stops or advances.
- `resume` continues from the first step that does not yet have a durable result in the stored run state.
- Completed step results are not recomputed during `resume`.
- `amata/v1` does not define attempts separately from retries because neither feature is part of the first-version contract.
- Automatic provider-session recovery, pause/continue behavior, and human intervention are deferred to [research/hardcore.md](../../research/hardcore.md).

### 10. Minimal Standard Plugin Set

The core engine should stay small. The required standard plugins in `amata/v1` are:
- `git.commit`
- `git.inspect`

Other plugin categories may be added later without growing the first-version contract.

`git.inspect` returns a typed snapshot of the current working tree:

```yaml
isRepo: <boolean>
hasDiff: <boolean>
files: <array of repo-relative paths>
```

Rules:
- In the Go engine, `git.inspect` and `git.commit` are standard engine-owned executors backed by the typed Git layer, even though the example bundle may wire them through external scripts to demonstrate the plugin contract.
- `git.inspect` succeeds with `isRepo: false`, `hasDiff: false`, and `files: []` when the current working directory is not inside a git work tree.
- `hasDiff` and `files` must be derived from the same plugin execution so later steps observe one consistent repo snapshot.
- `git.inspect` includes untracked files by default. Plugin config may allow stricter behavior, but the default must be inclusive.

## Implementation Notes

Suggested internal boundaries:
- Spec loader and schema validator.
- Workspace resolver and path normalizer.
- Workflow planner and execution context builder.
- Event store and snapshot writer.
- Built-in executor implementations.
- Expression engine registry.
- Plugin registry for executor types.
- CLI layer for `run`, `resume`, and `show`.

Important implementation notes:
- Prefer a single statically linked Go CLI over a polyglot runtime mesh for the engine core.
- Resolve folder semantics in the engine, not in ad hoc shell helpers.
- Do not model downstream state as shell-expanded environment variables.
- Keep execution records immutable after they are appended.
- Make template rendering and expression evaluation use the same type system.
- Treat plugin executors and built-in executors uniformly at the runtime boundary.
- Make agent structured-output mode derive from `response.schema` rather than repeated prompt boilerplate.
- Validate plugin step config in the engine, not ad hoc inside every plugin script.
- Keep the `go-git` boundary narrow and typed. If the engine has to fall back to the `git` CLI, isolate that code behind one package rather than scattering shell-outs across executors.

## Milestones

### Milestone 1: Core runner, workspace model, and durable state

Scope:
- Spec parser.
- Workspace root and state-dir normalization.
- Flow and step model.
- Event log and snapshot state.
- `run`, `resume`, and `show`.

Expected results:
- Simple sequential flows execute with durable completed-step state.
- Relative paths behave predictably.

Testable outcome:
- A workflow with `shell`, `expr`, and `assert` can be interrupted after a completed step and resumed without rerunning already completed steps.
- A workflow launched from outside the repo still resolves repo-facing relative paths from `workspace.root`.

### Milestone 2: Expressions and first-version control blocks

Scope:
- Starlark engine.
- `switch`, `call`, and `foreach`.
- `ctx.prev`, `ctx.next`, `ctx.this`, `ctx.inputs`, and `ctx.path`.

Expected results:
- Adjacent-step data flow becomes explicit without step-id lookups.

Testable outcome:
- A workflow with subflows and loops can pass data forward through `ctx.prev` and `ctx.this` only.

### Milestone 3: Codex, Claude, plugins, and the roadmap example

Scope:
- `codex` executor.
- `claude` executor.
- Plugin registry.
- `git.inspect` plugin.
- `git.commit` plugin.
- Reference `implement-roadmap` workflow.

Expected results:
- The `implement-roadmap` example executes mostly in YAML with Codex selecting the next open item from the roadmap file.

Testable outcome:
- A smoke workflow equivalent to `implement-roadmap` executes with built-in control flow and the `git.commit` step only.
- Engine-owned Git state and commit flows use the Go Git layer by default and do not depend on parsing shell `git status` output in the main runtime.

## Acceptance Criteria

- The engine can express the reference `implement-roadmap` flow without external shell drivers for queue management or routing.
- The reference workflow uses the prompt shape `Implement next open item from the <file>`.
- Step outputs are consumed through `ctx.prev`, `ctx.this`, and templates, not by referencing earlier steps by `id`.
- Relative repo-facing paths resolve from `workspace.root`.
- Registry-side executable paths resolve from the declaring registry file.
- Completed steps do not rerun after interruption and `resume`.
- The core engine implementation is Go.
- Engine-owned Git inspection uses `go-git` as the default typed layer, with any `git` CLI fallback isolated behind an internal adapter.
- The reference `implement-roadmap` workflow uses only `git.commit` as its standard Git step dependency, while the example bundle may still expose `git.inspect` and `git.commit` through transitional script wiring for plugin-protocol examples.
- Deferred features are tracked in [research/hardcore.md](../../research/hardcore.md).

## Risks

- Letting Codex pick the next open roadmap item is less deterministic than a dedicated roadmap parser.
- Without advanced recovery, long-running agent steps may still need a full rerun after interruption.
- `go-git` does not cover every Git behavior with full CLI parity, so the engine will still need a narrow escape hatch for unsupported or parity-sensitive operations.
- Repo-local state under `.amata/` still requires commit exclusion discipline.
- The first version may need richer data-passing helpers later if adjacent `prev`-based flow proves too restrictive.

## References

- Self-contained reference bundle: [example/README.md](example/README.md)
- Current Dagu workflow overview: [README.md](../../dagu/implement-roadmap/README.md)
- Current orchestration baseline:
  - `dagu/implement-roadmap/implement-roadmap.yaml`
  - `dagu/implement-roadmap/scripts/common.sh`
  - `dagu/implement-roadmap/scripts/implement-open-items-loop.sh`
  - `dagu/implement-roadmap/scripts/run-codex-prompt.sh`
  - `dagu/implement-roadmap/scripts/commit-if-changed.sh`
  - `dagu/implement-roadmap/test/smoke.sh`
- Deferred design research: [research/hardcore.md](../../research/hardcore.md)
