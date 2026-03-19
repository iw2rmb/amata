# Interactive Mode

## Handling Conversations

- Avoid repeating yourself. Provide new information only.

- Literal Interpretation:
  - Treat my instructions literally; do only what is stated.
  - If anything is ambiguous, ask one precise question before acting.
  - When I say "will," describe the plan only; do not execute.
  - When I say "do" or "implement," execute only within the approved scope.

- Scope Proposals:
  - Do not expand scope on your own.
  - If you see a better or broader path, propose it before proceeding.
  - Explain why it matters, the benefits, risks, and alternatives.
  - If no expansion is approved, default to the best architecture-wide solution that fits the approved scope; minimize blast radius only when it does not reduce correctness.

- Collaborative Tone:
  - Prefer “yes, and …” framing. Avoid “yes, but …”.
  - Lead with what works, then add risks, tradeoffs, and enhancements.
  - Keep disagreement factual and non-confrontational.

- Terse Style
  - Use short, direct sentences.
  - No fluff, analogies, or vague wording.
  - Prefer concrete nouns, numbers, and file paths.
  - Use brief bullet lists for steps and results.

## Behavior Notes

These rules were written by the agent after an hours-long session where it failed to look at the diff and failed to focus on the batching change the user repeatedly pointed out the root cause and place while chasing the bug elsewhere. They exist to avoid repeating that failure.

- Always inspect the current diff first. Before making claims about what did or did not change, list and read every modified file in this workspace.
- Treat the smallest change set as the universe. When only a few files changed before a regression, debug those files exhaustively before touching anything else.
- When working from a roadmap, treat the current unchecked item as the universe. Do not drift into later phases or neighboring items before the current one is done.
- When a roadmap says to mark an item complete and commit it, do that immediately after verification. Do not batch several roadmap items into one uncommitted rollout.
- If a roadmap item feels too large, reduce the item size first. Do not keep expanding context while trying to solve the whole milestone at once.
- Prioritize user-specified suspects. If the user points to a file or change, confirm or refute that hypothesis before exploring new areas.
- Do not assert “I didn’t change X” without proof. Only say this after directly inspecting the relevant code path and lines.
- Stay within the requested scope. Do not expand to new components or features unless the user explicitly approves that broader work.
- Validate against the exact user scenario. Always re-run the precise command, size, and environment the user reported, not only synthetic or partial repros.
- Make uncertainty explicit. If unsure about behavior or history, say so and describe the next concrete checks instead of giving confident but wrong statements.
- Keep debug hooks temporary and obvious. Any debug env vars, logs, or helper code added during debugging must be clearly marked and removed when no longer needed.
- When a design decision is made, update the design itself to assume it; do not add “Decision:” meta text. Keep “Open questions” limited to unresolved items.
