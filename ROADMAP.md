> Rules how to follow this template:
> - Apply [COSMIC](~/.codex/policies/estimating-effort.md) for decomposition granularity at the functional-process level. Use it to size and split work; do not copy COSMIC theory text into the subject roadmap.
> - Use COSMIC-style granularity: one numbered implementation line equals one functional action with one observable outcome.
> - Classify each roadmap item as either `determined` or `assumption-bound`.
> - For `determined` items, name concrete repositories/components and concrete classes/functions/structs touched by each functional action.
> - Keep all functional actions inside one item at the same detail level.
> - For `assumption-bound` items, add an `Assumptions:` block with current best references and unresolved unknowns.
> - For `assumption-bound` items, shift `Reasoning` one level to the right: low->medium, medium->high, high->xhigh, xhigh->xhigh.
> - Keep wording plain, precise, short, and direct.
> - Omit this `Rules...` block in the resulting roadmap.

# <Title of the feature>

Scope: <One short statement of boundary and intended outcome.>

Documentation: <Relevant docs. Use design docs or roadmap docs for this subject, not both in the same item set.>

- [ ] <n.n Item short title-summary>
  - Repository: <repo name or `auto`>
  - Component: <Concrete modules/files/classes/functions/structs>
  - Assumptions: <Only for assumption-bound items; omit when none>
  - Implementation:
    1. <One functional action at class/function/struct level>
    2. <One functional action at class/function/struct level>
    3. ...
  - Verification:
    1. <One-liner test scenario 1>
    2. <One-liner test scenario 2>
    3. ...
  - Reasoning: <Estimated implementation effort: low|medium|high|xhigh>

- [ ] <n.n Next item short title-summary>
...
