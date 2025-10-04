# <Task ID> – <Validation Scenario>

- **Identifier**: `<initiative>-<capability>-<sequence>`
- [ ] **Status**: Not started · In progress · Completed (include date + operator)
- **Blocked by**:
  - Environment readiness, fixtures, or upstream tasks (relative links)
- **Unblocks**:
  - Downstream validations, release gates, or launch tasks (relative links)
- **Owners / Operators**: TODO

- **Why**
  - Outline the risk or regression this validation addresses. Link to design doc / incident / SLA.
- **Scenario Overview**
  - Describe the environment (lab/staging/prod mimic), data sets, and expected workflow.
- **Parallelisation Notes**
  - Lab slot contention, shared tooling, or data resets required between runs
  - Coordinated schedules with other teams or validations
- **Prerequisites**
  - Lab credentials, feature flags, fixtures, scripts to prepare.
- **Execution Steps**
  1. Step-by-step commands (copyable)
  2. Expected checkpoints / screenshots / artefacts
- **Evidence To Capture**
  - `notes/<run>.tar.gz`
  - `reports/<date>.json`
  - Log / metric snapshots
- **Definition of Done**
  - Acceptance criteria for the run (include thresholds, success metrics).
- **Tests / Automation**
  - Automated suites triggered (e.g., `go test ./tests/lab`)
  - CI jobs that must stay red until work lands
- **CHANGELOG Update**
  - Add a dated summary to `docs/CHANGELOG.md` with command transcripts, artefacts, and follow-up owners.
- **Follow-Ups**
  - Issues or tasks spawned from findings.
