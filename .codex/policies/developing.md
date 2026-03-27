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
- Maintain one canonical test per behavior path.
- Avoid duplicate tests that do not add new assertions.
- Group tests by behavior domain (e.g., validation, orchestration).
- In negative tests:
  - Assert both failure result and absence of side effects.
- Use descriptive names encoding behavior and expected outcome.


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
