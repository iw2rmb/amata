# How to handle documentation: 

## Work Units, Naming, and Dependencies
- Treat **plans** as programme-level coordination artefacts (industry analogues: programme plans/initiatives). Store them in `docs/plans/<initiative>/README.md`, keep titles noun-based, and enumerate the features they coordinate.
- Treat **features** as capabilities/epics. Document them under `docs/design/<feature>/README.md` and reference the owning plan plus every roadmap task they decompose into.
- Treat **roadmap tasks** as work packages/user stories. Keep them in `roadmap/<feature>/<n>-<stage>.md` with sequence numbers reflecting execution order.
- Adopt the identifier format `<initiative>-<capability>-<sequence>` across plans, features, and tasks so dependency references stay unambiguous.
- Every plan, feature, and task must expose both **Blocked by** and **Unblocks** lists (relative links only) and keep them current after each edit.
- Mirror dependency metadata across artefacts: if a feature says it is blocked by Task A, Task A must list that feature in its Unblocks list.

## Workflow and Parallelisation Guardrails
- The high-level workflow remains: Design Docs → Roadmap Specs → Tests; use this flow to stage work so parallel development never outruns design intent.
- Major work begins with a design document under `docs/design/<feature>/README.md` describing intent, architecture, risks, test strategy, deliverables, and the dependency map covering upstream plans/features/tasks. Start from the best-fit template in `templates/design/README.md`.
- Translate the design into executable roadmap entries inside `roadmap/<feature>/<n>-<stage>.md`, one file per code file or behavioural slice. Each roadmap entry must document how it reduces blocking risk, the unblocking work it depends on, and the tasks it unlocks next.
- Plans under `docs/plans/<initiative>/README.md` orchestrate multiple features. Maintain a feature scoreboard with status, blocked-by items, and slack/ready-to-start signals so teams can launch work in parallel without re-reading the entire plan.
- After every roadmap task update, re-open and regenerate `docs/plans/README.md` so it lists all open tasks ordered by blocking dependencies (unblocked work first, most constrained items last). Never rely on a cached editor buffer when refreshing this queue.
- Keep design docs, plans, and roadmap tasks decomposed into small, testable slices. If scope grows, split follow-on work into additional docs so the backlog surfaces the next ready-to-run items.
- With every doc update, run a **parallelisation review**: inspect dependent artefacts, extract shared components/functions/fixtures that multiple tasks need, and schedule them as enabling tasks or dedicated "unblocking" plans before other work starts.
- Each roadmap task file must cover why the task is required, how it works, what changes are needed and where, definition of done, tests to perform, status with a checkbox, dependency metadata, and a short "Parallelisation Notes" section identifying co-ordination touchpoints.
- Implementation only begins after the corresponding failing tests or snapshots are committed. Keep PRs scoped to a single roadmap task wherever possible so reviewers can validate dependency fields quickly.
- Maintain `docs/design/README.md` as an index of all design documents with one-line summaries, status checkboxes, and dependency highlights; update it every time any design doc changes.
- When new information surfaces, update the design doc, plans, and roadmap tasks immediately, documenting the verification steps and noting which artefacts were reviewed so downstream teams know the new ground truth.
- If a task needs supporting scripts, fixtures, or helper commands, capture the additional context in the design document (and link it in the task) before implementation proceeds. Note whether that pre-work is required to unblock other tasks.
- As soon as work completes, mark the corresponding plan, design, and roadmap entries finished (checkboxes, status sections, timestamps) and confirm the Blocked by/Unblocks lists still make sense before starting fresh implementation.
- Ask clarifying questions whenever requirements or constraints are uncertain, including ambiguity in dependency or sequencing expectations.
- Any new behaviour must appear in `CHANGELOG.md` with concrete dates (YYYY-MM-DD).
- Every new or updated design document must list the exact upstream docs, specs, or code packages it depends on—use explicit relative links so the dependency chain remains traceable end-to-end.
- Before landing any documentation change, confirm the described behaviour matches the current implementation (or capture the delta as a follow-up) and record the verification details—date plus files inspected—inside the doc or roadmap entry.

## Parallel Work Coordination
- Use `docs/plans/README.md` as the single dependency-ordered reservation surface.
- Step 0: Immediately before editing, re-open `docs/plans/README.md` so you are working from the latest state—never depend on a cached copy.
- Step 1: Review the queue top-down; entries written as `- [ ] path` are planned and unclaimed, while lines starts with `- [x]` are already reserved.
- Step 2.1: If the task you want already starts with `- [x]`, leave the file unchanged and choose another unblocked task.
- Step 2.2: If the task is ready and unclaimed, update its line by changing to `- [x]` so other agents see it is reserved.
- Step 3: Once the task hands off or completes, remove its line entirely so downstream work can proceed.

# How to code
- Every function must start with a one-liner comment what/why this function performs
- If source code file edited exceeds 500 lines split it in logical modules in different files
- For golang source code follow `/Users/vk/.codex/GOLANG.md` instructions
- Run and update the tests referenced in roadmap tasks before declaring work complete, and document results in `docs/CHANGELOG.md`.

