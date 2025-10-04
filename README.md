# Spec-Driven LLM Development Docs

This repository curates the documentation system for a spec-driven software delivery workflow that relies on large language models (LLMs) to execute implementation tasks. It anchors every change in authored specifications so automated agents and human reviewers can collaborate with clear intent and traceable decisions.

## Structure
- `docs/design/<feature>/README.md` — feature design dossiers covering intent, architecture, risks, tests, and dependency maps derived from neighbouring work.
- `docs/tasks/<feature>/<n>-<stage>.md` — task specs that translate designs into executable, verifiable work packages with status, definition of done, and parallelisation notes.
- `templates/tasks/INDEX.md` — dependency-ordered task queue template showing what is ready to pick up next.
- `templates/` — scaffolding for designs and tasks to keep metadata, dependency mirroring, and entries in `docs/CHANGELOG.md` consistent.

## Workflow Highlights
1. Author or update design documentation before implementation, explicitly linking upstream features and downstream tasks.
2. Derive task specs from the design, ensuring each entry documents blocking risks, unblocks, and required verification.
3. Begin implementation only after the relevant failing tests or snapshots are committed, updating design artefacts with verification evidence as work progresses.

The combination of structured specs and LLM-assisted execution emphasises traceability, disciplined sequencing, and shared context so teams can scale delivery while maintaining safety and auditability.
