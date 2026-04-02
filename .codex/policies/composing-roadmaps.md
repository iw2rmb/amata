# Composing Roadmaps Policy

Roadmap is a sequence of independently shippable implementation steps.

Working on roadmap or step:
- Follow **Deciding On Architecture** policy: `~/.codex/policies/deciding-on-architecture.md`
- Follow **Estimating Effort** policy: `~/.codex/policies/estimating-effort.md`.
- Compose roadmap as a `YAML` file that follows `.codex/policies/roadmap.schema.json` JSON schema; call `yamllint <roadmap-yaml-file>` to validate.
- Put new roadmaps in `roadmap/` folder; when there are milestones (phases) implied by DD with multiple steps per milestone (phase), follow the structure:
  - `roadmap/{subject}/{mile|phase}-{n}-{scope}.yaml` per milestone
  - `roadmap/{subject}/index.md` to state common ground and a `[ ]` list of miles/phases listing
- Do not add any new sections that are not expected by template.
