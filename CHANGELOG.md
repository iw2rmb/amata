# Changelog

## 2025-10-04
- docs/AGENTS.md: Retired the plan document layer, streamlined workflow guidance to point at the design/task templates, and validated cross-references against `templates/design/README.md` and `templates/tasks/README.md` (reviewed 2025-10-04).
- docs/README.md: Updated documentation structure to surface the task queue and removed plan references; confirmed alignment with the relocation to `templates/tasks/INDEX.md` (reviewed 2025-10-04).
- templates/tasks/INDEX.md: Relocated the dependency-ordered queue from `docs/tasks/README.md` so it can be reused as a template; verified content matches the previous ledger (reviewed 2025-10-04).
- docs/templates/design/*: Removed plan dependencies, retargeted task links to `docs/tasks`, and cross-checked component, integration, program, and resiliency templates (reviewed 2025-10-04).
- docs/templates/tasks/*: Stripped parent plan metadata, updated sample paths to `docs/tasks`, and refreshed references after moving the queue template (reviewed 2025-10-04).
- templates/plans/*: Deleted superseded plan templates after confirming no remaining references in guidance (reviewed 2025-10-04).

## 2025-10-01
- docs/AGENTS.md: Replaced the `.inprogress.md` workflow with the dependency-ordered queue in `docs/plans/README.md`; ran `rm .inprogress.md`, `mkdir -p plans`, and `cat > plans/README.md` to establish the new ledger, then refreshed the coordination rules to enforce reading the latest file state before every edit.

## 2025-09-30
- docs/AGENTS.md: Reviewed `AGENTS.md`, `templates/design/README.md`, `templates/tasks/README.md`, and `templates/plans/README.md` to align dependency, naming, and parallelisation guidance; cross-checked launch, deprecation, and security plan templates for the new sections.
- docs/templates/plans/launch-readiness-plan.md: Added feature scoreboard, dependency map, unblocking backlog, and parallelisation strategy sections.
- docs/templates/plans/README.md: Enhanced plan guidance with feature scoreboards, dependency maps, and unblocking backlogs; verified launch, deprecation, and security plan templates include the new structures.
- docs/templates/plans/service-deprecation-plan.md: Added feature scoreboard, dependency mapping, and parallelisation scaffolding per updated plan guidance.
- docs/templates/design/README.md: Updated template guidance for dependency metadata, parallelisation snapshots, and unblocking work alignment; verified `component-design-spec.md`, `integration-alignment-spec.md`, `program-epic-blueprint.md`, and `resiliency-hardening-plan.md` include the new sections.
- docs/templates/tasks/README.md: Refreshed roadmap task guidance to emphasise dependency metadata, small testable slices, and parallelisation notes; checked implementation, validation, and documentation task templates for the new sections.
- docs/templates/plans/security-hardening-playbook.md: Added feature scoreboard, dependency map, unblocking backlog, and parallelisation strategy sections per updated plan guidance.
- docs/AGENTS.md: Reviewed `AGENTS.md`, `templates/design/README.md`, `templates/tasks/README.md`, and `templates/plans/README.md` to confirm workflow, decomposition, and template guidance remain consistent.
- docs/templates/plans/launch-readiness-plan.md: Template structure established — aligned with plan guidance in `AGENTS.md`; verify against implementation/docs before marking complete. launch readiness plan against implementation/docs.
- docs/templates/plans/README.md: Created `templates/plans/README.md` and initial plan templates aligned with `AGENTS.md` workflow and decomposition guidance.
- docs/templates/plans/service-deprecation-plan.md: Template structure established — aligned with plan guidance in `AGENTS.md`; verify against implementation/docs before marking complete.
- docs/templates/design/README.md: Inspected `AGENTS.md` and `templates/design/README.md` to ensure instructions on template selection, decomposition, deviations, and new template proposals align.
- docs/templates/tasks/README.md: Cross-checked `AGENTS.md`, `templates/design/README.md`, and `templates/tasks/README.md` for consistent roadmap workflow guidance and new template escalation paths.
- docs/templates/plans/security-hardening-playbook.md: Template structure established — aligned with plan guidance in `AGENTS.md`; verify against implementation/docs before marking complete.
