# Download Contract

## Summary
Add a generic amata executor for file retrieval:
- `download` for HTTP download to disk.

This document defines the contract only. Implementation details should stay in code/tests.

## Scope
In scope:
- Step contract for `download`.
- Runtime invariants for timeout and failure behavior in `download`.

Out of scope:
- Ploy-specific endpoint shapes.
- Migration/heal business logic.

## Why This Is Needed
Current amata has no built-in HTTP download executor. Shell+curl works but is hard to validate and duplicated across flows.

## Goals
- Generic and reusable download executor.
- Deterministic success/failure behavior.
- Atomic file replacement behavior.

## Non-goals
- Embedding ploy domain logic in amata core.
- Replacing `shell` for advanced custom network workflows.

## Current Baseline (Observed)
- Built-ins do not include HTTP download executor: `../internal/runtime/builtins.go`.
- `data.get` reads local files only and does not fetch network resources: `../internal/executor/dataget/dataget.go`.

## Target Contract or Target Architecture
Canonical step shape:

```yaml
download:
  url: "https://..."
  path: "/out/file.log"      # required absolute or workspace-relative path
  method: GET                # optional, default GET
  headers: {}                # optional map
  timeout: 5m                # optional
  mode: "0644"               # optional, default 0644
  fail_if_exists: false      # optional, default false
```

Rules:
- Fail on non-2xx response.
- If `fail_if_exists=true` and target file already exists, fail without writing.
- If `fail_if_exists=false`, replace existing target atomically.
- Write file atomically in same parent directory (temp file + flush + rename).
- Create parent directories when missing.
- Clean up temp file on error paths.
- Return metadata: `status`, `size_bytes`, `sha256`, `path`.

Deterministic failure codes:
- `invalid_download`
- `download_failed`
- `download_http_status`
- `download_file_exists`
- `download_write_failed`

## Implementation Notes
- Register executor in `../internal/runtime/builtins.go`.
- Add and register schema:
  - `../schemas/download.amata.schema.json`
  - `../internal/spec/step_schemas.go`
- Keep platform-global `response` behavior unchanged.
- Reuse shared HTTP helper for timeout/headers/body encoding.

## Milestones
1. Schema + registration.
- Expected result: `amata validate` accepts `download`.
- Testable outcome: schema validation tests pass.

2. `download` executor.
- Expected result: atomic write and metadata output.
- Testable outcome: tests cover non-2xx handling, checksum/size, parent-dir creation, overwrite behavior, and temp cleanup.

## Acceptance Criteria
- `download` is generic and ploy-agnostic.
- `download` write is atomic and leaves no orphan temp file on known failures.
- `download` supports `fail_if_exists` behavior deterministically.
- `amata validate` accepts `download`.

## Risks
- Large payloads can fail mid-stream and require robust temp cleanup.
- Weak path handling can cause writes outside expected workspace boundaries.

## References
- `~/@iw2rmb/amata/internal/runtime/builtins.go`
- `~/@iw2rmb/amata/internal/executor/dataget/dataget.go`
