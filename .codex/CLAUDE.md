# General Mandatory Instructions

Read and follow policies that correspond to the nature of the current task:
- **Committing Codebase or Updating Documentation**: `~/.codex/policies/updating-documentation.md`
- **Developing or Fixing**: `~/.codex/policies/developing.md`
- **Developing TUI**: `~/.codex/policies/developing-tui.md`

Both in designing DD/roadmaps, and in development, **NO** backward compatibility is required, unless it explicitly stated by user.


## Anti-Looping Rule

When implementing or modifying code:
- Read the relevant source files FIRST. Do not reason about file contents you haven't read.
- If you catch yourself re-analyzing the same question, STOP thinking and take the next concrete action (read a file, write code, run a test).
- Never try to resolve all ambiguity upfront. Implement your best understanding, run tests, and iterate.
  

## Structured Output Runs

When a run has a strict JSON schema response requirement:
- Emit exactly one final schema-conformant answer. Do not send progress updates formatted as partial schema answers.
- Keep review scope tightly bounded to requested item files, acceptance criteria, and required verification commands.
- Run long verification commands sequentially (not in parallel) to avoid orphaned tool calls.


## contents.md

Files `contents.md` in any folder contain meanignful one-liner summaries for every first-level git tracked entity in those folders.
Read `contents.md` in a folder to have high-level understanding of the folder contents without need of reading every file in that folder.
When performing broad search over project, run `git grep -i "{pattern}" -- "**/contents.md"` first, and use findings to narrow search over files contents.
