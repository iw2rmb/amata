# Communication
- Speak in terse, direct sentences. Do not use fluff, analogies, or vague wording.

# Documentation Workflow

## Work Units and Dependencies
- Author design docs at `docs/design/<subject>/README.md`; they are the only planning artefact.
- Keep each doc tightly scoped. If the work expands, split it into additional design docs instead of stretching the original.
- Register every design doc in `docs/design/QUEUE.md` as a checkbox entry ordered from ready-to-pull to most constrained.

## Workflow
- Before drafting or updating any artefact, run a web search to capture current versions, examples, documentation, and current best practices for the task.
- Follow this sequence: Design Doc → Tests → Implementation. Implementation must always trail a reviewed plan.
- When a new request lands, locate the relevant design doc. If alignment is required, revise that doc and its queue entry—never resurrect stale task specs.
- Draft every design doc using `/Users/vk/@iw2rmb/docs/design/TEMPLATE.md`.
- Ask clarifying questions whenever requirements, dependencies, or sequencing are uncertain.
- In every new or updated design doc, list the precise upstream docs, specs, or packages using explicit relative links to preserve the dependency chain.

## Scope Sizing (COSMIC)
- When a design doc feels broad, run `/Users/vk/@iw2rmb/docs/COSMIC.md` to decide whether to split the scope.
- Store the COSMIC assumptions with the design doc so future updates can reuse or refine them.

## Work Coordination
- Treat `docs/design/QUEUE.md` as the single dependency-ordered reservation surface.
- Step 0 — Re-open the queue immediately before editing; never rely on a cached copy.
- Step 1 — Review the queue top-down. Lines beginning with `- [ ]` are unclaimed; lines beginning with `- [x]` are already reserved.
- Step 2.1 — If the doc you need already reads `- [x]`, leave it untouched and pick another unblocked item.
- Step 2.2 — If the doc is available, flip its checkbox to `- [x]` so other agents see it is reserved.
- Step 3 — After hand-off or completion, mark the design doc finished with status and timestamps, clear its queue entries, then move `docs/design/<subject>` into `.archive/`.

# Coding Rules
- Begin every function with a one-line comment that states what the function does or why it exists.
- Follow `/Users/vk/@iw2rmb/docs/GOLANG.md` for Go code conventions.
