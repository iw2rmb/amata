# Updating Documentation Policy

1. When committing:
  - Ensure docs in docs/** are updated corresponding to changes.
2. In `design/**` and `roadmap/**`, references to other design docs or roadmaps are allowed only for not-yet-implemented prerequisites. Do not use completed transient docs as long-lived explanations of current behavior.
3. When updating documents in `docs/**`:
  - Explanations of shipped behavior, schemas, standards, instructions, difficult algorithms, decisions, and principles belong in `docs/**`. When design or roadmap text needs to explain current implementation, point to `docs/**`, not to completed design or roadmap history.
  - `docs/**` is the long-lived, self-sufficient documentation surface. No referring to `design/**`, `research/**`, or `roadmap/**` is allowed.
  - `docs/**` must not repeat design history. It must capture structured current-state snapshots of features, subjects, schemas, instructions, standards, and important implementation principles, relying on the codebase and code comments for low-level detail.
4. Keep documents short, simple, focused, and within their domains. If a document drifts into mixed state, split it for consistency.
5. Keep documents cross-referenced. For cross-reference integrity checks, run `~/@iw2rmb/amata/scripts/check_docs_links.sh` from the target project root.
