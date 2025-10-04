# <Program / Epic Name>

- **Identifier**: `<initiative>-<capability>`
- **Owning Plan**: `[<Plan Name>](../../docs/plans/<initiative>/README.md)`
- **Status**: [ ] Draft · [ ] In progress · [ ] Completed — last updated YYYY-MM-DD
- **Linked Roadmap Folder**: `../../roadmap/<program>/`
- **Blocked by**:
  - `../<upstream-program>/README.md`
  - `<initiative>-<capability>-<sequence>`
- **Unblocks**:
  - Downstream programmes/features/tasks (relative links)
- **Last Verification**: YYYY-MM-DD — reviewed <files/tests> and roadmap checkboxes
- **Upstream Dependencies**:
  - Cross-program docs (relative links)
  - External requirements/mandates

## Intent
Describe the transformation or large milestone this program delivers and why it matters.

## Parallelisation Snapshot
| Feature / Stage | Ready When | Owner | Parallel Tracks |
| --- | --- | --- | --- |
| Stage 1 | Enabling API merged | name | Parallel with Stage 2 tasks |
Outline which stages can start concurrently, prerequisites, and slack vs. critical path items.

## Shared Components & Unblocking Candidates
List shared tooling, schemas, infrastructure, or policy updates required before multiple features can progress. State whether they live in a dedicated unblocking plan.

## Success Metrics
List quantitative signals (latency, failure rate, adoption) and qualitative acceptance criteria.

## Stage Breakdown
| Stage | Status | Summary | Roadmap Tasks |
| --- | --- | --- | --- |
| Stage 1 | [ ] | What ships | `<initiative>-<capability>-01`, `<initiative>-<capability>-02` |

## Current State Snapshot
Narrate what has shipped so far, including code references and validation evidence.

## Upcoming Milestones
Detail the next deliverables, sequencing constraints, and gating criteria before moving ahead.

## Workstreams
### <Workstream A>
- Scope
- Owners
- Dependencies
- Risks & mitigations

### <Workstream B>
...

## Risk Register
| Risk | Likelihood | Impact | Mitigation | Owner |
| --- | --- | --- | --- | --- |

## Change Management & Communication
Describe how stakeholders are informed (release notes, changelog updates, operator briefings). Include changelog requirements.

## Test & Validation Plan
Summarise required failing tests, lab runs, beta phases. Link to roadmap tasks that add tests.

## Rollout & Adoption Plan
Phased enablement, feature flag strategy, operator training, fallback paths.

## Open Questions
Track unresolved decisions or dependencies.

## References
List supporting design docs, roadmap root, status dashboards, changelog entries.
