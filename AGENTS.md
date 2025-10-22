# Communication

- **Communicate in terse, direct, explicit terms. No fluff, analogies, or vague wording.**

# How to handle documentation: 

## Work Units and Dependencies
- Design docs are the only planning artefact. Author each one at `docs/design/<subject>/README.md` and treat it as the unit that is delivered and verified.
- Keep the doc short and focused; if the scope grows, split into additional design docs rather than reviving tasks.
- Each design doc must be listed in the shared queue (`docs/design/QUEUE.md`) using checkbox entries ordered from ready-to-pull to most constrained work.

## Workflow
- Before drafting or revising any artefact, actively use web search to capture the latest library/component versions, code snippets, documentation, and current best practices needed for the work.
- The workflow is now: Design Doc → Tests → Implementation. Keep docs authoritative so implementation always trails a reviewed plan.
- When a new request arrives, check for an existing design doc. If alignment is needed, revise the doc and its queue; never resurrect task specs.
- Draft every design doc using template from `/Users/vk/@iw2rmb/docs/design/TEMPLATE.md`.
- Maintain `docs/design/QUEUE.md` as the shared pull list for open design docs. Re-open it before editing so you never operate on a stale queue.
- Maintain `docs/design/README.md` as an index of all design documents with one-line summaries, status checkboxes, and dependency highlights; update it every time any design doc changes.
- As soon as work completes, mark the design doc finished (status, timestamps), confirm its queue entries are cleared, then move the entire `docs/design/<subject>` folder into the root `.archive/` directory.
- Ask clarifying questions whenever requirements or constraints are uncertain, including ambiguity in dependency or sequencing expectations.
- Every new or updated design document must list the exact upstream docs, specs, or code packages it depends on—use explicit relative links so the dependency chain remains traceable end-to-end.

## Scope Sizing (COSMIC)
- When a design doc feels broad, run the COSMIC sizing checklist in `/Users/vk/@iw2rmb/docs/COSMIC.md` to decide whether to split the work into multiple design docs.
- Keep the COSMIC assumptions with the design doc so future updates can reuse the estimate or refine it.

## Work Coordination
- Use `docs/design/QUEUE.md` as the single dependency-ordered reservation surface.
- Step 0: Immediately before editing, re-open `docs/design/QUEUE.md` so you are working from the latest state—never depend on a cached copy.
- Step 1: Review the queue top-down; entries written as `- [ ] path` are planned and unclaimed, while lines starting with `- [x]` are already reserved.
- Step 2.1: If the design doc you want already starts with `- [x]`, leave the file unchanged and choose another unblocked doc.
- Step 2.2: If the design doc is ready and unclaimed, update its line by changing to `- [x]` so other agents see it is reserved.
- Step 3: Once the doc hands off or completes, remove its line entirely so downstream work can proceed.

# How to code
- Every function must start with a one-liner comment what/why this function performs
- For golang source code follow `/Users/vk/@iw2rmb/docs/GOLANG.md` instructions
- Run and update the tests referenced in the active design doc before declaring work complete, and document results in `CHANGELOG.md`.
