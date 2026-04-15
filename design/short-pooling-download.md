# Short Polling + Download Contract

## Summary
Add two generic amata executors for external asynchronous work orchestration without binding amata to ploy internals:
- `pooling.short` for request + short polling.
- `download` for file retrieval.

This contract is the prerequisite for ploy and ploy-lib DDs and must be treated as canonical.

## Scope
In scope:
- Step contract for `pooling.short`.
- Step contract for `download`.
- Runtime invariants and failure semantics.

Out of scope:
- Ploy-specific endpoint shape.
- Migration/heal business logic.

## Why This Is Needed
Current amata has no built-in HTTP request/poll primitives. Shell+curl works, but it is hard to validate, harder to resume safely, and duplicates logic across flows.

## Goals
- Keep executors generic and reusable.
- Keep schema-validated response contracts.
- Keep deterministic failure/cancel behavior.

## Non-goals
- Domain-specific ploy APIs in amata core.
- Replacing `shell` for advanced custom network logic.

## Current Baseline (Observed)
- Built-ins currently do not include HTTP/poll/download executors: `../internal/runtime/builtins.go`.
- `data.get` reads local files only: `../internal/executor/dataget/dataget.go`.
- Amata image already includes `curl`, `jq`, `yq`: `~/@iw2rmb/ploy/images/amata/Dockerfile`.

## Target Contract or Target Architecture
### `pooling.short`
Canonical step shape:

```yaml
pooling.short:
  request:
    url: "https://..."
    method: POST            # optional, default POST
    headers: {}             # optional map
    body: {}                # optional any
    timeout: 30s            # optional
    response:
      schema: "#/schemas/request_response"   # optional; if omitted, raw response is allowed
  confirm:
    url: "{{ ctx.value.request.response.value.status_url }}"
    method: GET             # optional, default GET
    headers: {}             # optional map
    interval: 3s            # optional, default 3s
    timeout: 20m            # optional, default 20m
    response:
      schema: "#/schemas/confirm_response"   # optional; if omitted, raw response is allowed
  done_when: "ctx.value.confirm.response.value.status in ['ok','failed']"   # required expr
  success_when: "ctx.value.confirm.response.value.status == 'ok'"            # required expr
```

Rules:
- Step must execute exactly one `request`, then repeated `confirm` calls.
- `confirm.url` may use request response fields via `ctx.value.request.response.value...`.
- `done_when` gates polling termination.
- `success_when` decides final step status after `done_when=true`.
- If timeout is reached before `done_when=true`, fail with `step_timeout`.
- Returned step value shape:

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

### `download`
Canonical step shape:

```yaml
download:
  url: "https://..."
  path: "/out/file.log"      # required absolute or workspace-relative path
  method: GET                  # optional, default GET
  headers: {}                  # optional map
  timeout: 5m                  # optional
  mode: "0644"                # optional, default 0644
  response:
    schema: "#/schemas/download_meta"   # optional metadata schema for step value
```

Rules:
- Writes payload to `path` atomically (`tmp + rename`).
- Creates parent directories when missing.
- Fails on non-2xx response.
- Returns metadata (`status`, `size_bytes`, `sha256`, `path`).

## Implementation Notes
- Add executor registrations in `../internal/runtime/builtins.go`.
- Add schemas:
  - `../schemas/pooling.short.amata.schema.json`
  - `../schemas/download.amata.schema.json`
- Keep `stall` behavior compatible with existing executor policy.
- Reuse shared HTTP client helper for timeout + headers + body encoding.

## Milestones
1. Add schemas + executor skeletons.
- Expected result: `amata validate` accepts both step types.
- Testable outcome: schema tests pass.

2. Implement `pooling.short` runtime.
- Expected result: deterministic request/poll flow with expression-based completion.
- Testable outcome: unit tests for success/timeout/failure paths.

3. Implement `download` runtime.
- Expected result: atomic file writes with metadata output.
- Testable outcome: unit tests for status handling and checksum.

## Acceptance Criteria
- Both new step types are generic and ploy-agnostic.
- `pooling.short` supports `confirm.url` templating from request response.
- Validation + runtime failure codes are deterministic.
- `resume` preserves consistent terminal state for these steps.

## Risks
- Poorly defined remote response schemas can cause brittle expressions.
- Long polling endpoints may require per-provider retry tuning.

## References
- `~/@iw2rmb/amata/internal/runtime/builtins.go`
- `~/@iw2rmb/amata/internal/executor/dataget/dataget.go`
- `~/@iw2rmb/ploy/images/amata/Dockerfile`
