# What will help me to do my job better and easier
- Need a reliable documented non-interactive `claude -p` wrapper (with auth/state checks and timeout) because direct CLI invocation intermittently reports missing prompt input or hangs without output.
> Wrapper: ~/@iw2rmb/auto/claude-wrapper.sh.
> Instructions: ~/@iw2rmb/auto/claude-wrapper.md.
- Add a repo-standard docs cross-reference checker script (e.g., tests/check_docs_cross_refs.sh) that validates concrete local doc/code links and ignores symbol/glob backticks.
- 2026-03-06: Add a repo script (e.g. `tests/check_docs_links.sh`) that validates markdown cross-references under `design/ docs/ research/ roadmap/` and fails on missing local files; this would avoid manual `rg` ad-hoc checks.
> Script implemented and instructions provided in the global AGENTS.md.
- Please add a preflight disk-space check (and optional cleanup hint) before running Rust/Swift test builds, because full-volume failures now block validation late in the cycle.
- A reusable non-interactive app/macOS regression harness for multiple concurrent interactive command blocks (for example two `psql` sessions with focus switches and typed input) would make focus/pending-tail bugs much faster to reproduce and verify than current ad hoc test-hook assembly.
- Add a dedicated testing-tools index in the docs repo (human README + machine-readable index) for replay tools, frame capture, test hooks, and scenario runners; today that information is scattered across design docs, runtime validation notes, and code comments, which makes discovery unreliable during implementation.
- A documented subagent handoff summary would help orchestration: when a spawned Codex agent changes files, show whether those edits landed in my current worktree, stayed on an isolated branch, or require an explicit merge/cherry-pick step.
- A tiny repo-local Dagu smoke-test harness for stubbed agent CLIs would help workflow changes: this failure came from Dagu output interpolation semantics, and catching that in one scripted fixture would be much faster than reading nested dag-run logs after a full roadmap run.
- A shared repo helper for "ensure Rust FFI is built if inputs changed" would help local Swift test loops; the same invalidation logic is currently re-decided across shell scripts and Rust test helpers, and a single reusable command would avoid repeated cargo startup without duplicating dependency guesses in each caller.
