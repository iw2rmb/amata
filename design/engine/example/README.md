# Engine Example Bundle

This folder is a self-contained reference bundle for the engine contract in [../engine.md](../engine.md).

It contains:
- `implement-roadmap.yaml`
  - the workflow spec
- `sdk/`
  - shared helpers for the remaining example scripts
- `scripts/`
  - roadmap helper scripts
- `fixture-repo/`
  - a sample target tree inside the host repository

Built-in runtime assumptions from the engine design:
- `shell`
- `codex`
- `claude`
- `git.inspect`
- `git.commit`

Example operator flow:
1. Run `design/engine/example/implement-roadmap.yaml` from an existing Git-managed repository checkout.

Notes:
- In the target Go engine, `git.inspect` and `git.commit` are engine-owned standard executors backed by the typed Git layer.
- The reference `implement-roadmap` scenario uses only built-in executors plus the roadmap helper scripts. It does not need a plugin registry or any Python Git adapters.
- `sdk/python.py` remains because the roadmap helper scripts import the shared request/result helpers.
- The example `implement-roadmap.yaml` resolves `workspace.root` to the host repository root and targets the sample roadmap file under `design/engine/example/fixture-repo/`.
- Repo-facing relative paths in `implement-roadmap.yaml` resolve from `workspace.root`.
- The workflow carries data forward through `ctx.prev` instead of referencing earlier steps by `id`.
- Codex picks the next open roadmap item directly from the roadmap file in this first-version example.
- `git.inspect` is a standard Git executor that reports `isRepo`, `hasDiff`, and `files` from one repo snapshot.
- `implement-roadmap.yaml` should not need structural changes when those Git step types move into the Go engine. The workflow contract stays the same; only the executor implementation boundary changes.

Minimal `git.inspect` usage:

```yaml
schemas:
  git_inspect_result:
    type: object
    required: [isRepo, hasDiff, files]
    additionalProperties: false
    properties:
      isRepo: boolean
      hasDiff: boolean
      files:
        type: array
        items: string

flows:
  inspect:
    steps:
      - type: git.inspect
        response:
          schema:
            $ref: "#/schemas/git_inspect_result"
      - assert: $.prev.value["isRepo"]
      - expr: $.prev.value["files"]
```
