# <Integration Name> Alignment Spec

- **Identifier**: `<initiative>-<capability>`
- **Status**: [ ] Draft · [ ] In progress · [ ] Completed — last updated YYYY-MM-DD
- **Participating Systems**: <System/Project 1> · <System/Project 2> · <System/Project 3> (expand as needed)
- **Linked Tasks**:
  - [ ] `<initiative>-<capability>-<sequence>` – `docs/tasks/<area>/<sequence>-<stage>.md`
- **Blocked by**:
  - `docs/design/<doc>/README.md`
  - `<initiative>-<other-capability>-<sequence>`
- **Unblocks**:
  - Downstream integrations/tasks (relative links)
- **Last Verification**: YYYY-MM-DD — compared against <tests/fixtures/documents>
- **Upstream Dependencies**:
  - `docs/design/<doc>/README.md`
  - `<@owner>/<project>/blob/main/<doc>/README.md`
  - External API specs/packages (relative links)

## Purpose
Clarify why the integration is needed and the business/user impact.

## Scope
Define what is covered in this alignment (protocols, payloads, workflows) and what stays out of scope.

## Current Contract Snapshot
Describe the as-is behaviour for every participating system (endpoints, subjects, CLI flows). Include tables if helpful.

## Target Contract
Detail the desired contract: payload schemas, subjects/queues, CLI commands, environment variables, and error handling. Provide canonical examples.

## System Responsibilities
| System | Responsibilities | Required Changes |
| --- | --- | --- |

## Compatibility & Migration
Explain how you will migrate from current to target state: feature flags, dual-write, backfill steps, cutover criteria.

## Testing & Validation
- Shared contract tests (location + owner)
- Integration/Lab runs required
- Tooling or fixtures to add (link back to design doc + task specs)

## Telemetry & Debugging
Define metrics, logs, and tooling needed to debug the integrated path across repos.

## Risks & Mitigations
List cross-repo risks and how to address them (e.g., version skew, auth failures, schema drift).

## Decision Log
| Date | Decision | Context / Link |
| --- | --- | --- |
| YYYY-MM-DD | Example decision | Refer to issue/PR |

## Follow-Up Tasks (YYYY-MM-DD)
Enumerate tasks created or updated because of this alignment. Use the same checklist structure as the Workspace HTTP API design, without the redundant trailing status:
- [x] Completed (YYYY-MM-DD) – [<N> <Task name>](<path-to-task.md>)
- [ ] Planned – [<N> <Task name>](<path-to-task.md>)

Capture a verification note once the task alignment is double-checked, e.g. `Status verification: task entries reviewed on YYYY-MM-DD.`

## References
Relative links to source design docs, API schemas, SDK docs, and changelog entries.
