# Communication
- Speak in terse, direct sentences. Do not use fluff, analogies, or vague wording.

# Documentation Workflow

## Workflow
- Follow this sequence: Design Doc → Tests → Implementation. Implementation must always trail a reviewed plan.
- Author design docs at `docs/design/<subject>/README.md` (relative to a project's root folder); they are the only planning artefact.
- Register every design doc in `docs/design/QUEUE.md` (relative to a project's root folder) as a checkbox entry ordered from ready-to-pull to most constrained.
- Write every design doc using `/Users/vk/@iw2rmb/docs/design/TEMPLATE.md`.
- If the COSMIC evaluation of a design doc exceeds 4 points, decompose and replace that design doc with series of design docs, each scoring 4 points or less.

## Work Coordination
- Treat `docs/design/QUEUE.md` (relative to a project's root folder) as the single dependency-ordered reservation surface.
- Step 0 — Re-open the queue immediately before editing; never rely on a cached copy.
- Step 1 — Review the queue top-down. Lines beginning with `- [ ]` are unclaimed; lines beginning with `- [x]` are already reserved.
- Step 2.1 — If the doc you need already reads `- [x]`, leave it untouched and pick another unblocked item.
- Step 2.2 — If the doc is available, flip its checkbox to `- [x]` so other agents see it is reserved.
- Step 3 — After hand-off or completion,
    3.1 Clear record for this design doc from queue.
    3.2 Move this design doc into `.archive/` (relative to a project's root folder).
    3.3 Review the implementation and add concise in-code comments (preferred) or doc notes (secondary) that prevent future misreads.
    3.4 Commit everything.

# Coding Rules
- Begin every function with a one-line comment that states what the function does or why it exists.
- If programming language used in project is type-safe, use custom types to harden the logic.
- Follow `/Users/vk/@iw2rmb/docs/GOLANG.md` for projects in Go.
