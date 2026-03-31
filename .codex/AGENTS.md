# General Mandatory Instructions

When in **interactive mode**, read and follow policies from `~/.codex/policies/interactive-mode.md`.

Read and follow policies that correspond to the nature of the current task:
- **Composing Desing Docs**: `~/.codex/policies/composing-design-docs.md`
- **Composing Roadmaps or Implementation Steps**: `~/.codex/policies/composing-roadmaps.md`
- **Committing Codebase or Updating Documentation**: `~/.codex/policies/updating-documentation.md`
- **Developing or Fixing**: `~/.codex/policies/developing.md`
- **Developing TUI**: `~/.codex/policies/developing-tui.md`
- **Deciding on Architecture**: `~/.codex/policies/deciding-on-architecture.md`

Both in designing DD/roadmaps, and in development, **NO** backward compatibility is required, unless it explicitly stated by user.


## Anti-Looping Rule

When implementing or modifying code:
- Read the relevant source files FIRST. Do not reason about file contents you haven't read.
- If you catch yourself re-analyzing the same question, STOP thinking and take the next concrete action (read a file, write code, run a test).
- Never try to resolve all ambiguity upfront. Implement your best understanding, run tests, and iterate.
  

## Structured Output Runs

When a run has a strict JSON schema response requirement:
- Emit exactly one final schema-conformant answer. Do not send progress updates formatted as partial schema answers.
- Do not encode progress text as synthetic `gaps` items.
- Keep review scope tightly bounded to requested item files, acceptance criteria, and required verification commands.
- Run long verification commands sequentially (not in parallel) to avoid orphaned tool calls.
- Use per-command execution limits and fail fast: if verification cannot complete, return `ok=false` with an explicit actionable timeout gap.


## Documentation Folders Structure

In every project, follow convetion:
- `design/` — design docs (how to implement).
- `research/` — research docs (what are options and how cool feature can possibly be).
- `roadmap/` — decomposed plans/implementation notes (in what order what to implement).
- `docs/` — actual state docs (how it works right now).


## contents.md

File `contents.md` in any folder contains meanignful one-liner summaries for every first-level git tracked entity in that folder.
Prefer search over `contents.md`s to prevent context bloat with entire contents of files.
Content of `contents.md`s is kept actual via automatic updates on every commit.

When and how to use it effectively: 
- Start any search with `git grep -i "{pattern}" -- "**/contents.md"`.
- Only when you find no match in `contents.md`s, use `rg` over files' contents.
