# Mandatory Instructions

When Codex agent is in the **interactive mode**, it **MUST** read and follow policies from `/Users/vk/.codex/policies/interactive-mode.md`.

## Policies

Additional policies **MUST** be loaded when naming is literally corresponds the task:

- When **composing desing docs**: `/Users/vk/.codex/policies/composing-design-docs.md`
- When **composing or implementing roadmaps**: `/Users/vk/.codex/policies/composing-and-implementing-roadmaps.md`

## Executing Commands Policy

**Always prefix commands with `rtk`**: this saves tokens by filtering out meaningless leftovers.

For example, 

```bash 
rtk grep -n ...` or `rtk git commit ...
```

If RTK has a dedicated filter, it uses it. If not, it passes through unchanged. This means RTK is **always safe to use**. Even in command chains with `&&`, use `rtk`.

## Development Policy

- **NO** backward compatibility is required.
- **Always** prefer **architecture-wide** solutions over ways to save time or effort.
- When editing files with 500+ LOC, **always** consider to split them logically into smaller ones with distinctive and clear domains.
- Compose commit message from the current diff.

## Fixing Policy

- **Absolute** priority is **the root cause** to locate and fix.
- **NO** band-aiding or quick fixes.

## Database Schema Development Policy

- Modifying schema:
  - update initial migration instead of creating migration.
  - update `CREATE` statements instead of `ALTER`/`DROP` statements.
- Do **NOT** plan data migrations.

## Documentation Policy

### Folders Structure

- `design/` — design docs (how to implement).
- `research/` — research docs (what are options and how cool feature can possibly be).
- `roadmap/` — decomposed plans/implementation notes (in what order what to implement).
- `docs/` — actual state docs (how it works right now).

### Policy

- When committing, you **MUST** ensure that `docs/`, `roadmap/`, `design/`, `research/` updated with diff in mind.
- When updating, ensure that every document is kept within its domain or split documents.
- `design/**` and `roadmap/**` are short-lived working documents. Once the corresponding work is implemented and no unfinished design or roadmap depends on them as prerequisites, remove both the design doc and its roadmap.
- In `design/**` and `roadmap/**`, references to other design docs or roadmaps are allowed only for not-yet-implemented prerequisites. Do not use completed transient docs as long-lived explanations of current behavior.
- Explanations of shipped behavior, schemas, standards, instructions, difficult algorithms, decisions, and principles belong in `docs/**`. When design or roadmap text needs to explain current implementation, point to `docs/**`, not to completed design or roadmap history.
- `docs/**` is the long-lived, self-sufficient documentation surface. It must not refer to `design/**`, `research/**`, or `roadmap/**`.
- `docs/**` should not repeat design history. It should capture structured current-state snapshots of features, subjects, schemas, instructions, standards, and important implementation principles, relying on the codebase and code comments for low-level detail.
- When document exceeds 1000 LOC, consider splitting into narrower domains, or extracting `##`-sections or large tables into separate files.
- Keep all documents cross-referenced.
- For cross-reference integrity checks, run `/Users/vk/@iw2rmb/auto/scripts/check_docs_links.sh` from the target project root.

## Tests Writing Policy

- Prefer table-driven tests when setup and assertions are the same and only inputs or expected outcomes differ.
- Keep one canonical test per behavior path; represent input variants as table rows, not separate top-level tests.
- Group tests by behavior domain (validation, orchestration, state transitions) once a file grows beyond a few tests.
- In negative tests, assert both the response and the absence of side effects (for example, store writes were not called).
- Merge or remove tests that do not add a unique assertion beyond existing coverage.
- Use test names that encode both behavior and expected outcome.
- For test refactors, run focused targets first, then package-level tests.

## Feedback Loop

- The moment you realize that it would be much easier for you to work if there will be some information or automation provided to you, please write the idea into `~/@iw2rmb/auto/i-want.md`, and I will be providing my responses there under every request starting with `> `.
