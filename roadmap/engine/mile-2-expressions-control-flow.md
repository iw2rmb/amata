# Engine Milestone 2 Expressions and First Control Blocks

Scope: Add the shared expression system, validated step value plumbing, and first-version control blocks so workflows branch and recurse through `ctx.prev` without step-id lookups.

Documentation: [Engine design](../../design/engine/engine.md)

Legend: [ ] todo, [x] done.

- [ ] 2.1 Integrate Starlark evaluation and one runtime context shape
  - Repository: auto
  - Component: `internal/expr`, `internal/template`, runtime context builder
  - Verification: `$.` shorthand resolves through `ctx`, `$$` escapes literal scalars, templates and expressions return the same JSON-like types
  - Reasoning: high
  1. Add a Starlark evaluator that accepts immutable JSON-like runtime input and returns JSON-serializable values.
  2. Build one context shape that exposes `ctx.workspace`, `ctx.params`, `ctx.prev`, and the current step bindings without any step-id lookup API.
  3. Implement whole-scalar `$.` shorthand and `$$` escaping in literal-or-expression scalar positions.
  4. Reuse the same evaluator for `expr`, `when`, `expect`, and `{{ ... }}` template rendering.

- [ ] 2.2 Implement response resolution and schema validation
  - Repository: auto
  - Component: `internal/runtime/response`, `internal/schema`, executor result adapters
  - Verification: `response.from` selects the correct source, schema mismatches fail structurally, downstream steps consume validated `value` instead of raw stdout
  - Reasoning: xhigh
  1. Resolve step `value` from executor-native output, `stdout`, `stderr`, or a named artifact according to `response.from`.
  2. Implement local schema registry loading and `#/schemas/...` `$ref` resolution for workflow-owned schemas.
  3. Validate resolved values before publishing them downstream and surface schema failures as structured step errors.
  4. Keep raw process and agent output in artifacts while making `value` the only supported data path for downstream expressions.

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
