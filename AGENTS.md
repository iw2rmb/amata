# How to handle documentation: 

## Work Units, Naming, and Dependencies
- Treat **features** as capabilities/epics. Document them under `docs/design/<feature>/README.md` and reference every task they decompose into.
- Treat **tasks** as work packages/user stories. Keep them in `docs/tasks/<feature>/<n>-<stage>.md` with sequence numbers reflecting execution order.
- Adopt the identifier format `<initiative>-<capability>-<sequence>` across features and tasks so dependency references stay unambiguous.
- Every feature and task must expose both **Blocked by** and **Unblocks** lists (relative links only) and keep them current after each edit.
- Mirror dependency metadata across artefacts: if a feature says it is blocked by Task A, Task A must list that feature in its Unblocks list.

## Workflow and Parallelisation Guardrails
- The high-level workflow remains: Design Docs → Task Specs → Tests; use this flow to stage work so parallel development never outruns design intent.
- When any new feature request arrives—including seemingly small issue fixes, behaviour adjustments, or additive enhancements—confirm whether relevant design docs already exist; if they conflict with the request, stop and obtain explicit user approval before changing them. When the request fits an existing design, extend the doc accordingly, add any required follow-on tasks so implementation covers the new inputs, and move the design doc back to Planned status.
- Draft design docs with the templates catalogued in `/Users/vk/@iw2rmb/docs/templates/design/README.md`; the templates define required metadata, dependency mirroring, and the parallelisation snapshot.
- Break designs into task specs using the structures in `/Users/vk/@iw2rmb/docs/templates/tasks/README.md` so blockers, definition of done, tests, and parallelisation notes stay consistent.
- After every task update, re-open and regenerate `docs/tasks/README.md` using template in `/Users/vk/@iw2rmb/docs/templates/tasks/INDEX.md` so it lists all open tasks ordered by blocking dependencies (unblocked work first, most constrained items last). Never rely on a cached editor buffer when refreshing this queue.
- Lean on the template parallelisation checklist to spot shared fixtures or enabling work before implementation begins.
- Implementation only begins after the corresponding failing tests or snapshots are committed. Keep PRs scoped to a single task wherever possible so reviewers can validate dependency fields quickly.
- Maintain `docs/design/README.md` as an index of all design documents with one-line summaries, status checkboxes, and dependency highlights; update it every time any design doc changes.
- When new information surfaces, update the design doc and tasks immediately, documenting the verification steps and noting which artefacts were reviewed so downstream teams know the new ground truth.
- As soon as work completes, mark the corresponding design and task entries finished (checkboxes, status sections, timestamps) and confirm the Blocked by/Unblocks lists still make sense before starting fresh implementation.
- Ask clarifying questions whenever requirements or constraints are uncertain, including ambiguity in dependency or sequencing expectations.
- Any new behaviour must appear in `CHANGELOG.md` with concrete dates (YYYY-MM-DD).
- Every new or updated design document must list the exact upstream docs, specs, or code packages it depends on—use explicit relative links so the dependency chain remains traceable end-to-end.
- Before landing any documentation change, verify the current implementation or tests reflect the described behaviour (or note the gap as a follow-up), then record the verification date, evidence, and files reviewed in CHANGELOG.md.

## Complexity Estimation (COSMIC)
- While decomposing design docs into tasks, size every prospective task using the COSMIC checklist in `/Users/vk/@iw2rmb/docs/COSMIC.md` before the task spec is finalised.
- If the planned COSMIC function points (CFP) for a task exceed 4, split the work into smaller, testable tasks wherever feasible so each slice remains independently verifiable.
- Capture the planned COSMIC sizing inside the task spec when drafting it, and record the factual (post-implementation) sizing once the code lands so the task documents both the estimate and actual complexity.

## Parallel Work Coordination
- Use `docs/tasks/README.md` as the single dependency-ordered reservation surface.
- Step 0: Immediately before editing, re-open `docs/tasks/README.md` so you are working from the latest state—never depend on a cached copy.
- Step 1: Review the queue top-down; entries written as `- [ ] path` are planned and unclaimed, while lines starts with `- [x]` are already reserved.
- Step 2.1: If the task you want already starts with `- [x]`, leave the file unchanged and choose another unblocked task.
- Step 2.2: If the task is ready and unclaimed, update its line by changing to `- [x]` so other agents see it is reserved.
- Step 3: Once the task hands off or completes, remove its line entirely so downstream work can proceed.

# How to code
- Every function must start with a one-liner comment what/why this function performs
- If source code file edited exceeds 500 lines split it in logical modules in different files
- For golang source code follow `/Users/vk/@iw2rmb/docs/GOLANG.md` instructions
- Run and update the tests referenced in tasks before declaring work complete, and document results in `CHANGELOG.md`.
