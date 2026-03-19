# Live Agent Artifact Streaming

Scope: Stream `claude` and `codex` step `stdout`/`stderr` into artifact files while the provider process is still running, without changing workflow semantics.

Documentation: `docs/engine/index.md`

- [x] 1.1 Define streaming artifact contract for agent executors
  - Repository: `amata`
    1. Refactor `internal/executor/agent/agent.go` artifact flow so `stdout.txt` and `stderr.txt` can be created before provider execution and reused after provider completion.
    2. Add a shared stream-capture helper in `internal/executor/agent` that owns file lifecycle, writer wiring, and deterministic cleanup semantics.
    3. Keep `internal/executor/agent.Response` and `state.Artifacts` result semantics stable so downstream response resolution continues to read the same artifact paths.
    4. Define edge-case handling for cancellation, provider crash, and artifact write errors with explicit failure code mapping in agent executor boundaries.
  - Verification:
    1. Run `go test ./internal/executor/agent` and confirm existing artifact and metadata assertions still pass.
    2. Add tests that force file-open and file-write failures and confirm stable `artifact_capture_failed` behavior.
    3. Validate failed steps still persist partial `stdout.txt`/`stderr.txt` content produced before failure.
  - Reasoning: high

- [x] 1.2 Implement Codex stdout/stderr live streaming
  - Repository: `amata`
    1. Update `internal/executor/codex/codex.go` runner to stream process pipes to artifact-backed writers instead of buffering only in `bytes.Buffer`.
    2. Preserve transcript extraction from `last-message.txt` and structured output parsing flow used by `codex` response handling.
    3. Ensure command completion closes writers deterministically so readers can tail files without race-prone sleeps.
    4. Keep provider metadata command capture unchanged so diagnostics remain comparable across runs.
  - Verification:
    1. Add `internal/executor/codex` tests that observe incremental growth of `stdout.txt` while the fake command is still running.
    2. Add cancellation and non-zero exit tests to confirm streamed partial output is preserved.
    3. Run `go test ./internal/executor/codex ./internal/executor/agent`.
  - Reasoning: high

- [x] 1.3 Implement Claude stdout/stderr live streaming
  - Repository: `amata`
    1. Update `internal/executor/claude/claude.go` runner to stream process pipes to artifact-backed writers with the same helper contract as `codex`.
    2. Preserve structured-output envelope parsing behavior and `response.Value` population for both provider-schema and fallback modes.
    3. Keep prompt-transcript artifact behavior unchanged so response resolvers and tests continue to use current artifact names.
    4. Ensure stderr streaming does not interfere with CLI progress stream rendering responsibility in runtime layer.
  - Verification:
    1. Add `internal/executor/claude` tests for in-flight file writes and final structured parse correctness.
    2. Add failure-path tests to confirm streamed files persist when `claude -p` returns an error.
    3. Run `go test ./internal/executor/claude ./internal/executor/agent`.
  - Reasoning: high

- [x] 1.4 Validate runtime behavior and document shipped contract
  - Repository: `amata`
    1. Add integration-style runtime coverage in `internal/runtime/runner_test.go` to confirm agent step artifacts become readable during execution and remain stable after step completion.
    2. Keep live progress event ordering unchanged in `internal/progress` and `internal/runtime` while adding artifact streaming behavior.
    3. Update `docs/engine/index.md` executor behavior sections for `codex` and `claude` to state that `stdout`/`stderr` artifacts are streamed during execution.
    4. Cross-check docs links and references from `docs/index.md` after documentation updates.
  - Verification:
    1. Run focused tests: `go test ./internal/runtime ./internal/progress`.
    2. Run full regression: `go test ./...`.
    3. Run `~/@iw2rmb/amata/scripts/check_docs_links.sh` from repository root after doc edits.
  - Reasoning: xhigh
