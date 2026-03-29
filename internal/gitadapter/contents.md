[README.md](README.md) Overview of the git adapter package and responsibilities of inspect/commit service files.
[adapter.go](adapter.go) Provides inspect and commit orchestration over repository state, include/exclude path filtering, and typed snapshot/commit results.
[adapter_test.go](adapter_test.go) Integration-style repository tests covering inspect snapshots, scoped commit behavior, and commit metadata edge cases.
[cli.go](cli.go) Implements git CLI helpers for staging paths, committing, diff checks, and collecting commit statistics.
