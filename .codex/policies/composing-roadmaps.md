# Composing Roadmaps Policy

Roadmap is a serie of sequential implementation steps.

Working on roadmap or step:
- Follow **Deciding On Architecture** policy: `~/.codex/policies/deciding-on-architecture.md`
- Before composing, inspect corresponding codebase and docs
- Plan steps to be as smallest independently shippable + verifiable units as possible (e.g. there is no ambigouty/gray areas and it is possible to be specific on details)
- Put new roadmaps in `roadmap/` folder; when there are milestones (phases) implied by DD with multiple steps per milestone (phase), follow the structure:
  - `roadmap/{subject}/{mile|phase}-{n}-{scope}.md` per milestone
  - `roadmap/{subject}/index.md` to state common ground and a `[ ]` list of miles/phases listing
- Ensure roadmap and steps align with this template: `~/@iw2rmb/amata/ROADMAP.md`
