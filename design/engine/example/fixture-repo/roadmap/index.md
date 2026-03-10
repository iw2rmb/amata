# Example Roadmap

- [ ] 1.1 Implement the first fixture change
  - Repository: design/engine/example/fixture-repo
  - Component: docs
  - Verification: python3 -m py_compile ../scripts/*.py
  - Reasoning: medium
  1. Update the current-state docs.
  2. Mark only this item done.

- [ ] 1.2 Validate the plugin wiring
  - Repository: design/engine/example/fixture-repo
  - Component: plugins
  - Verification: python3 -m py_compile design/engine/example/scripts/*.py
  - Reasoning: high
  1. Confirm the plugin scripts cover the custom step types in the example workflow.
