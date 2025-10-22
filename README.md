# Spec-Driven LLM Development Docs

This repository curates the documentation system for a spec-driven software delivery workflow that relies on large language models (LLMs) to execute implementation slices. Design docs are the single source of intent so automated agents and human reviewers can collaborate with clear decisions and traceable evidence.

## Structure
- `docs/design/<subject>/README.md` — compact design dossiers covering why the change exists, what must happen, where edits land, and how verification succeeds.
- `docs/design/QUEUE.md` — dependency-ordered checklist showing which design docs are ready to pull next.
- `design/TEMPLATE.md` — the only template used to author new design docs.
- `.archive/<subject>/` — storage for fully verified design docs moved out of the active queue.
- `templates/` — scaffolding for design docs and supportive guidance to keep metadata, queue entries, and `CHANGELOG.md` updates consistent.

## Workflow Highlights
1. Author or update the design document before implementation, explicitly linking upstream artefacts and noting verification evidence.
2. Update `docs/design/QUEUE.md` so others can reserve the doc; keep the queue ordered by readiness and dependencies.
3. Begin implementation only after the relevant failing tests or snapshots are committed, updating the design doc with verification evidence as work progresses.
4. Once verification is complete, move the design doc folder from `docs/design/<subject>` into `.archive/<subject>` to retire it from active planning.

The combination of structured specs and LLM-assisted execution emphasises traceability, disciplined sequencing, and shared context so teams can scale delivery while maintaining safety and auditability.
