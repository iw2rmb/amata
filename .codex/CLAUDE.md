# General Mandatory Instructions

Read and follow policies that correspond to the nature of the current task:
- **Committing Codebase or Updating Documentation**: `~/.codex/policies/updating-documentation.md`
- **Developing or Fixing**: `~/.codex/policies/developing.md`
- **Developing TUI**: `~/.codex/policies/developing-tui.md`

Both in designing DD/roadmaps, and in development, **NO** backward compatibility is required, unless it explicitly stated by user.


## Anti-Looping Rule

HARD RULE — your FIRST action on any task MUST be a tool call (Read, Glob, Grep, Bash).
Do NOT reason about what files might contain or what a task "really means" before reading code.

- Read the relevant source files FIRST. Do not reason about file contents you haven't read.
- If a task mentions a file, function, or concept — read it immediately. Do not theorize.
- If you catch yourself writing "I'm realizing", "Let me step back", "the question is really about", or re-analyzing the same topic — STOP thinking and call a tool.
- Never try to resolve all ambiguity upfront. Take the most literal reading, start implementing, course-correct after seeing the code.
- Maximum thinking before first tool call: ~500 tokens. If you haven't called a tool yet, you're looping.
  

## Structured Output Runs

When a run has a strict JSON schema response requirement:
- Emit exactly one final schema-conformant answer. Do not send progress updates formatted as partial schema answers.
- Keep review scope tightly bounded to requested item files, acceptance criteria, and required verification commands.
- Run long verification commands sequentially (not in parallel) to avoid orphaned tool calls.


## contents.md

Files `contents.md` in any folder contain meanignful one-liner summaries for every first-level git tracked entity in those folders.
Read `contents.md` in a folder to have high-level understanding of the folder contents without need of reading every file in that folder.
When performing broad search over project, run `git grep -i "{pattern}" -- "**/contents.md"` first, and use findings to narrow search over files contents.
