[gitcommit.go](gitcommit.go) `git.commit` executor that validates commit inputs, excludes workspace state paths, and returns commit metadata.
[gitcommit_test.go](gitcommit_test.go) Tests git commit executor behavior with fake and real repositories, including exclusions, no-op cases, and metadata shaping.
