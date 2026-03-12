# Roadmap Policy

## Composing

- Before composing roadmap, you **MUST** inspect corresponding codebase and docs.
- Create roadmap file in `roadmap/` folder. When having milestones (phases) follow the structure:
  - `roadmap/{subject}/{mile|phase}-{n}-{scope}.md` per milestone.
  - `roadmap/{subject}/index.md` to state common ground and a `[ ]` list of miles/phases listing.
- **STRICTLY** follow `~/@iw2rmb/auto/ROADMAP.md` template.
- Provide imlementation steps with plain and clear instructions, code snippets/references.

## Implementation

- Work **ONE** open item at a time.
  - If that item is too large to complete safely in one pass, decompose that item into smaller ones, then update plan and move to the first open item.
- For each next open item in that document:
  - Implement it.
  - Call Codex agent to review the resulting diff for architectural sanity, correctness, completness; check for redundancy, overengineering, options to simplify; address findings if any.
  - Mark the item complete and replace it's content with a summarized key takeaways over implementation; commit.
- After completing all items:
  - Order Codex subagent with `gpt-5.4` model and `xhigh` reasoning to confirm by inspecting the codebase that every step from the roadmap is implemented correctly and in full; wired end-to-end; there are no leftovers.
  - Address gaps, if any.
  - Remove the transient design/roadmap pair if the shipped docs now stand on their own.
  - Commit.
