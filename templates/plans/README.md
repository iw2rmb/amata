# Plan Template Overview

Use these plan templates to coordinate execution workflows that accompany design decisions. Follow the documentation flow in `AGENTS.md` so plans stay aligned with Design Docs → Roadmap Specs → Tests and reference the right design and task artefacts.

## Work Hierarchy & Naming
- Plans model programmes/initiatives; use `<initiative>` as the identifier and mirror it in every linked design and roadmap entry.
- Plans must enumerate the features (`<initiative>-<capability>`) they orchestrate and surface the tasks (`<initiative>-<capability>-<sequence>`) ready to start.
- Each plan now includes dependency boards capturing Blocked by/Unblocks relationships; keep them consistent with the corresponding design and task docs.

## Parallel Execution Expectations
- Maintain a **Feature Scoreboard** table with status, owner, target dates, and critical blockers so stakeholders know which tracks can start in parallel.
- Capture a **Dependency Map** section outlining critical path vs. slack items, handoffs, and shared resources (e.g., environments, reviewers) that could throttle throughput.
- Add an **Unblocking Backlog** detailing the enabling work that should land before dependent tasks (these may become separate "unblocking" plans when scope expands).
- Record decision gates, verification evidence, and risk mitigations alongside the dependency data so approvals align with the plan’s critical path.

## Available Templates

1. `service-deprecation-plan.md`
   - **When to use**: Retiring or sunsetting a service, API, or feature where migration, comms, and rollback need orchestration.
   - **Highlights**: Timeline checkpoints, migration support, stakeholder communications, risk tracking, dependency map, feature scoreboard, and verification hook-ins.

2. `launch-readiness-plan.md`
   - **When to use**: Cross-functional go/no-go planning for major releases once implementation is complete.
   - **Highlights**: Launch gates, feature readiness scoreboard, owner roster, dependency and capacity overview, enablement checklist, contingency scenarios, and evidence capture.

3. `security-hardening-playbook.md`
   - **When to use**: Programmes focused on strengthening security controls, patch waves, or compliance deliverables.
   - **Highlights**: Threat model updates, control rollout matrix, dependency visualisation, shared tooling backlog, validation cadence, and remediation tracking.

## How to Use These Templates

- Start with the template closest to your programme, then adjust sections when it improves clarity or completeness—capture the deviation rationale in the doc. Keep plans scoped to small, actionable stages; split oversized efforts into follow-on plans.
- Link back to the parent design doc and roadmap tasks so the plan shows how execution lines up with committed work.
- Populate the Feature Scoreboard, Dependency Map, and Unblocking Backlog before circulating the plan so contributors can queue parallel work safely.
- Record who owns each stage, the decision gates, and the evidence required before advancing to the next phase.
- If none of the plan templates match the work, pause and propose a new template inside `templates/plans/` before drafting.
- Add dated entries to `docs/CHANGELOG.md` for the plan and any related design/task docs once you confirm the plan aligns with reality.
