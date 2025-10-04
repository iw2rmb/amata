# <Area> Resiliency & Hardening Plan

- **Identifier**: `<initiative>-<capability>`
- **Owning Plan**: `[<Plan Name>](../../docs/plans/<initiative>/README.md)`
- **Status**: [ ] Draft · [ ] In progress · [ ] Completed — last updated YYYY-MM-DD
- **Linked Roadmap Tasks**:
  - [ ] `<initiative>-<capability>-<sequence>` – `../../roadmap/<area>/<sequence>-<stage>.md`
- **Blocked by**:
  - Supporting design docs or enabling tasks (relative links)
- **Unblocks**:
  - Downstream resiliency programmes/tasks (relative links)
- **Last Verification**: YYYY-MM-DD — failover/lab evidence in <notes/reports>
- **Upstream Dependencies**:
  - `../docs/design/<supporting-doc>/README.md`
  - Runbooks, incident reviews, or lab guides (relative links)

## Purpose
Summarise the resiliency problem, customer impact, and SLA/SLO targets.

## Parallelisation Snapshot
| Mitigation Track | Ready When | Owner | Parallel Tracks |
| --- | --- | --- | --- |
| Example | Chaos harness updated | name | Parallel with alerting upgrade |
Clarify which mitigations can progress concurrently and what enabling work must exist first.

## Shared Components & Unblocking Candidates
List reusable tooling (kill scripts, fixtures, dashboards) and infrastructure prerequisites that unlock multiple mitigations. Flag enabling work that belongs in an unblocking plan.

## Critical Components In Scope
List the services or workflows included (gateway, scheduler, DNS, exporters, CLI, etc.).

## Observed Failure Modes
Describe known outages or risks motivating the work. Link to incident reports or lab findings.

## Hardening Objectives
- Objective 1 (e.g., "Ensure gossip recovers within 5s")
- Objective 2

## Mitigation Matrix
| Component | Mitigation | Owner | Roadmap Link | Verification |
| --- | --- | --- | --- | --- |
| Example | Dual listeners + shared dedupe KV | Control Plane | `../../roadmap/...` | Kill test, `go test ./...` |
Document critical path vs. parallel-ready mitigations within the table notes.

## Operational Playbook Updates
List updates required in runbooks or CLIs so operators can execute the mitigations.

## Instrumentation & Telemetry
Metrics, logs, health endpoints, tracing, alerts to add or refine. Capture target thresholds.

## Test Strategy
- Unit: helpers for fencing/retry/etc.
- Integration: process kill/drain scenarios.
- Lab/Production Drills: describe scripts, environments, artefacts to capture.

## Follow-Up Work (YYYY-MM-DD)
Document the remaining hardening gaps or future iterations. Follow the roadmap checklist format, omitting redundant trailing status text:
- [x] Completed (YYYY-MM-DD) – [<N> <Task name>](<path-to-task.md>)
- [ ] Planned – [<N> <Task name>](<path-to-task.md>)

Add a final verification line when you reconfirm status with roadmap owners, e.g. `Status verification: roadmap entries reviewed on YYYY-MM-DD.`

## References
Relative links to design docs, incident write-ups, shared dashboards, and automation scripts.
