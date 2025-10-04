# <Component Name> Design Spec

- **Identifier**: `<initiative>-<capability>`
- **Status**: [ ] Draft · [ ] In progress · [ ] Completed — last updated YYYY-MM-DD
- **Linked Tasks**:
  - [ ] `<initiative>-<capability>-<sequence>` – `../../docs/tasks/<feature>/<sequence>-<stage>.md`
- **Blocked by**:
  - `<initiative>-<capability>-<sequence>` / `../<related-design>/README.md`
- **Unblocks**:
  - `<initiative>-<capability>-<sequence>` / downstream features
- **Last Verification**: YYYY-MM-DD — inspected <files/tests> (capture evidence + commands)
- **Upstream Dependencies**:
  - `../<related-design>/README.md`
  - External specs or packages (relative links)

## Intent
Summarise the capability this component adds, the user-facing impact, and how it plugs into the existing Grid/Ploy/Skazka architecture.

## Parallelisation Snapshot
| Track | Ready When | Owner | Notes |
| --- | --- | --- | --- |
| Example task | Enabling fixture merged | name | Parallel with Task N+1 |
Highlight which tasks can start immediately, which wait on enabling work, and slack vs. critical path.

## Shared Components & Unblocking Candidates
List shared libraries, fixtures, environments, or scripts worth building first to unlock multiple tasks. Link to enabling work where relevant.

## Context
Explain the current behaviour or gap. Reference relevant design docs, task specs, or production findings that motivate the change.

## Goals
- Goal 1
- Goal 2

## Non-Goals
- Explicitly call out scenarios or surfaces that remain unchanged.

## Current State
Describe how things work today (include diagrams/flow if helpful). Note any constraints, scale numbers, or reliability metrics.

## Proposed Architecture
### Overview
Narrative of the end-to-end flow.

### Interfaces & Contracts
List APIs/CLIs/events added or modified. Provide example requests/responses or schemas.

### Data Model & Persistence
Tables, KV buckets, subjects, file formats touched.

### Failure Modes & Recovery
Enumerate failure scenarios and how the design handles them.

## Dependencies & Interactions
Reference runtime/services/packages that must change. Note sequencing with other tasks or releases, critical path vs. optional parallel work, and any shared reviewers or environments.

## Risks & Mitigations
| Risk | Impact | Mitigation |
| --- | --- | --- |
| Example | Service downtime | Feature flag + staged rollout |

## Observability & Telemetry
Metrics, logs, traces, health endpoints, CLI hooks required to operate the feature.

## Test Strategy
- Unit
- Integration
- E2E / lab validation
Call out new suites, fixtures, or automation required and who owns them.

## Rollout Plan
Phases (flag, shadow, GA), handoff steps, operator documentation updates.

## Open Questions
Track unresolved decisions. Update/close as they resolve.

## Follow-Up Work (YYYY-MM-DD)
Outline remaining tasks or future enhancements. Use the task-aligned checklist format:
- [x] Completed (YYYY-MM-DD) – [<N> <Task name>](<path-to-task.md>)
- [ ] Planned – [<N> <Task name>](<path-to-task.md>)

Close the section with a verification note when you confirm the task roster, e.g. `Status verification: task entries reviewed on YYYY-MM-DD.`

## References
Link to design docs, specs, or changelog entries this document depends on. Ensure relative paths.
