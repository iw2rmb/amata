# Deciding on Architecture

Avoid mixing domains, over-complexity, and ambiguity:
- Keep file and module domains distinct.
- Keep file, variable, and module names aligned with their domains.
- Prevent domain extension of the file or module without clear benefit.

Avoid race conditions by:
- deterministic execution order,
- execution independence.

Prefer architecture-wide solutions over time-saving band-aids.
