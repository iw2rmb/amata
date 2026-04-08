# Estimating Effort

This doc defines implementation effort estimation in `CFP` (`COSMIC Functional Points`).
`CFP` is useful only when scope, boundary, and granularity are consistent.
Follow this checklist to size a feature or roadmap item in `CFP`.


## Clarify inputs

Clarify purpose, scope, layer(s), functional users, boundary, triggering events, objects of interest, data groups, external interfaces, and error or confirmation messaging by inspecting docs and code.
If unknown, state assumptions explicitly.


## Fix granularity

Measure at the functional-process level.
One functional process has one triggering `Entry`.


## Map the work

Identify functional users and triggering events, then derive functional processes.
Use the same decomposition level across all compared processes.


## Model data

List objects of interest and group attributes into data groups.
Rule: different frequency or different key(s) means different data groups.
Model both persisted and boundary-crossing transient data groups if they participate in counted movements.


## Identify data movements per process

Use `COSMIC` movement types: `Entry` (E), `Exit` (X), `Read` (R), `Write` (W).
Each distinct movement counts as `1 CFP`.

Counting rules:
- Count only movements crossing the measured boundary or storage/interface boundary.
- Do not count pure in-memory transformations that do not cross a boundary.
- Ignore UI control commands unless they carry business data groups across the measured boundary.
- Count one `Exit` for all error or confirmation messaging per process.
- A valid process has at least one `Entry` and at least one `Exit` or `Write`.


## Aggregate sizes

`CFP_total = Σ(E) + Σ(X) + Σ(R) + Σ(W)`.
Aggregate only processes inside the defined scope and at comparable granularity.
Do not double-count the same movement across phases or items.


## Changes (enhancements)

`CFP` is implementation effort of the enhancement.
Estimate enhancement delta from before and after process maps:
- Added movement: `+1`.
- Deleted movement: `+1` in change effort.
- Modified movement: count as delete plus add (`+2`) unless the change is only naming with no boundary, data group, or movement-type change.


## Underestimation checks

Apply these checks before finalizing `CFP`:
- Count per execution path, not per feature. If behavior is added to multiple stages, cycles, or modes, count each path unless it is the same functional process.
- Count integration boundaries explicitly. Include contract validation, scheduling/orchestration, completion/reporting, and status handling when they cross boundaries.
- Count required file or artifact inputs/outputs as boundary data movements.
- Include change effort for required non-regression and deterministic-order verification when behavior is replicated across paths.

Required final sanity check:
- Did I count all execution paths?
- Did I count all integration boundaries?
- Did I count required data inputs/outputs?
- Should this item be split before sizing?


## Map CFP to roadmap reasoning

When a roadmap item needs `Reasoning: low|medium|high|xhigh`, map by `CFP`:

| CFP_delta | Reasoning |
|-----------|-----------|
| 1-3       | low       |
| 4-8       | medium    |
| 9-16      | high      |
| 17+       | xhigh     |


## Output contract

Always provide at least:
- mapped `Reasoning`,
- assumptions and open questions.

If explicitly asked for evidence, provide this table:

```md
| Functional process | E | X | R | W | CFP |
|--------------------|---|---|---|---|-----|
| <name>             | # | # | # | # | sum |
| ...                |   |   |   |   |     |
| TOTAL              |   |   |   |   | N   |

<delta summary, if enhancement>
<short assumptions note>
<open questions, if any>
```


## Pseudocode aid (when text is unclear)

If future code is unclear, draft minimal pseudocode that makes `E/X/R/W` explicit, then count from it.
`COSMIC` permits sizing from available artifacts and design models.

```yaml
process <name>:
  trigger: Entry(<data group from <functional user>>)
  reads:   Read(<data group[s] from storage or external interface>)
  writes:  Write(<data group[s] to storage or external interface>)
  outputs: Exit(<data group[s] to <functional user>>)
  outputs: Exit(errors)   # one Exit covers all error/confirmation messages
```
