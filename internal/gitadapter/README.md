# `gitadapter`

Git repository inspection and path-scoped commit adapter used by the `git.inspect` and `git.commit` executors.

- `adapter.go` - Public service API and core inspect/commit flow, including include/exclude path filtering.
- `cli.go` - Git CLI mutation helpers for stage/diff/commit operations and commit metadata extraction.
- `adapter_test.go` - Integration-style adapter tests against temporary real git repositories.
