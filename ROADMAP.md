> Rules how to follow this template:
> - Apply [COSMIC](~/.codex/policies/estimating-effort.md) methodology for functional-process granularity and decomposition. Do not mix it with subject documentation.
> - Use COSMIC-style granularity: one functional action per numbered line.
> - Consider per item if it's determined (it is possible to point out all components, classes, functions, and structs involved; there is no ambigouty) or there are assumptions.
> - For determined items, name or refer to concrete components, classes, functions, and structs per functional action. 
> - For determined items, keep functional actions within roadmap are at the same level of detalisation.
> - For non-determined items, provide references or names determined at a time, and shift estimated reasoning to the right: low->medium, medium->high, high->xhigh, xhigh->xhigh.
> - Keep wording plain, precise, short, and direct.
> - Omit `Rules...` block in the resulting roadmap.

# <Title of the feature>

Scope: <One short statement of boundary and intended outcome.>

Documentation: <Relevant docs. Use design docs or roadmap docs for this subject, not both.>

- [ ] <n.n Item short title-summary>
  - Implementaion:
    1. <Functional action1 at class/function/struct level>
    2. <Functional action2 at class/function/struct level>
    3. ...
  - Verification: 
    1. <One-liner test scenario 1>
    2. <One-liner test scenario 2>
    3. ...
  - Reasoning: <Estimated reasoning (effort) required for implementation: low|medium|high|xhigh>

- [ ] <n.n Next item short title-summary>
...
