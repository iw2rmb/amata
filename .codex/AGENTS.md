# Mandatory Instructions

When Codex agent is in the **interactive mode**, it **MUST** read and follow policies from `~/.codex/policies/interactive-mode.md`.
Don't use `rtk ` prefix when executing commands, it strips important data unpredictibly.

## Policies

Additional policies **MUST** be loaded when naming is literally corresponds the task:

- When **composing desing docs**: `~/.codex/policies/composing-design-docs.md`
- When **composing or implementing roadmaps**: `~/.codex/policies/composing-and-implementing-roadmaps.md`


## Aster

- Aster is a framework for the effective codebase navigation and manipulation.
- Get instructions at `http://localhost:9090/v1/coding-agents-instructions`.


## Architecture Patterns Policy

Repetitive boilerplate is the primary source of bugs.
The solution is to unify and generalize.


### Overengineering

Overengineering is the second source of bugs.
It comes from generalization with unclear boundaries.

To protect yourself from overengineering, 
- write down edge-cases before writing the code.
- keep scopes and domains distinctive and clean.


### Race conditions

Race conditions must be solved by:
  - execution order determinism,
  - execution independence;
  but never by time waits because they are not guaranteed.
  
  
### [TRIZ](https://en.wikipedia.org/wiki/TRIZ)

Best solution excludes the problem. For example,
  - Complex cache management and invalidation can be solved by fixing perfomance bottles that led to caching in the first place, thus removing caching.
  - Complex state management can be solved by splitting components into distinctive domains with clear borders.
  - Complex support of multiple communication channels can be solved by stripping to one that fits all needs.


## Context Window Compacting Policy

The most effective way to compact context window is:
- Precisely scope the current task;
- Pin docs and code related;

After compact and before proceeding, reread:
  - ~/.codex/AGENTS.md, 
  - scope, docs, code related, 
  - and the last conversation interaction.


## Development Policy

- **NO** backward compatibility is required.
- **ALWAYS** prefer architecture-wide solutions over time-saving band-aids.
- Compose commit message from the current diff.
- 500+ LOC files and 100+ LOC functions are first-class signs for mixed boundaries, thus overengineering; and are candidates to split or simplify.


## Fixing Policy

- **ALWAYS** prefer find and solve the root cause over local fix.
- For **EVERY** case, the algorithm is:
  - repeat it programmatically in the environment and conditions that are as close to actual ones as possible;
  - if there are no tools to do that: write them, if they buggy - fix them before continue;
  - find the root cause;
  - validate solution with that tool.


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
- For cross-reference integrity checks, run `~/@iw2rmb/auto/scripts/check_docs_links.sh` from the target project root.


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
