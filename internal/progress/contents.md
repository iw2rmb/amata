[agent_output.go](agent_output.go) Parses Claude/Codex stdout and stderr artifacts into token totals, recent action details, and latest event timing.
[agent_output_test.go](agent_output_test.go) Tests token formatting and agent progress rendering for Claude/Codex streams, including stderr token fallback behavior.
[descriptor.go](descriptor.go) Builds step descriptor metadata from runtime context/results, including agent artifact paths and per-executor summary text.
[descriptor_test.go](descriptor_test.go) Validates descriptor text, summaries, and wrapping across executor types and run states.
[progress.go](progress.go) Defines progress event/snapshot models and reporter logic for run/step lifecycle emission.
[prompt_render.go](prompt_render.go) Renders agent prompt/thinking/shell detail blocks and appends formatted last-action content to step details.
[stream.go](stream.go) Implements plain and Bubble Tea progress stream renderers, active-step handling, and interactive detail toggles.
[stream_git_commit.go](stream_git_commit.go) Formats git.commit totals and per-file diff tables with optional colors and file hyperlinks.
[stream_git_commit_test.go](stream_git_commit_test.go) Tests git commit diff-table alignment, totals formatting, ANSI coloring, and hyperlink rendering.
[stream_test.go](stream_test.go) Tests stream block/model rendering for statuses, spinner behavior, collapsed active stacks, and keyboard-expanded agent sections.
