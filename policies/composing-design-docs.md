# Composing Design Documents (DD) Policy

You **MUST** follow these instructions.

## When a DD is required

Write a DD when at least one of these is true:
- The change is expected to affect many files.
- The change is expected to cross several modules or repositories.
- The problem took a long time to understand.
- Proven patterns are unclear.
- Success is hard to measure without an explicit contract.

By default, store the result at docs/design/{scope}.md.

## What a DD must contain

A DD is an implementation-oriented architecture document. It must define the contract clearly enough that implementation and review can be checked against it.
Required sections:

1. Summary
  - Plain statement of the problem and expected outcome.
2. Scope
  - Precise, finite scope.
  - State what is in scope and what is not.
3. Why This Is Needed
  - Explain the concrete failure, limitation, or architectural pressure.
4. Goals
  - List the intended properties the design must achieve.
5. Non-goals
  - List adjacent work that is explicitly excluded.
6. Current Baseline (Observed)
  - Describe current codebase behavior as observed in code.
  - Use concrete file references.
7. Target Contract or Target Architecture
  - Define the intended invariants, model, ownership, and boundaries.
  - Prefer explicit rules over narrative prose.
8. Implementation Notes
  - Cover the most important changes required.
  - Include the key modules, boundaries, and data-flow changes.
9. Milestones
  - Use milestones only when decomposition is useful.
  - Each milestone must have:
    - Scope
    - Expected Results
    - testable outcome
10. Acceptance Criteria
  - State how correctness will be judged at the end.
11. Risks
  - List concrete technical risks or likely failure modes.
12. References
  - Cross-link related docs, roadmap items, research, and code.

## DD quality bar

A DD must be:
- Grounded in the current codebase, not only in existing docs.
- Specific enough to refute wrong implementations.
- Narrow enough to stay finite.
- Explicit about authority, invariants, ownership, and failure behavior.
- Written for implementation, not for brainstorming.

A DD must not:
- Substitute “research options” for an actual design choice.
- Use fake precision where decisions are still open.
- Add milestones when the change is too small to justify them.
- Omit acceptance criteria for implementation-ready work.

## Document types

Not every file in docs/design/ is the same type. Use the right shape for the document.

- Implementation DD
  - Default type.
  - Must follow the full structure above.
- High-level decision doc
  - Allowed when the goal is to lock a direction before detailed design.
  - Must clearly say it is not implementation-ready.
  - Must include required follow-up design work.
- Product/spec doc
  - Allowed for UI behavior or shortcut/spec inventories.
  - Should not pretend to be an architecture DD.
  - Must still state scope, status, and references.

## Process

1. Read relevant docs/docs/**, docs/roadmap/**, and existing design docs.
2. Inspect the actual code paths involved.
3. If the design depends on external libraries, standards, or unfamiliar domains, add focused research in docs/research/.
4. Use subagents when they reduce context bloat or parallelize bounded investigations.
5. Treat subagent output as input material, not as final truth.
6. Compose the DD from verified codebase facts and resolved design decisions.
7. If docs disagree with code, align the docs.

## Subagent use

Subagents are optional tools, not a required authoring ceremony.
Use them for:
- bounded codebase investigations
- parallel assumption checks
- external research summaries
- drafting when the structure is already clear

Do not outsource the core design judgment. The final DD owner must verify:
- current baseline against code
- architectural claims
- cross-references
- milestone sanity

## Relationship to roadmap

A DD defines the target design. A roadmap defines implementation order.
If implementation will happen in phases:
- put architecture and invariants in docs/design/
- put execution order and step-by-step rollout in docs/roadmap/
