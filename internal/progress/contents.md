[agent_output.go](agent_output.go) Summarizes agent stdout logs into total token counts and the latest actionable event details.
[agent_output_test.go](agent_output_test.go) Verifies token formatting and Claude/Codex output summarization used in rendered progress blocks.
[descriptor.go](descriptor.go) Builds step descriptor text from executor context and result data for progress rendering.
[descriptor_test.go](descriptor_test.go) Tests descriptor generation across executor types, statuses, and prompt/detail formatting behavior.
[progress.go](progress.go) Defines progress event/snapshot models and reporter logic that emits run and step lifecycle updates.
[prompt_render.go](prompt_render.go) Renders agent prompt markdown and appends last-action token/activity lines for step details.
[stream.go](stream.go) Implements plain and TUI stream renderers that display live and completed progress events.
[stream_git_commit.go](stream_git_commit.go) Renders git commit totals and per-file diff tables with styling and optional file hyperlinks.
[stream_git_commit_test.go](stream_git_commit_test.go) Tests git commit table alignment, totals formatting, color output, and hyperlink rendering.
[stream_test.go](stream_test.go) Covers block and stream-model rendering behavior, including spinner and status styling expectations.
