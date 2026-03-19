# Hardcore Research

## Summary

Capture deferred design work that should not block `amata/v1`.

This document is research, not an implementation-ready design. The shipped first-version contract lives in [docs/engine/index.md](../docs/engine/index.md).

## Deferred Subjects

### 1. Attempts, retries, and provider recovery

Questions to resolve:
- Are attempts and retries distinct concepts in the engine, or is one sufficient?
- What is the durable boundary for a step that mutates the repository?
- How should recovery behave when a provider-specific session can be resumed?
- When a resumed step already left diffs in the repo, should the recovery prompt switch to a shape such as `You are resuming ...`?
- Should Codex and Claude share one recovery model or keep provider-specific contracts?

Evidence needed before design work:
- A clear vocabulary for attempts, retries, reruns, and resume.
- At least one end-to-end scenario covering repo diffs, partial artifacts, and a resumed provider session.
- A step-result durability rule that avoids duplicate commits or lost work.

### 2. Parallelism

Questions to resolve:
- What is the workspace-isolation model for parallel branches?
- Are separate worktrees required for mutating branches?
- Which executors are safe to run in parallel without extra isolation?
- How should artifacts, logs, and child failures be merged back into the main run?

Evidence needed before design work:
- A workspace-locking or workspace-cloning strategy.
- A failure model for partial branch completion.
- A concrete workflow that benefits materially from parallel execution.

### 3. Human-in-the-loop

Questions to resolve:
- What is the unit of human approval: step, flow, milestone, or diff snapshot?
- How should prompts, artifacts, and pending decisions be presented to the operator?
- What is the durable state model while waiting for a human response?

Evidence needed before design work:
- At least one concrete local workflow where a human gate adds clear value.
- A persisted state shape for pending decisions.
- A CLI interaction model that does not leak implementation details into workflow specs.

### 4. Pause and Continue

Questions to resolve:
- Is pause operator-driven, workflow-driven, or both?
- How does a paused run differ from a failed run waiting for `resume`?
- What state must be persisted to continue safely after a pause?
- How should pause interact with agent subprocesses that are still running?

Evidence needed before design work:
- A lifecycle model for `running`, `paused`, `failed`, and `completed`.
- A safe stop rule for in-flight executors.
- A concrete CLI contract for pause and continue operations.

## References

- First-version contract: [docs/engine/index.md](../docs/engine/index.md)
