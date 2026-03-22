# Deciding on Architecture

When working on roadmaps or design docs, apply these rules and enrich tasks/goals with concrete actions needed to satisfy them.

Avoid mixing domains, over-complexity, and ambiguity:
- Keep file and module domains distinct.
- Keep file, variable, and module names aligned with their domains.
- Prevent domain extension of the file or module without clear benefit.
- Split files above 500 LOC and functions above 100 LOC unless there is a clear reason not to.

Avoid race conditions by:
- deterministic execution order,
- execution independence.

Prefer architecture-wide solutions over time-saving band-aids.
