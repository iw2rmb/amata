# Development Policy

## Core Principles

- Prefer reuse over creating new code. Extend or extract helpers when possible.
- Keep code small and modular unless there is a clear justification otherwise.
- Solve root causes instead of applying superficial fixes.
- Optimize for clarity, maintainability, and minimal duplication.


## Code Structure

- Split files exceeding ~500 LOC.
- Split functions exceeding ~100 LOC.
- Before implementing:
  - Check for existing helpers.
  - Check for similar logic and consolidate.


## Testing Guidelines

- Use table-driven tests when inputs vary but structure is the same.
- Before adding a new test function, scan the file for existing tests with the same arrange/act/assert shape.
- If shape matches, add a new table row to the existing test instead of creating another near-duplicate test function.
- Maintain one canonical test per behavior path.
- Avoid duplicate tests that do not add new assertions.
- Group tests by behavior domain (e.g., validation, orchestration).
- Do not add legacy-specific negative validation tests that exist only to reject previous contract shapes.
- Do not add legacy-specific rejection guards in runtime validation code.
- Validate only against the current contract/schema. If strict rejection of unknown keys is required, encode it in schema, not in ad hoc code branches for old formats.
- Use descriptive names encoding behavior and expected outcome.
- Acceptance gate: if 2+ tests in the same file share setup/assertion structure, rewrite them into a single table-driven test before finalizing.
- Exception: non-table tests are allowed only when behavior shape differs materially; include one short comment explaining why table-driven form is not appropriate.
- Response contract: when adding or changing tests, report `table-driven: yes|no`; if `no`, include a one-line reason.
- Optional CI warning recommendation: add a non-blocking check that flags repeated test setup/assertion patterns in the same file.


## Debugging and Fixing

For non-trivial or unclear issues:

1. Reproduce the issue programmatically in a realistic environment.
2. If tooling is missing, create minimal tools to reproduce.
3. Identify root cause.
4. Validate the fix using the same reproduction method.

Shortcut allowed when root cause is immediately obvious.


## Database Schema Development

- Modify initial migrations instead of creating new ones.
- Update `CREATE` statements instead of using `ALTER` or `DROP`.
- Do not plan or implement data migrations.


## Commit Practices

- Follow documentation update policy: `~/.codex/policies/updating-documentation.md`.
- Base commit messages on actual diff.
