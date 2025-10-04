# Design Template Overview

Use these templates to keep design documentation consistent across teams. Follow the documentation workflow in `AGENTS.md` so the template you pick stays aligned with the Design Docs → Roadmap Specs → Tests cadence.

## Work Hierarchy & Naming
- **Plan (Programme/Initiative)** → coordinates features; reference it from the metadata block using the `<initiative>` identifier.
- **Feature (Capability/Epic)** → represented by the design doc itself; list the `<initiative>-<capability>` identifier alongside status, owners, and roadmap links.
- **Task (Work Package/Story)** → individual roadmap entries; reference them using `<initiative>-<capability>-<sequence>` so cross-doc dependency lists remain unambiguous.
- Every template now includes `Blocked by` and `Unblocks` fields. Keep the two lists mutually consistent across plans, designs, and tasks.

## Parallelisation Checklist
- Capture a **Parallelisation Snapshot** section summarising active features, their ready-to-start tasks, and any shared assets that must exist first.
- Record the dependency graph (critical path, slack, and handoffs) so contributors can spot independent tracks quickly.
- Add a **Shared Components & Unblocking Candidates** list that highlights fixtures, libraries, or scripts worth delivering up front to unlock downstream work.
- Ensure each slice is small and testable, reflecting industry guidance on work breakdown structures and dependency management—design docs should make it obvious when multiple teams can run in parallel.

## Available Templates

1. `component-design-spec.md`
   - **When to use**: New service/component capabilities or substantial feature expansions. Mirrors documents like `@iw2rmb/grid/docs/design/membership/README.md` and `@iw2rmb/ploy/docs/design/ipfs-artifacts/README.md` where the goal is to spell out intent, architecture, interfaces, and rollout.
   - **Highlights**: Expanded metadata with dependency fields, detailed architecture and interface sections, risk and observability callouts, parallelisation snapshot, shared component inventory, and an entry in `docs/CHANGELOG.md` so evidence stays attached to the doc.

2. `integration-alignment-spec.md`
   - **When to use**: Cross-repo contract alignments (e.g., `@iw2rmb/ploy/docs/design/workflow-rpc-alignment/README.md`). Focus on harmonising payloads, subjects, CLI flows, and migration tactics between systems.
   - **Highlights**: Participating systems table, dependency and contract matrix, compatibility/migration plan, shared assets to build ahead of time, and shared decision log to capture trade-offs.

3. `resiliency-hardening-plan.md`
   - **When to use**: Reliability programmes that cut across multiple components, similar to `@iw2rmb/grid/docs/design/ha/README.md` or `@iw2rmb/grid/docs/design/jobs-reliability/README.md`.
   - **Highlights**: Failure-mode inventory, mitigation matrix, telemetry requirements, dependency visualisation (critical path vs. slack), lab drill expectations, shared tooling backlog, and evidence log for ongoing verification.

4. `program-epic-blueprint.md`
   - **When to use**: Multi-stage initiatives with dedicated roadmap folders, such as `@iw2rmb/grid/docs/design/rolling-upgrade/README.md` or `@iw2rmb/ploy/docs/design/shift/README.md`.
   - **Highlights**: Stage scoreboard, workstream breakdowns, dependency board, risk register, success metrics, rollout/adoption planning sections, and explicit unblocking work queues.

## How to Use These Templates

- Start from the template that matches your milestone scope, then tailor sections rather than deleting required headings. Decompose work into focused docs so each design tackles a single coherent slice; note any follow-on work separately.
- Populate the metadata block with the owning plan, roadmap tasks, verification evidence, and up-to-date Blocked by/Unblocks lists.
- Fill out the **Parallelisation Snapshot** early so contributors can see what starts immediately and what depends on enabling work.
- Link roadmap tasks with relative paths and call out upstream dependencies in the metadata block so the chain stays traceable.
- After drafting or revising a design doc, update `docs/design/README.md` with the summary, status checkbox, dependency highlights, and a dated verification note referencing the files you inspected.
- Capture supporting scripts, fixtures, or helper commands inside the design doc before implementation begins so roadmap tasks can reference them, and flag which items belong in an "unblocking" plan.
- If none of the existing templates fit the work, pause and propose a new template in `templates/design/` before drafting.
