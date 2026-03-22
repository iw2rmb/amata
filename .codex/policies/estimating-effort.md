# Estimating Effort

This doc describes methodology of implementation effort estimation in `CFP` (`COSMIC Functional Points`). 
The benefit of this metric is that it refletcts linear dependency between `CFP` and effort required.
Follow this checklist to size a feature in `CFP`.


## Clarify Inputs

Clarify purpose, measurement scope and layer(s), functional users, boundary, persistent storage, required granularity, triggering events, objects of interest and data groups, external interfaces, and error/confirmation messaging by inspecting corresponding docs and codebase. If unknown, state assumptions.


## Fix granularity

Measure at the functional process level of granularity.


## Map the work

Identify functional users and a simple context. Derive functional processes from triggering events. One process has one triggering Entry.


## Model data

List objects of interest. Group attributes into data groups. Rule: different frequency or different key(s) ⇒ different data groups.


## Identify data movements per process

Use `COSMIC` types: `Entry` (E), `Exit` (X), `Read` (R), `Write` (W). 
Each distinct movement = 1 `CFP`. 
Ignore UI control commands. 
Count one `Exit` for all error/confirmation messages per process. 
A process has ≥1 `Entry` and either an `Exit` or a `Write`.


## Aggregate sizes

`CFP` = Σ(E)+Σ(X)+Σ(R)+Σ(W). Sum processes only within the defined scope and at comparable decomposition and granularity.


## Changes

For enhancements, count added, modified, and deleted data movements; then aggregate per rules.


## Output format

If explicitly requested to provide estimation evidence, follow this format:

```md
| Functional process | E | X | R | W | CFP |
|--------------------|---|---|---|---|-----|
| <name>             | # | # | # | # | sum |
| ...                |   |   |   |   |     |
| TOTAL              |   |   |   |   | N   |

<short note of assumptions>

<open questions if there are any>
```


## Pseudocode aid (use when text is unclear)

If the future code is unclear, draft minimal pseudocode that makes `Entries`/`Exits`/`Reads`/`Writes` explicit, then count from it. 
`COSMIC` permits sizing from available artefacts and design models.

```yaml
process <name>:
  trigger: Entry(<data group from <functional user>>)
  reads:   Read(<data group[s] from persistent storage>)
  writes:  Write(<data group[s] to persistent storage>)
  outputs: Exit(<data group[s] to <functional user>>)
  outputs: Exit(errors)   # one Exit covers all error/confirmation messages
```


## Reference

- [Glossary](https://cosmic-sizing.org/cosmic-sizing/cosmic-glossary/)
- [Methodology](https://cosmic-sizing.org/wp-content/uploads/2020/08/Measuring-software-size-v1.0-August-2020-1.pdf)
- [Estimation](https://cosmic-sizing.org/cosmic-sizing/estimating-with-software-size/)
