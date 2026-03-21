# Unified File Scan Spike Phase 2 Candidate-First Symbols Inventory

Scope: Verify and lock the shipped candidate-first file-scan contract so `git.inspect` and `git.commit` consume one deterministic candidate set and expose consistent progress symbols.

Documentation: [Engine](../../docs/engine/index.md)

Legend: [ ] todo, [x] done.

- [x] 2.1 Build one candidate-first repository snapshot
  - Repository: auto
  - Component: `internal/gitadapter`, `internal/executor/gitinspect`
    1. Open the target repository from any nested `cwd` and derive repo root deterministically.
    2. Read one status snapshot and convert it into sorted repo-relative candidate paths including untracked files.
    3. Return typed snapshot fields (`isRepo`, `hasDiff`, `files`) to the `git.inspect` step value.
  - Verification:
    1. `go test ./internal/gitadapter -run 'TestInspectIncludesUntrackedFilesInSingleSnapshot|TestInspectOutsideRepositoryReturnsTypedEmptyResult'`
    2. `go test ./internal/executor/gitinspect -run 'TestExecutorIncludesUntrackedFilesInTypedSnapshot|TestExecutorReturnsTypedSnapshotOutsideRepository'`
  - Reasoning: high

- [x] 2.2 Normalize candidate filtering before commit mutation
  - Repository: auto
  - Component: `internal/gitadapter`, `internal/executor/gitcommit`
    1. Normalize exclude prefixes from relative and absolute paths without allowing repository-root escape.
    2. Apply directory-prefix filtering to the candidate list before staging.
    3. Return typed no-op results when no included paths remain after filtering or no staged diff exists.
  - Verification:
    1. `go test ./internal/gitadapter -run 'TestFilterPathsUsesDirectoryPrefixSemantics|TestCommitExcludesAbsolutePrefixesAndKeepsExcludedStagedChanges'`
    2. `go test ./internal/executor/gitcommit -run 'TestExecutorReturnsTypedNoOpWhenOnlyExcludedStateDirChanged'`
  - Reasoning: high

- [x] 2.3 Wire commit/inspect inventory through runtime and progress symbols
  - Repository: auto
  - Component: `internal/runtime`, `internal/spec`, `internal/progress`
    1. Append `workspace.state_dir` to commit exclusions and expose typed commit payload fields (`committed`, `commit`, `paths`, `repoRoot`, `metadata`).
    2. Register `git.inspect` and `git.commit` in built-in runtime executor wiring and embedded step-schema validation.
    3. Render stable `git.inspect` and `git.commit` progress descriptor summaries from typed result values.
  - Verification:
    1. `go test ./internal/executor/gitcommit -run 'TestExecutorAddsWorkspaceStateDirToExclusions|TestExecutorCommitsUntrackedFilesAndPreservesExcludedPaths'`
    2. `go test ./internal/runtime -run 'TestBuiltinRegistration|TestRunnerLiveProgressIncludesGitCommitCompletedLineSummary'`
    3. `go test ./internal/spec -run 'TestLoadAcceptsEmbeddedBuiltInStepSchemas|TestLoadRejectsInvalidBuiltInStepSchemas'`
  - Reasoning: high

- [x] 2.4 Keep docs contract aligned to shipped behavior
  - Repository: auto
  - Component: `docs/engine`
    1. Document single-snapshot candidate sourcing for `git.inspect`/`git.commit`.
    2. Document default `workspace.state_dir` exclusion and path-scoped commit behavior.
    3. Document committed-step progress summary details based on structured metadata.
  - Verification:
    1. `go test ./internal/runtime -run 'TestRunnerLiveProgressIncludesGitCommitCompletedLineSummary'`
    2. `go test ./internal/progress -run 'TestStepDescriptorShapes'`
  - Reasoning: medium

## Verification Mapping (rerun required by acceptance criteria)

Rerun status (2026-03-21): passed.
- Command:
  - `go test ./internal/gitadapter ./internal/executor/gitinspect ./internal/executor/gitcommit ./internal/runtime ./internal/spec ./internal/progress`
  - `./scripts/check_docs_links.sh`
- Result:
  - `ok` for all listed Go packages and `check_docs_links: cross-reference integrity passed`.

### 2.1 Candidate-first repository snapshot
- Code paths:
  - `internal/gitadapter/adapter.go`: `Inspect`, `changedPaths`, `openRepository`
  - `internal/executor/gitinspect/gitinspect.go`: `Execute`
- Tests:
  - `internal/gitadapter/adapter_test.go`: `TestInspectIncludesUntrackedFilesInSingleSnapshot`, `TestInspectOutsideRepositoryReturnsTypedEmptyResult`
  - `internal/executor/gitinspect/gitinspect_test.go`: `TestExecutorIncludesUntrackedFilesInTypedSnapshot`, `TestExecutorReturnsTypedSnapshotOutsideRepository`
- Wiring evidence:
  - `internal/runtime/builtins.go`: `git.inspect` registration
  - `schemas/git.inspect.amata.schema.json`: built-in step schema and default value schema binding

### 2.2 Candidate filtering and exclusion normalization
- Code paths:
  - `internal/gitadapter/adapter.go`: `filterPaths`, `normalizeExcludePrefixes`, `normalizeExcludePrefix`, `normalizeRepoRelativePath`, `isExcludedPath`, `Commit`
  - `internal/executor/gitcommit/gitcommit.go`: `resolveExcludePaths`, `Execute`
- Tests:
  - `internal/gitadapter/adapter_test.go`: `TestFilterPathsUsesDirectoryPrefixSemantics`, `TestCommitExcludesAbsolutePrefixesAndKeepsExcludedStagedChanges`
  - `internal/executor/gitcommit/gitcommit_test.go`: `TestExecutorReturnsTypedNoOpWhenOnlyExcludedStateDirChanged`
- Wiring evidence:
  - `internal/gitadapter/cli.go`: path-scoped `git add -A -- <paths>`, `git diff --cached -- <paths>`, `git commit -- <paths>`
  - `docs/engine/index.md`: candidate-path sourcing and scoped staging contract in `git.commit` behavior

### 2.3 Runtime and progress symbol wiring
- Code paths:
  - `internal/executor/gitcommit/gitcommit.go`: state-dir exclusion append and typed output payload
  - `internal/runtime/builtins.go`: runtime builtin registration
  - `internal/spec/step_schemas.go`: built-in schema compile list includes `git.inspect` and `git.commit`
  - `internal/progress/descriptor.go`: `gitInspectDescriptorFromResult`, `gitCommitDescriptorFromResult`, status-symbol mapping
- Tests:
  - `internal/executor/gitcommit/gitcommit_test.go`: `TestExecutorAddsWorkspaceStateDirToExclusions`, `TestExecutorCommitsUntrackedFilesAndPreservesExcludedPaths`
  - `internal/runtime/runner_test.go`: `TestBuiltinRegistration`
  - `internal/runtime/runner_progress_test.go`: `TestRunnerLiveProgressIncludesGitCommitCompletedLineSummary`
  - `internal/spec/spec_test.go`: `TestLoadAcceptsEmbeddedBuiltInStepSchemas`, `TestLoadRejectsInvalidBuiltInStepSchemas`
- Wiring evidence:
  - `schemas/git.commit.amata.schema.json` and `schemas/git.inspect.amata.schema.json` enforce accepted fields
  - `docs/engine/index.md` documents typed summary fields and descriptor guarantees

### 2.4 Documentation alignment
- Code/doc paths:
  - `docs/engine/index.md` lines for `git.inspect`, `git.commit`, and renderer metadata guarantees
- Tests:
  - `internal/runtime/runner_progress_test.go`: verifies `git.commit` completed-line summary and detail text
  - `internal/progress/descriptor_test.go`: verifies descriptor shapes for `git.inspect` and `git.commit`
- Wiring evidence:
  - `internal/progress/descriptor.go` converts typed step values into user-facing summary symbols consumed by renderers.
