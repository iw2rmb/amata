# Task Template Overview

Select the template that matches the type of task spec you are drafting. Each structure addresses the required fields: why the task exists, how it should work, concrete changes, definition of done, tests, status tracking, and the dependency metadata expected by `AGENTS.md`.

## Work Hierarchy & Naming
- Reference the owning initiative (`<initiative>`) and feature (`<initiative>-<capability>`) in the metadata block.
- Name the task file using `<sequence>-<focus>.md` and surface the fully-qualified identifier `<initiative>-<capability>-<sequence>` near the top so dependencies stay readable.
- Keep Blocked by/Unblocks lists in sync with the parent design doc and the task index.

## Parallelisation Expectations
- Ensure every task is the smallest testable slice; if you cannot list definitive verification steps, break the task down further or create an enabling task first.
- Record shared infrastructure, fixtures, or code paths in the **Parallelisation Notes** section so neighbouring tasks can reuse them.
- Document what the task unblocks next and whether it should land as dedicated enabling work before other stories proceed.
- Update dependency metadata whenever the task changes state so the task index reflects parallel-ready capacity accurately.

## Complexity Planning (COSMIC)
- Size every new task with the COSMIC method described in `../../COSMIC.md` as you draft the spec, keeping the assumptions alongside the estimate.
- When the planned COSMIC function points (CFP) exceed 4, split the work into smaller, testable tasks whenever feasible so each slice has its own verification path.
- Populate the **Planned Complexity (COSMIC)** table during drafting and add a **Factual Complexity** update after implementation lands so estimates and actuals stay paired in the task history.

## Available Templates

1. `implementation-task.md`
   - **When to use**: Code-focused slices like `@iw2rmb/grid/docs/tasks/membership/01-protocol-foundation.md` or `@iw2rmb/ploy/docs/tasks/mods/01-planner-skeleton.md`.
   - **Highlights**: Explicit change list, test coverage requirements, dependency matrices, parallelisation notes, and cues to record verification evidence plus downstream docs impact in `docs/CHANGELOG.md`.

2. `validation-task.md`
   - **When to use**: Operational drills and lab validations similar to `@iw2rmb/grid/docs/tasks/deploy/06-vps-lab-validation.md`.
   - **Highlights**: Environment prerequisites, execution steps, artefacts to collect, dependencies, parallel execution considerations (e.g., lab slot contention), and an entry in `docs/CHANGELOG.md` for repeatable runs.

3. `documentation-task.md`
   - **When to use**: Communication deliverables such as `@iw2rmb/grid/docs/tasks/rolling-upgrade/06-changelog.md`.
   - **Highlights**: Audience mapping, affected pages, review steps, dependency callouts (other docs/features), unblocking notes, and follow-up enablement checklist.

## Using the Templates

- Reference the parent design doc at the top of the task file and keep the status checkbox synced with the design doc scoreboard.
- Enumerate the change list and tests you will add; if implementation work is required, ensure the prerequisite failing test exists before starting execution.
- Populate the Blocked by/Unblocks lists and **Parallelisation Notes** before marking a task "Ready" so other contributors can pick it up confidently.
- Adjust the template structure when it improves clarity or completeness, noting the deviation so reviewers understand the intent.
- Log verification evidence (date and inspected files) in `docs/CHANGELOG.md` before marking the checkbox complete so the task entry stays aligned with the expectations in `AGENTS.md`.
- If no template captures the work, pause and propose a new task template in `templates/tasks/` before continuing.
