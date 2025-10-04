# Roadmap Task Template Overview

Select the template that matches the type of roadmap entry you are drafting. Each structure addresses the required fields: why the task exists, how it should work, concrete changes, definition of done, tests, status tracking, and the dependency metadata expected by `AGENTS.md`.

## Work Hierarchy & Naming
- Reference the owning plan (`<initiative>`) and feature (`<initiative>-<capability>`) in the metadata block.
- Name the roadmap file using `<sequence>-<focus>.md` and surface the fully-qualified identifier `<initiative>-<capability>-<sequence>` near the top so dependencies stay readable.
- Keep Blocked by/Unblocks lists in sync with the parent design doc and plan scoreboard.

## Parallelisation Expectations
- Ensure every task is the smallest testable slice; if you cannot list definitive verification steps, break the task down further or create an enabling task first.
- Record shared infrastructure, fixtures, or code paths in the **Parallelisation Notes** section so neighbouring tasks can reuse them.
- Document what the task unblocks next and whether it should land in a dedicated "unblocking" plan before other stories proceed.
- Update dependency metadata whenever the task changes state so plans can calculate parallel-ready capacity accurately.

## Available Templates

1. `implementation-task.md`
   - **When to use**: Code-focused slices like `@iw2rmb/grid/roadmap/membership/01-protocol-foundation.md` or `@iw2rmb/ploy/roadmap/mods/01-planner-skeleton.md`.
   - **Highlights**: Explicit change list, test coverage requirements, dependency matrices, parallelisation notes, and cues to record verification evidence plus downstream docs impact in `docs/CHANGELOG.md`.

2. `validation-task.md`
   - **When to use**: Operational drills and lab validations similar to `@iw2rmb/grid/roadmap/deploy/06-vps-lab-validation.md`.
   - **Highlights**: Environment prerequisites, execution steps, artefacts to collect, dependencies, parallel execution considerations (e.g., lab slot contention), and an entry in `docs/CHANGELOG.md` for repeatable runs.

3. `documentation-task.md`
   - **When to use**: Communication deliverables such as `@iw2rmb/grid/roadmap/rolling-upgrade/06-changelog.md`.
   - **Highlights**: Audience mapping, affected pages, review steps, dependency callouts (other docs/features), unblocking notes, and follow-up enablement checklist.

## Using the Templates

- Reference the parent design doc at the top of the task file and keep the status checkbox synced with the design doc scoreboard.
- Enumerate the change list and tests you will add; if implementation work is required, ensure the prerequisite failing test exists before starting execution.
- Populate the Blocked by/Unblocks lists and **Parallelisation Notes** before marking a task "Ready" so other contributors can pick it up confidently.
- Adjust the template structure when it improves clarity or completeness, noting the deviation so reviewers understand the intent.
- Log verification evidence (date and inspected files) in `docs/CHANGELOG.md` before marking the checkbox complete so the roadmap entry stays aligned with the expectations in `AGENTS.md`.
- If no template captures the work, pause and propose a new roadmap template in `templates/tasks/` before continuing.
