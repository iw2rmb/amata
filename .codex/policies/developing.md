# Development Policy

Before writing a function:
- Check for designated helpers to cover parts or entire function.
- Check for similar function and consider to extend it or extract common ground into helper to reuse in both.

When commiting:
- Compose commit message from the current diff. 
- Follow **Updating Documentation** policy: `.codex/policies/updating-documentation.md`.


## Database Schema Development

- Modifying schema:
  - update initial migration instead of creating migration.
  - update `CREATE` statements instead of `ALTER`/`DROP` statements.
- Do **NOT** plan data migrations.


## Writing Tests

- Prefer table-driven tests when setup and assertions are the same and only inputs or expected outcomes differ.
- Keep one canonical test per behavior path; represent input variants as table rows, not separate top-level tests.
- Group tests by behavior domain (validation, orchestration, state transitions) once a file grows beyond a few tests.
- In negative tests, assert both the response and the absence of side effects (for example, store writes were not called).
- Merge or remove tests that do not add a unique assertion beyond existing coverage.
- Use test names that encode both behavior and expected outcome.
- For test refactors, run focused targets first, then package-level tests.


## Fixing

- **ALWAYS** prefer find and solve the root cause over local fix.
- For **EVERY** case, the algorithm is:
  - repeat it programmatically in the environment and conditions that are as close to actual ones as possible;
  - if there are no tools to do that: write them, if they buggy - fix them before continue;
  - find the root cause;
  - validate solution with that tool.
