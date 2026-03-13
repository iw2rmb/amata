# Engine Example Bundle

This folder is a self-contained reference bundle for the shipped engine contract in [Engine docs](../../../docs/engine/index.md).

It contains:
- `implement-roadmap.yaml`
  - the workflow spec
- `scripts/`
  - roadmap helper scripts
- `fixture-repo/`
  - a sample target tree inside the host repository

Built-in runtime assumptions from the shipped engine contract:
- `shell`
- `codex`
- `claude`
- `git.inspect`
- `git.commit`

Example operator flow:
1. Run `amata run design/engine/example/implement-roadmap.yaml --workspace <repo> --set roadmap_file=<roadmap.md>` from any checkout that contains this bundle.

Notes:
- `git.inspect` and `git.commit` are engine-owned standard executors backed by the typed Git layer.
- The reference `implement-roadmap` scenario uses only built-in executors plus the roadmap helper scripts. It does not need a plugin registry or any Python Git adapters.
- The example workflow uses `call`, `switch`, response schemas, and `ctx.prev` for the per-item loop, review, and final phases. There is no external routing script.
- Repo-facing relative paths in `implement-roadmap.yaml` resolve from `workspace.root`.
- Helper script paths resolve from `ctx.spec.dir`, so the workflow bundle can target a different repository through `--workspace` without extra `scripts_dir` parameters.
- Launch-time inputs such as `roadmap_file`, `codex_model`, or `claude_model` are overridden through repeated `--set key=value` flags.
- `roadmap_file` accepts repo-relative paths, absolute paths, and `~/...` home-relative paths.
- The default `roadmap_file` still points at the sample roadmap under `design/engine/example/fixture-repo/`.
- The active roadmap helper contract is the direct CLI in `scripts/roadmap_items.py`.

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
