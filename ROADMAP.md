# <Title of the feature>

Scope: <One short statement of boundary and intended outcome.>

Documentation: <Relevant docs. Use design docs or roadmap docs for this subject, not both.>

> Method: `Users/vk/@iw2rmb/auto/COSMIC.md` <Use for functional-process granularity and decomposition. Do not mix it with subject documentation. Omit this line in the resulting roadmap.>

Legend: [ ] todo, [x] done.

> Rules:
> - Use COSMIC-style granularity: one functional action per numbered line.
> - Keep all numbered actions in one item at the same level of detail.
> - Name concrete classes, functions, and structs when useful. Do not go deeper.
> - Keep wording plain, precise, short, and direct.
> - Omit `Rules` block in the resulting roadmap.

- [ ] <n.n Item short title-summary>
  - Repository: <Which repository it affects>
  - Component: <Which components/modules/crates it affects>
  - Verification: <Comma separated one-liner test scenarios>
  - Reasoning: <Estimated reasoning level required for gpt-5.4 model: xhigh|high|medium|low>
1. <Functional action at class/function/struct level>
2. <Functional action at class/function/struct level>
3. <Functional action at class/function/struct level>

- [ ] <n.n Next item short title-summary>
...
