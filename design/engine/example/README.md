# Engine Example Bundle

This folder is a self-contained reference bundle for the engine contract in [../engine.md](../engine.md).

It contains:
- `implement-roadmap.yaml`
  - the workflow spec
- `plugins.yaml`
  - a concrete plugin registry example for custom step types and plugin config schemas
- `sdk/`
  - small language-specific helpers for the engine/plugin process contract
- `scripts/`
  - executable plugin implementations and a fixture bootstrap script
- `fixture-repo/`
  - a minimal repository layout that the workflow can target

Built-in runtime assumptions from the engine design:
- `shell`
- `codex`
- `claude`

Plugin steps in this example are wired through `plugins.yaml`:
- `git.inspect`
- `git.commit`

Example operator flow:
1. Bootstrap the fixture repository:
   `sh design/engine/example/scripts/init_fixture_repo.sh`
2. Load the plugin registry from `design/engine/example/plugins.yaml`.
3. Run `design/engine/example/implement-roadmap.yaml`.

Notes:
- `plugins.yaml` is a concrete example of registry wiring, not a normative part of the engine spec.
- In the target Go engine, `git.inspect` and `git.commit` should be engine-owned standard executors backed by the typed Git layer. Their script wiring here is transitional and exists to demonstrate the external plugin protocol.
- `plugins.yaml` shows plugin-side config schemas so the engine can validate config before spawning a script.
- `sdk/python.py` contains only protocol-level helpers shared across plugins.
- Script paths in `plugins.yaml` are resolved relative to the registry file.
- Repo-facing relative paths in `implement-roadmap.yaml` resolve from `workspace.root`.
- The workflow carries data forward through `ctx.prev` instead of referencing earlier steps by `id`.
- Codex picks the next open roadmap item directly from the roadmap file in this first-version example.
- `git.inspect` is a shell-backed plugin that reports `isRepo`, `hasDiff`, and `files` from one repo snapshot.
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
