# Short Polling Contract

## Summary
Add a generic amata executor for external asynchronous work orchestration:
- `polling.short` for request + short polling.

This document defines the contract only. Implementation details should stay in code/tests.

## Scope
In scope:
- Step contract for `polling.short`.
- Runtime invariants for timeout, resume, and failure behavior in `polling.short`.

Out of scope:
- Ploy-specific endpoint shapes.
- Migration/heal business logic.

## Why This Is Needed
Current amata has no built-in HTTP request/poll executor. Shell+curl works but is hard to validate, hard to resume safely, and duplicated across flows.

## Goals
- Generic and reusable polling executor.
- Deterministic success/failure behavior.
- Safe resume using step checkpoint records.

## Non-goals
- Embedding ploy domain logic in amata core.
- Replacing `shell` for advanced custom network workflows.

## Current Baseline (Observed)
- Built-ins do not include HTTP/poll executors: `../internal/runtime/builtins.go`.
- Runtime resume re-enters unfinished steps; generic executors do not persist step-local progress by default: `../internal/runtime/runner.go`, `../internal/runtime/runner_progress.go`, `../internal/state/store.go`.

## Target Contract or Target Architecture
Canonical step shape:

```yaml
polling.short:
  request:
    url: "https://..."
    method: POST            # optional, default POST
    headers: {}             # optional map
    body: {}                # optional any
    timeout: 30s            # optional, default 30s
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
- The first `confirm` attempt runs immediately after successful `request`; `confirm.interval` applies only between subsequent `confirm` attempts.
- `confirm.url` may reference request response fields via runtime context.
- `request.timeout` defaults to `30s` when omitted.
- `done_when` decides when polling stops and must resolve to a boolean.
- `success_when` decides final step status after `done_when=true` and must resolve to a boolean.
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
  - continue confirm polling without re-running `request` after a checkpointed successful `request`,
  - return terminal result without extra HTTP calls when already terminal,
  - preserve confirm-timeout continuity across resume.
- Startup behavior:
  - no checkpoint -> execute `request`.
  - valid non-terminal checkpoint -> skip `request`, continue `confirm`.
  - valid terminal checkpoint -> return checkpointed terminal result.
  - checkpoint exists but invalid/unreadable -> fail `invalid_checkpoint`.
- Checkpoint cleanup is runtime-owned and only after durable `step_recorded` for same `(frame_id, step_index)`.

Deterministic failure codes:
- `invalid_request`
- `invalid_confirm`
- `request_failed`
- `request_http_status`
- `confirm_failed`
- `confirm_http_status`
- `invalid_done_when` (cannot evaluate/resolve `done_when`, or result is not boolean)
- `invalid_success_when` (cannot evaluate/resolve `success_when`, or result is not boolean)
- `polling_unsuccessful`
- `confirm_timeout`
- `invalid_checkpoint`

Runtime-owned failure codes (outside polling HTTP logic):
- `step_stalled`
- `invalid_stall`
- `deadline_exceeded`
- `canceled`

## Implementation Notes
- Register executor in `../internal/runtime/builtins.go`.
- Add and register schema:
  - `../schemas/polling.short.amata.schema.json`
  - `../internal/spec/step_schemas.go`
- Keep platform-global `response` behavior unchanged.
- Extend executor step context with durable `frame_id`.
- Reuse shared HTTP helper for timeout/headers/body encoding.

## Milestones
1. Schema + registration.
- Expected result: `amata validate` accepts `polling.short`.
- Testable outcome: schema validation tests pass.

2. `polling.short` executor.
- Expected result: deterministic polling flow with checkpoint-based resume.
- Testable outcome: tests cover success, timeout, unsuccessful terminal status, transport/status failures, decode behavior, resume after checkpointed successful request without re-running request, malformed checkpoint behavior, terminal resume without extra HTTP.

## Acceptance Criteria
- `polling.short` is generic and ploy-agnostic.
- `polling.short` supports `confirm.url` templating from request response.
- `polling.short` evaluates `done_when`/`success_when` deterministically.
- `polling.short` resume does not re-run `request` when checkpoint records a successful request.
- `polling.short` resume returns terminal checkpoint result without extra HTTP calls.
- `polling.short` confirm timeout is consistent across resume.
- `amata validate` accepts `polling.short`.

## Risks
- Weak `done_when`/`success_when` expressions can make workflows brittle.
- Poor checkpoint keying can bind to the wrong step record.
- Provider-specific polling behavior may require per-provider interval tuning.

## References
- `~/@iw2rmb/amata/internal/runtime/builtins.go`
- `~/@iw2rmb/amata/internal/runtime/runner.go`
- `~/@iw2rmb/amata/internal/runtime/runner_progress.go`
- `~/@iw2rmb/amata/internal/state/store.go`
