# General Mandatory Instructions

When in **interactive mode**, read and follow policies from `~/.codex/policies/interactive-mode.md`.

Read and follow policies that correspond to the nature of the current task:
- **Composing Desing Docs**: `~/.codex/policies/composing-design-docs.md`
- **Composing Roadmaps or Implementation Steps**: `~/.codex/policies/composing-roadmaps.md`
- **Committing Codebase or Updating Documentation**: `~/.codex/policies/updating-documentation.md`
- **Developing or Fixing**: `~/.codex/policies/developing.md`
- **Developing TUI**: `~/.codex/policies/developing-tui.md`
- **Deciding on Architecture**: `~/.codex/policies/deciding-on-architecture.md`


## Baseline
- Both in designing DD/roadmaps, and in development, **NO** backward compatibility is required, unless it explicitly stated by user.
- Never add legacy-shape rejection guards or tests that enumerate previous contract states.
- Contract acceptance must be defined by the current schema/contract only. If strict key rejection is needed, express it in the schema (for example `additionalProperties: false`), not via ad hoc legacy-specific validation code.


## Documentation Folders Structure

In every project, follow convetion:
- `design/` — design docs (how to implement).
- `research/` — research docs (what are options and how cool feature can possibly be).
- `roadmap/` — decomposed plans/implementation notes (in what order what to implement).
- `docs/` — actual state docs (how it works right now).
