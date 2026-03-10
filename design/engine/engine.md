# Engine Design

## Summary

Define a local-first workflow engine for coding-agent-driven development flows. The engine must replace stringly typed orchestration and shell-script control loops with typed step results, built-in control-flow blocks, resumable run state, and pluggable executors for shell, agents, git, and domain-specific helpers.

The reference outcome is that the current `implement-roadmap` workflow can be expressed mostly in YAML, with shell used only for true leaf commands or small executor plugins, not for queue management, iteration, routing, or state persistence.

See the reference example bundle in `design/engine/example/`, especially `implement-roadmap.yaml` and `plugins.yaml`.

## Scope

In scope:
- A single-machine CLI runner for local development workflows.
- YAML workflow spec format.
- Persistent run state and `resume <run-id>` semantics.
- Typed step outputs with schema validation.
- Built-in control-flow blocks for sequence, branching, iteration, subflow calls, assertions, and parallel work.
- Built-in shell and agent executors.
- Pluggable step executors and pluggable expression languages.
- A reference shape for the `implement-roadmap` workflow.

Out of scope:
- Distributed scheduling, worker pools, or remote queues.
- Multi-tenant service concerns.
- Full sandboxing or container orchestration as a mandatory runtime.
- Replacing every external tool with a built-in primitive.
- UI work beyond a CLI.

## Why This Is Needed

The current Dagu-based flow works only after pushing most orchestration into shell helpers, which defeats the purpose of a declarative workflow layer.

Concrete pressures:
- `dagu/implement-roadmap/implement-roadmap.yaml` now sequences only coarse phases; real iteration and routing live elsewhere.
- `dagu/implement-roadmap/scripts/implement-open-items-loop.sh` and `dagu/implement-roadmap/scripts/fix-queue.sh` own loops, temp files, JSON parsing, and commit boundaries.
- `dagu/implement-roadmap/scripts/common.sh` parses roadmap markdown with bash and `jq`, then shell code passes that state around via stdout files.
- `dagu/implement-roadmap/scripts/run-codex-prompt.sh` wraps agent execution in temp files because agent outputs are not first-class workflow values.
- `dagu/implement-roadmap/scripts/commit-if-changed.sh` has to manually exclude workflow state from commits because the workflow engine does not understand operational state vs repo state.
- The draft in [yaml.md](../../yaml.md) already identifies the missing primitives: typed responses, resume support, explicit control flow, and expression evaluation over run history.

The core failure to avoid is silent misrouting when structured outputs are treated like raw strings. The engine must own structured state directly.

## Goals

- Make YAML the owner of orchestration logic.
- Keep shell as a leaf executor, not the control plane.
- Make every step produce a typed `value` validated against a declared schema when requested.
- Keep raw process and agent artifacts available without making them the main data model.
- Support repeated steps without ambiguous references.
- Make run history explicit through `path[]`, while also providing ergonomic indexed refs.
- Use Starlark as the default expression language.
- Allow expression engines to be plugged in by name, including JavaScript when needed.
- Support resumable runs from durable state on disk.
- Support subflows so common loops do not require external shell drivers.
- Support executor plugins so domain helpers such as roadmap parsing or git commits do not require ad hoc wrapper scripts.

## Non-goals

- Becoming a general-purpose distributed workflow platform.
- Making JavaScript the mandatory expression language.
- Embedding large domain-specific features directly into the core runner.
- Guaranteeing deterministic agent behavior.
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
- Correctness and refactor passes duplicate the same queue-processing pattern in `dagu/implement-roadmap/scripts/fix-queue.sh`.
- Markdown roadmap parsing is implemented as a reusable bash function with regex matching in `dagu/implement-roadmap/scripts/common.sh`.
- Queue persistence is ad hoc JSON under `.amata/queues`, written by `dagu/implement-roadmap/scripts/write-queue.sh`.
- Agent execution is shell-wrapped in `dagu/implement-roadmap/scripts/run-codex-prompt.sh`.
- The current repo already contains a smoke test for the Dagu workflow because correctness depends on behavior that the orchestrator itself does not enforce: `dagu/implement-roadmap/test/smoke.sh`.
- The draft in [yaml.md](../../yaml.md) already points toward a structured engine with typed responses, control flow, and resume support, but it still needs a tighter runtime contract.

## Target Contract or Target Architecture

### 1. Runtime Model

The engine executes a normalized workflow spec and persists an append-only event log for each run under a stable run directory.

Required runtime invariants:
- Every logical step execution gets a unique execution reference: `<step-id>#<ordinal>`.
- Every execution produces a structured result object, even on failure.
- Step outputs are stored as typed `value` plus raw artifacts.
- Resume reconstructs state from persisted events, not from recomputing shell helpers.
- Workflow operational state is kept outside the target repository tree by default, even when the workflow runs inside a repo.

Suggested run-state layout:
- `.amata/runs/<run-id>/spec.yaml`
- `.amata/runs/<run-id>/events.ndjson`
- `.amata/runs/<run-id>/snapshot.json`
- `.amata/runs/<run-id>/artifacts/...`

### 2. CLI

Minimum CLI surface:

```text
amata run <spec.yaml> [--set key=value ...] [--run-id <id>]
amata resume <run-id>
```

Optional but useful:

```text
amata show <run-id> [--json]
```

Contract:
- `run` copies the resolved spec into the run directory and records a spec digest.
- `resume` refuses to continue if the stored spec digest and current requested spec differ, unless an explicit override is given.
- Step retries and loop progress must survive process interruption.

### 3. Execution Context

`path[]` is the authoritative execution history. It records every completed or failed step execution in order.

Each `path[]` entry contains at least:
- `ref`
- `step_id`
- `flow`
- `status`
- `attempt`
- `started_at`
- `finished_at`
- `inputs`
- `value`
- `error`
- `artifacts`

To avoid ambiguity for repeated steps, the engine also exposes indexed views:
- `ctx.path`
- `ctx.steps.<id>.all`
- `ctx.steps.<id>.last`
- `ctx.steps.<id>.count`

Loop and call scopes add stable relative refs:
- `ctx.this`
  - current loop item or current subflow input
- `ctx.prev`
  - previous sibling iteration result in the current repeated scope, if any
- `ctx.next`
  - next input item in the current repeated scope, if any
- `ctx.inputs`
  - subflow inputs when evaluating inside a called flow

Rules:
- `ctx.path` is the source of truth for global history.
- `ctx.steps.<id>.last` is a convenience index, not an alternative data model.
- `ctx.next` never points to a future execution result. It only exposes the next input item in collection-aware scopes.
- Expressions must not depend on undeclared external state.

### 4. Expressions

Default expression language: Starlark.

Expression engine contract:
- The engine receives an immutable JSON-like `ctx`.
- The engine returns a JSON-serializable value.
- Expressions perform computation only; they do not perform IO.

Shorthand form:

```yaml
when:
  expr: ctx.steps.scan.last.value.open_count > 0
```

Explicit engine selection:

```yaml
when:
  lang: js
  expr: |
    return ctx.steps.scan.last.value.open_count > 0;
```

Rules:
- Starlark is built in and must be available in every run.
- Other engines are registered by plugin name.
- Template interpolation uses the same expression registry.

### 5. Step Result Contract

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

### 6. YAML Building Blocks

#### Top-level spec

```yaml
version: amata/v1
name: <workflow-name>
entry: <flow-name>
params: {}
defaults: {}
schemas: {}
flows: {}
```

#### Flow

```yaml
flows:
  main:
    inputs: {}
    steps: []
```

#### Common step fields

```yaml
- id: <unique-within-flow>
  type: <built-in-or-plugin-type>
  when: <expression-or-null>
  timeout: 10m
  retry:
    max_attempts: 3
    backoff: 5s
  on_error:
    action: stop | continue | call
    flow: <flow-name-when-action-is-call>
  response:
    from: value | stdout | stderr | artifact:<name>
    schema: <json-schema-object-or-ref>
```

Sequential order is implicit in the `steps:` list. Non-linear routing comes from explicit control blocks, not a generic `then` pointer on every leaf step.

#### Built-in step types

- `shell`
  - Runs a command.
  - Can capture `stdout`, `stderr`, exit code, and parsed `value`.
  - Can optionally run under a container image.

- `agent`
  - Provider-neutral agent invocation.
  - Required fields: `provider`, `prompt`, `model`.
  - Optional fields: `reasoning`, `cwd`, `env`.
  - Aliases such as `codex` and `claude` are allowed as sugar over `agent`.

- `expr`
  - Evaluates an expression and returns its value.

- `assert`
  - Fails the run when an expression is false.

- `foreach`
  - Iterates over an input collection.
  - Supports `order: seq | parallel`.
  - Child steps execute with `ctx.this`, `ctx.prev`, and `ctx.next`.
  - Returns an array of child flow results.

- `switch`
  - Evaluates cases in order and executes the first matching branch.

- `call`
  - Invokes a named subflow with explicit inputs.

- `parallel`
  - Runs named child branches concurrently and returns their collected results.

#### Plugin step types

Any non-built-in `type` is resolved through the executor registry. Example categories:
- `roadmap.items`
- `roadmap.mark_done`
- `git.commit`

The plugin contract is:
- receive validated step config
- receive `ctx`
- return a standard step result object

The example bundle in [example/README.md](example/README.md) includes a concrete, non-normative plugin registry file at [example/plugins.yaml](example/plugins.yaml). That registry resolves plugin executables relative to the registry file so the example remains self-contained.

### 7. Templates

Fields such as prompts may be templates. Template expressions use the same engine registry as normal expressions.

Example:

```yaml
prompt: |
  Repository root: {{ ctx.params.repo_dir }}
  Selected item:
  {{ to_json(ctx.this.value) }}
```

### 8. Failure and Resume Semantics

Rules:
- A failed step records its structured error and artifacts before the run stops or enters `on_error`.
- `resume` continues from the next incomplete execution point using persisted events and loop cursors.
- Completed step results are not recomputed during resume unless a policy explicitly says they are volatile.
- A repeated step keeps its historical refs on resume; ordinals do not get renumbered.

### 9. Minimal Standard Plugin Set

The core engine should stay small, but a standard local-development plugin pack is expected.

Priority plugins:
- `git.commit`
- `git.status`
- `roadmap.items`
- `roadmap.mark_done`

These are intentionally outside the core runner so that domain-specific logic does not leak into the base execution engine.

## Implementation Notes

Suggested internal boundaries:
- Spec loader and schema validator.
- Workflow planner and execution context builder.
- Event store and snapshot writer.
- Built-in executor implementations.
- Expression engine registry.
- Plugin registry for executor types.
- CLI layer for `run` and `resume`.

Important implementation notes:
- Do not model downstream state as shell-expanded environment variables.
- Do not require every executor to serialize intermediate values to temp files.
- Keep execution records immutable after they are appended.
- Make template rendering and expression evaluation use the same type system.
- Treat plugin executors and built-in executors uniformly at the runtime boundary.

## Milestones

### Milestone 1: Core runner and durable state

Scope:
- Spec parser.
- Flow and step model.
- Event log and snapshot state.
- `run` and `resume`.

Expected results:
- Simple sequential flows execute and resume correctly.

Testable outcome:
- A workflow with `shell`, `expr`, and `assert` can be interrupted and resumed without rerunning completed steps.

### Milestone 2: Expressions and control blocks

Scope:
- Starlark engine.
- `foreach`, `switch`, `call`, and `parallel`.
- `ctx.path`, `ctx.steps.<id>.all`, `ctx.steps.<id>.last`, `ctx.prev`, `ctx.next`.

Expected results:
- Repeated steps become unambiguous without shell-managed loop state.

Testable outcome:
- A workflow with nested loops can reference prior iterations and global history through typed refs only.

### Milestone 3: Agent and plugin execution

Scope:
- `agent` executor with provider adapters.
- Plugin registry.
- Standard plugins for git, docs, and roadmap helpers.

Expected results:
- The `implement-roadmap` example can be represented mostly in YAML with no shell loop scripts.

Testable outcome:
- A smoke workflow equivalent to `implement-roadmap` executes with built-in control flow and plugin leaf steps only.

## Acceptance Criteria

- The engine can express the reference `implement-roadmap` flow without external shell drivers for iteration, queue management, or routing.
- Step outputs are consumed through typed `value`, not by reparsing raw stdout in downstream logic.
- Resume works after interruption in the middle of a `foreach` loop.
- Repeated step references are unambiguous through `ctx.path` and `ctx.steps.<id>.all/last`.
- Starlark is the default expression engine.
- At least one non-Starlark expression engine can be registered without changing the core workflow format.
- Plugin step types can participate in the same validation, persistence, and resume model as built-in step types.

## Risks

- The engine can grow into an overgeneralized orchestration platform if the core surface is not kept small.
- Plugin APIs can become unstable if step configs are not validated strictly.
- Starlark may need helper functions for ergonomic JSON-like access and template rendering.
- Agent outputs remain nondeterministic; strict schema validation reduces but does not remove this risk.
- Resume semantics become fragile if executors perform hidden side effects without reporting them.

## References

- Draft requirements: [yaml.md](../../yaml.md)
- Self-contained reference bundle: [example/README.md](example/README.md)
- Current Dagu workflow overview: [README.md](../../dagu/implement-roadmap/README.md)
- Current orchestration baseline:
  - `dagu/implement-roadmap/implement-roadmap.yaml`
  - `dagu/implement-roadmap/scripts/common.sh`
  - `dagu/implement-roadmap/scripts/implement-open-items-loop.sh`
  - `dagu/implement-roadmap/scripts/fix-queue.sh`
  - `dagu/implement-roadmap/scripts/run-codex-prompt.sh`
  - `dagu/implement-roadmap/scripts/commit-if-changed.sh`
  - `dagu/implement-roadmap/test/smoke.sh`
