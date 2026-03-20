# Updating Documentation Policy

1. When committing, you **MUST** ensure that corresponding docs updated accordingly with diff taken into account.
2. When updating `design/**` and `roadmap/**`,
  - `design/**` and `roadmap/**` are short-lived working documents. Once the corresponding work is implemented and no unfinished design or roadmap depends on them as prerequisites, remove both the design doc and its roadmap.
  - In `design/**` and `roadmap/**`, references to other design docs or roadmaps are allowed only for not-yet-implemented prerequisites. Do not use completed transient docs as long-lived explanations of current behavior.
3. These rules are applied to `docs/**` updates:
  - Explanations of shipped behavior, schemas, standards, instructions, difficult algorithms, decisions, and principles belong in `docs/**`. When design or roadmap text needs to explain current implementation, point to `docs/**`, not to completed design or roadmap history.
  - `docs/**` is the long-lived, self-sufficient documentation surface. It must not refer to `design/**`, `research/**`, or `roadmap/**`.
  - `docs/**` should not repeat design history. It should capture structured current-state snapshots of features, subjects, schemas, instructions, standards, and important implementation principles, relying on the codebase and code comments for low-level detail.
4. In general, when updating docs, 
  - Keep them short, simple, focused, and within their domains. If document is drifting into mixed state, split it for consistency.
  - Keep documents cross-referenced. For cross-reference integrity checks, run `~/@iw2rmb/amata/scripts/check_docs_links.sh` from the target project root.
