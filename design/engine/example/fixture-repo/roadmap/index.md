# Example Roadmap

- [ ] 1.1 Implement the first fixture change
  - Repository: design/engine/example/fixture-repo
  - Component: docs
  - Verification: python3 -m py_compile ../scripts/*.py
  - Reasoning: medium
  1. Update the current-state docs.
  2. Mark only this item done.

- [ ] 1.2 Validate built-in executor wiring
  - Repository: design/engine/example/fixture-repo
  - Component: workflow
  - Verification: python3 -m py_compile design/engine/example/scripts/*.py
  - Reasoning: high
  1. Confirm the built-in engine steps cover the example workflow without plugin scripts.
