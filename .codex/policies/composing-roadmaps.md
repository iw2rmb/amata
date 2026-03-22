# Composing Roadmaps Policy

Roadmap is a sequence of independently shippable implementation steps.

Working on roadmap or step:
- Follow **Deciding On Architecture** policy: `~/.codex/policies/deciding-on-architecture.md`
- Follow **Estimating Effort** policy: `~/.codex/policies/estimating-effort.md`
- Before composing, inspect corresponding codebase and docs.
- Plan steps as the smallest independently shippable and verifiable units possible.
- Avoid ambiguity: each step must identify concrete files/modules and expected observable outcomes.
- Use one functional action per numbered implementation line.
- For every item, classify work as either `determined` or `assumption-bound`:
  - `determined`: all involved components/classes/functions/structs are known.
  - `assumption-bound`: unresolved unknowns remain; add an explicit `Assumptions:` block in the item.
- Set `Reasoning` from `CFP_delta` using `estimating-effort.md` mapping.
- For `assumption-bound` items, shift `Reasoning` one level right (`low->medium`, `medium->high`, `high->xhigh`, `xhigh->xhigh`).
- Put new roadmaps in `roadmap/` folder; when there are milestones (phases) implied by DD with multiple steps per milestone (phase), follow the structure:
  - `roadmap/{subject}/{mile|phase}-{n}-{scope}.md` per milestone
  - `roadmap/{subject}/index.md` to state common ground and a `[ ]` list of miles/phases listing
- Ensure roadmap and steps align with this template: `~/@iw2rmb/amata/ROADMAP.md`.
