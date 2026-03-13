# Engine Milestone 2 Expressions and First Control Blocks

Scope: Add the shared expression system, validated step value plumbing, and first-version control blocks so workflows branch and recurse through `ctx.prev` without step-id lookups.

Documentation: [Engine design](../../design/engine/engine.md)

Legend: [ ] todo, [x] done.

- [x] 2.1 Integrate Starlark evaluation and one runtime context shape
  - Added shared `internal/expr` Starlark evaluation over immutable JSON-like runtime input and `internal/template` rendering that preserves raw JSON-like values for whole-template expressions.
  - Unified runtime context under `ctx.workspace`, `ctx.params`, `ctx.prev`, plus current-step `ctx.status`, `ctx.value`, `ctx.error`, and `ctx.artifacts` bindings for `expect`.
  - Reused the same resolver across `expr`, `when`, `expect`, `assert`, and templated shell scalar fields, including whole-scalar `$.` shorthand and `$$` escaping coverage.

- [x] 2.2 Implement response resolution and schema validation
  - Added `internal/runtime/response` so succeeded steps resolve `value` from executor-native output, `stdout`, `stderr`, or `artifact:<name>` before `expect` and downstream `ctx.prev.value` use.
  - Added `internal/schema` with workflow-local schema compilation, shorthand normalization, and `#/schemas/...` `$ref` rewriting for response validation.
  - Failed response schema compilation now stops the step with `invalid_response_schema`, while structural mismatches stop it with `response_schema_mismatch`.
  - Kept raw process output in artifacts and added runner coverage that downstream steps consume validated `value` instead of raw artifact paths.

- [x] 2.3 Implement `switch`, `call`, and recursive flow frames
  - Added a deterministic flow planner plus persisted frame-push and atomic control-return events so `call` and `switch` execute through one resumable flow stack, including recursive subflow returns.
  - `switch` now evaluates cases in order, runs only the first matching branch, and records a structured result that carries branch metadata and the nested step output forward.
  - New flow frames start with the caller's current `ctx.prev`, then update only within that frame so recursive flows can receive upstream data without restoring step-id lookup semantics.
  - Removed `id` from runtime step-result bindings and made `id` lookups fail in the expression object so step ids stay diagnostics-only.

- [ ] 2.4 Add tests for branching, recursive flows, templates, and validation failures
  - Repository: auto
  - Component: evaluator tests, planner tests, integration workflow tests
  - Verification: recursive subflows run with `ctx.prev` only, `expect` fails the current step, invalid schemas and missing refs stop the run cleanly
  - Reasoning: medium
  1. Add table-driven evaluator tests for shorthand expressions, template interpolation, and literal escaping.
  2. Add flow tests for `switch` branching and recursive `call` with `ctx.prev`-only data passing.
  3. Add negative tests for invalid schema refs, schema validation errors, and failed `expect` expressions.
  4. Add an integration workflow that mirrors the example loop shape without agent executors and proves step-id lookups are unnecessary.
