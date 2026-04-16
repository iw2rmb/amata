# Short Polling + Download Contract

## Summary
Add two generic amata executors for external asynchronous work orchestration:
- `polling.short` for request + short polling.
- `download` for file retrieval.

This document defines the contract only. Implementation details should stay in code/tests.

## Scope
In scope:
- Step contracts for `polling.short` and `download`.
- Runtime invariants for timeout, resume, and failure behavior.

Out of scope:
- Ploy-specific endpoint shapes.
- Migration/heal business logic.

## Why This Is Needed
Current amata has no built-in HTTP request/poll/download executors. Shell+curl works but is hard to validate, hard to resume safely, and duplicated across flows.

## Goals
- Generic and reusable HTTP executors.
- Deterministic success/failure behavior.
- Safe resume without replaying completed external operations.

## Non-goals
- Embedding ploy domain logic in amata core.
- Replacing `shell` for advanced custom network workflows.

## Current Baseline (Observed)
- Built-ins do not include HTTP/poll/download executors: `../internal/runtime/builtins.go`.
- `data.get` reads local files only: `../internal/executor/dataget/dataget.go`.
- Runtime resume re-enters unfinished steps; generic executors do not persist step-local progress by default: `../internal/runtime/runner.go`, `../internal/runtime/runner_progress.go`, `../internal/state/store.go`.

## Target Contract or Target Architecture
### `polling.short`
Canonical step shape:

```yaml
polling.short:
  request:
    url: "https://..."
    method: POST            # optional, default POST
    headers: {}             # optional map
    body: {}                # optional any
    timeout: 30s            # optional
  confirm:
    url: "{{ ctx.value.request.response.value.status_url }}"
    method: GET             # optional, default GET
    headers: {}             # optional map
    interval: 3s            # optional, default 3s
    timeout: 20m            # optional, default 20m
    request_timeout: 30s    # optional, default 30s
  done_when: "ctx.value.confirm.response.value.status in ['ok','failed']"   # required expr
  success_when: "ctx.value.confirm.response.value.status == 'ok'"            # required expr
```

Rules:
- Execute exactly one `request`, then repeat `confirm` until terminal.
- `confirm.url` may reference request response fields via runtime context.
- `done_when` decides when polling stops.
- `success_when` decides final step status after `done_when=true`.
- `done_when=true` and `success_when=false` must fail with `polling_unsuccessful`.
- If `confirm.timeout` elapses before `done_when=true`, fail with `confirm_timeout`.
- Confirm loop timeout is wall-clock and must remain consistent across resume.
- Per-confirm call timeout must be bounded by remaining overall confirm timeout.
- Body decode for request and confirm is deterministic:
  - empty/whitespace-only -> `null`
  - valid JSON -> decoded JSON
  - otherwise -> raw string
- Runtime-owned failures remain runtime-owned (`step_stalled`, `invalid_stall`, `deadline_exceeded`, `canceled`).

Returned step value:

```yaml
request:
  response:
    status: <int>
    headers: <map>
    value: <decoded body>
confirm:
  attempts: <int>
  response:
    status: <int>
    headers: <map>
    value: <decoded body>
```

Resume/checkpoint invariants:
- Persist checkpoint after successful `request` and after each successful `confirm`.
- Checkpoint must be atomic (`tmp + rename`) and scoped by `(frame_id, step_index)`.
- Checkpoint must include enough data to:
  - detect whether invocation matches current resolved input (`invocation_key`),
  - continue confirm polling without replaying `request`,
  - return terminal result without extra HTTP calls when already terminal,
  - preserve confirm-timeout continuity across resume.
- Startup behavior:
  - no checkpoint -> execute `request`.
  - valid matching non-terminal checkpoint -> skip `request`, continue `confirm`.
  - valid matching terminal checkpoint -> return checkpointed terminal result.
  - checkpoint exists but invalid/unreadable -> fail `invalid_checkpoint`.
  - valid checkpoint but invocation mismatch -> ignore as stale; execute fresh `request`.
- Checkpoint cleanup is runtime-owned and only after durable `step_recorded` for same `(frame_id, step_index)`.

Deterministic failure codes:
- `invalid_request`
- `invalid_confirm`
- `request_failed`
- `request_http_status`
- `confirm_failed`
- `confirm_http_status`
- `invalid_done_when`
- `invalid_success_when`
- `polling_unsuccessful`
- `confirm_timeout`
- `invalid_checkpoint`

### `download`
Canonical step shape:

```yaml
download:
  url: "https://..."
  path: "/out/file.log"      # required absolute or workspace-relative path
  method: GET                 # optional, default GET
  headers: {}                 # optional map
  timeout: 5m                 # optional
  mode: "0644"               # optional, default 0644
```

Rules:
- Fail on non-2xx response.
- Write file atomically in same parent directory (temp file + flush + rename).
- Create parent directories when missing.
- Clean up temp file on error paths.
- Return metadata: `status`, `size_bytes`, `sha256`, `path`.

Deterministic failure codes:
- `invalid_download`
- `download_failed`
- `download_http_status`
- `download_write_failed`

## Implementation Notes
- Register executors in `../internal/runtime/builtins.go`.
- Add and register schemas:
  - `../schemas/polling.short.amata.schema.json`
  - `../schemas/download.amata.schema.json`
  - `../internal/spec/step_schemas.go`
- Keep platform-global `response` behavior unchanged.
- Extend executor step context with durable `frame_id`.
- Reuse shared HTTP helper for timeout/headers/body encoding.

## Milestones
1. Schemas + registration.
- Expected result: `amata validate` accepts both step types.
- Testable outcome: schema validation tests pass.

2. `polling.short` executor.
- Expected result: deterministic polling flow with checkpoint-based resume.
- Testable outcome: tests cover success, timeout, unsuccessful terminal status, transport/status failures, decode behavior, resume without request replay, stale/malformed checkpoint behavior, terminal resume without extra HTTP.

3. `download` executor.
- Expected result: atomic write and metadata output.
- Testable outcome: tests cover non-2xx handling, checksum/size, parent-dir creation, and temp cleanup.

## Acceptance Criteria
- `polling.short` and `download` are generic and ploy-agnostic.
- `polling.short` supports `confirm.url` templating from request response.
- `polling.short` evaluates `done_when`/`success_when` deterministically.
- `polling.short` resume does not replay `request` when checkpoint matches.
- `polling.short` resume returns terminal checkpoint result without extra HTTP calls.
- `polling.short` confirm timeout is consistent across resume.
- `download` write is atomic and leaves no orphan temp file on known failures.
- `amata validate` accepts both step types.

## Risks
- Weak `done_when`/`success_when` expressions can make workflows brittle.
- Poor checkpoint keying can cause stale reuse or unnecessary replay.
- Provider-specific polling behavior may require per-provider interval tuning.

## References
- `~/@iw2rmb/amata/internal/runtime/builtins.go`
- `~/@iw2rmb/amata/internal/executor/dataget/dataget.go`
- `~/@iw2rmb/amata/internal/runtime/runner.go`
- `~/@iw2rmb/amata/internal/runtime/runner_progress.go`
- `~/@iw2rmb/amata/internal/state/store.go`
