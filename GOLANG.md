# Golang Tooling Instructions

## Core Tooling
- Run `gofmt -w` on every edited Go file; only switch to `gofumpt` if the repository already standardizes on it.
- Follow up with `goimports -w` when imports need automatic grouping or standardization.
- Execute `staticcheck ./...` on affected packages whenever behavior, public APIs, or concurrency logic change.
- Use `go vet ./...` to catch compilation-level issues before handoff; tighten the package set if only a subset is relevant.
- Validate behavior with `go test ./...`; prefer narrowing to the touched packages during iteration and finish with the wider suite (or coverage flags) before sign-off.
- Run `govulncheck ./...` when touching dependency graphs or releasing security-sensitive code.

## Workflow Expectations
- Limit formatting, vetting, linting, and tests to the code that changed unless a broader run is necessary; call out any intentional gaps.
- Retry failing commands once; if they still fail, include the exact command and output summary in your response with your interpretation.
- Keep the worktree clean between tool invocations so diffs correlate with tool feedback and failures are easier to triage.
- Record the ordered list of commands run so the user can reproduce the sequence quickly.

## Verification Notes
- Confirm tool output matches expectations before replying (formatting applied, warnings resolved, tests green).
- Surface remaining warnings or test failures explicitly, along with suggested follow-up actions.
- Store persistent artifacts (coverage profiles, logs) under `logs/` inside the repository when references are needed.
