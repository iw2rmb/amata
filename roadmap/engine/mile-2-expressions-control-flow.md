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

- [ ] 2.3 Implement `switch`, `call`, and recursive flow frames
  - Repository: auto
  - Component: `internal/runtime`, flow planner, call-stack model
  - Verification: `switch` picks the first matching branch, recursive `call` returns through one flow stack, `ctx.prev` carries all upstream data needed by later steps
  - Reasoning: high
  1. Add named-flow lookup and flow-frame push/pop handling for `call`.
  2. Implement `switch` case evaluation in order with first-match execution and structured branch results.
  3. Keep `ctx.prev` scoped to the current flow frame and require prior steps to carry forward any older data explicitly.
  4. Treat step `id` as diagnostics-only metadata and block any runtime data access path that depends on `id`.

- [ ] 2.4 Add tests for branching, recursive flows, templates, and validation failures
  - Repository: auto
  - Component: evaluator tests, planner tests, integration workflow tests
  - Verification: recursive subflows run with `ctx.prev` only, `expect` fails the current step, invalid schemas and missing refs stop the run cleanly
  - Reasoning: medium
  1. Add table-driven evaluator tests for shorthand expressions, template interpolation, and literal escaping.
  2. Add flow tests for `switch` branching and recursive `call` with `ctx.prev`-only data passing.
  3. Add negative tests for invalid schema refs, schema validation errors, and failed `expect` expressions.
  4. Add an integration workflow that mirrors the example loop shape without agent executors and proves step-id lookups are unnecessary.
