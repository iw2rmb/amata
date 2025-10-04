# <Task ID> – <Concise Title>

- **Identifier**: `<initiative>-<capability>-<sequence>`
- [ ] **Status**: Not started · In progress · Completed (set one, include date)
- **Blocked by**:
  - `<initiative>-<capability>-<sequence>` / `../design/<feature>/README.md`
- **Unblocks**:
  - Downstream tasks/features (relative links)
- **Planned Complexity (COSMIC)**
  - Sized on: YYYY-MM-DD · Planned CFP: _

| Functional process | E | X | R | W | CFP |
|--------------------|---|---|---|---|-----|
| <name>             |   |   |   |   |     |
| TOTAL              |   |   |   |   |     |

  - Assumptions / notes: TODO
- **Factual Complexity (COSMIC)**
  - Measured on: YYYY-MM-DD · Actual CFP: _ (fill after implementation)

| Functional process | E | X | R | W | CFP |
|--------------------|---|---|---|---|-----|
| <name>             |   |   |   |   |     |
| TOTAL              |   |   |   |   |     |

  - Variance / follow-ups: TODO

- **Why**
  - Describe the customer or system problem this task solves. Reference design doc section(s).
- **How / Approach**
  - Summarise the implementation strategy (keep high-level; details belong in code review and design doc).
- **Changes Needed**
  - `path/to/file.go` – what to add/update
  - `cmd/...` – flags, wiring, CLI UX
- **Definition of Done**
  - Behavioural acceptance criteria
  - Feature flags / config toggles updated
  - Documentation updated if required
- **Tests To Add / Fix**
  - Unit: `<package>/_test.go`
  - Integration: `<path>`
  - Lab / manual: commands to run, artefacts to capture
- **Parallelisation Notes**
  - Shared fixtures, data, or helpers needed by neighbouring tasks
  - Environments/reviewers needed in parallel
  - Whether this task requires enabling work to land first
- **Dependencies & Blockers**
  - Other tasks or upstream merges required (cross-check with Blocked by list)
- **Verification Steps**
  - Commands/tests to run before marking complete (record date + evidence)
- **Changelog / Docs Impact**
  - Note if `CHANGELOG.md` or runbooks must update
- **Notes**
  - Scratchpad for reviewers or follow-ups
