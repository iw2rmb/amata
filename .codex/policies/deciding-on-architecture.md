# Deciding on Architecture

When working on roadmaps or design docs, take these rules into account and enrich tasks/goals with actions required to align to these criterias.

Avoid mixing domains, overcomplexity, and ambigouty:
- Keep files' and modules' domains distinctive.
- Keep file, var, and module names to correspond their domains.
- Prevent domain extension of the file or module without clear benefit.
- 500+ LOC files and 100+ LOC functions consider to split.

Avoid race conditions by:
- execution order determinism,
- execution independence.

Prefer architecture-wide solutions over time-saving band-aids.
