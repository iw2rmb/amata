# Implement Roadmap Dagu workflow

Dagu Docs: https://docs.dagu.sh/reference/cli

Workflow **MUST** base on:
  - Roadmap Implementation policy at .codex/policies/composing-and-implementing-roadmaps.md
  - Documentation Handling policy at .codex/AGENTS.md

Roadmap is a file based on `ROADMAP.md` template. 
See examples in ../{ploy|aster|flourish|spok}/roadmap/**

## Runtime helpers

Workflow shell helpers live in `./scripts/` and are implemented in bash:
- `next-open-item.sh`
- `remaining-open-items.sh`
- `write-queue.sh`
- `next-queue-item.sh`
- `mark-queue-item-done.sh`
- `queue-has-more.sh`
- `commit-if-changed.sh`
- `run-codex-prompt.sh`
- `run-claude-prompt.sh`
- `implement-open-items-loop.sh`
- `fix-queue.sh`
- `correctness-phase.sh`
- `refactor-phase.sh`
- `update-docs-phase.sh`

Control flow lives in the bash helpers, not in Dagu JSON output expressions.
Dagu only sequences the coarse phases.
This avoids silent skips caused by Dagu treating step outputs as raw stdout strings in interpolation.

AI execution is explicit:
- Codex steps run through `codex exec` so model and reasoning are set per step.
- Codex prompt runs persist prompts, logs, session IDs, and final outputs under `${STATE_DIR}/codex-runs/` when a state dir is available.
- Codex prompt runs watch for session/log inactivity, kill a wedged `codex exec`, and resume the same session once before failing with artifact paths.
- Claude steps run through `claude -p` so the workflow does not depend on Dagu's generic `type: agent` abstraction.
- Commit steps exclude the workflow `STATE_DIR` so `.amata/` queue files never leak into repository commits.

Codex watchdog tuning:
- `CODEX_WATCHDOG_IDLE_SECONDS` sets the inactivity threshold before recovery. Default: `900`.
- `CODEX_WATCHDOG_POLL_SECONDS` sets how often the watchdog checks activity. Default: `15`.
- `CODEX_WATCHDOG_MAX_RECOVERIES` sets how many resume attempts are allowed before the wrapper fails. Default: `1`.

Runtime prerequisites:
- `codex` CLI installed and authenticated
- `claude` CLI installed and authenticated
- `dagu` CLI installed

Example run:

```bash
dagu start dagu/implement-roadmap/implement-roadmap.yaml -- \
  ROADMAP_FILE=roadmap/my-feature/index.md \
  CODEX_MODEL=gpt-5.4 \
  CLAUDE_MODEL=sonnet
```


## For every item in roadmap, sequentially

- implement:
  - call codex agent with gpt-5.4 with reasoning defined in item or high as default
    - prompt: |
        implement next open item from the <roadmap-file-path>
        mark item as done
        respond with:
          - proposed commit message text
          - estimated reasoning for review, based on the complexity of the task
- review:
  - call codex agent with gpt-5.4 with reasoning from the implementation step response
    - review DIFF and address findings
- commit
  - call shell command to commit using message from `implement`


## After all tasks are complete

- review all:
  - call codex agent with gpt-5.4 with reasoning xhigh
    - prompt: confirm by inspecting codebase that everything from the roadmap is wired, implemented correctly and in full, leaving no leftovers.
    - respond with list of issues to solve and reasoning required
  - for each item:
    - fix gap:
      - call codex agent with gpt-5.4 with reasoning from `review all` for that item
      - close by agent with corresponding reasoning and respond with commit message
    - commit
      - call shell command to commit with message from `fix gap`


## Refactoring

- refactor:
  - call claude
    - prompt: |
        review codebase related to implemented roadmap items
        for redundancy, overengineering, dead code;
        options to simplify algorithms, reduce boilerplate;
        considerations to split files with mixed domains or high LOC number.
    - respond with list of refactoring targets and reasoning required
  - for each item:
    - implement refactor:
      - call claude with reasoning from `refactor` for that item
      - apply the targeted refactor, run relevant checks, and respond with commit message plus summary
    - review diff:
      - call codex agent with gpt-5.4 with reasoning from `refactor` for that item
      - validate the actual uncommitted diff for sanity using claude summary as context, patch findings if needed, and approve the result
    - commit
      - call shell command to commit with message from claude's apply step


## Updating docs

- docs:
  - call codex agent with gpt-5.4 with reasoning `high`
    - prompt: |
      - delete completed roadmap and it's design doc if there are no refs in upcoming DDs/roadmaps;
      - ensure that corresponding docs updated with taking into account that this DD is completed.
  - commit:
    - call shell command to commit with a `cleanup` message
