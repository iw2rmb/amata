[00_embed.go](00_embed.go) Embeds built-in step schema JSON files and exposes helpers to list and read schema documents by name.
[assert.amata.schema.json](assert.amata.schema.json) JSON Schema for `assert` steps with assertion input, optional message, and response/stall settings.
[call.amata.schema.json](call.amata.schema.json) JSON Schema for `call` control steps that invoke another flow.
[claude.amata.schema.json](claude.amata.schema.json) JSON Schema for `claude` agent steps with prompt/model/runtime and response controls.
[codex.amata.schema.json](codex.amata.schema.json) JSON Schema for `codex` agent steps with prompt/model/runtime and response controls.
[common.response-config.amata.schema.json](common.response-config.amata.schema.json) Shared schema for `response` configuration, including source selection and output schema constraints.
[common.stall.amata.schema.json](common.stall.amata.schema.json) Shared schema for stall handling policies (`rerun`, `error`, or fallback flow call).
[common.string-or-expr.amata.schema.json](common.string-or-expr.amata.schema.json) Shared schema type allowing either literal strings or `{expr: ...}` expressions.
[crush.amata.schema.json](crush.amata.schema.json) JSON Schema for `crush` agent steps with prompt/model/runtime and response controls.
[data.get.amata.schema.json](data.get.amata.schema.json) JSON Schema for `data.get` steps that read files and extract values via query/default options.
[expr.amata.schema.json](expr.amata.schema.json) JSON Schema for `expr` steps that evaluate expressions.
[for_each.amata.schema.json](for_each.amata.schema.json) JSON Schema for `for_each` steps with item source, alias, and required body steps.
[git.commit.amata.schema.json](git.commit.amata.schema.json) JSON Schema for `git.commit` steps, including message/body/exclusions and default result schema binding.
[git.commit.value.amata.schema.json](git.commit.value.amata.schema.json) JSON Schema for `git.commit` result payloads with commit status, paths, and file-level diff metadata.
[git.inspect.amata.schema.json](git.inspect.amata.schema.json) JSON Schema for `git.inspect` steps and default output schema reference.
[git.inspect.value.amata.schema.json](git.inspect.value.amata.schema.json) JSON Schema for `git.inspect` result payloads describing repository/diff state and file list.
[shell.amata.schema.json](shell.amata.schema.json) JSON Schema for `shell` steps with command, cwd/files options, and default output schema reference.
[shell.value.amata.schema.json](shell.value.amata.schema.json) JSON Schema for `shell` result payloads containing process exit code.
[switch.amata.schema.json](switch.amata.schema.json) JSON Schema for `switch` control steps with ordered conditional/default cases and nested steps.
[when.amata.schema.json](when.amata.schema.json) JSON Schema for reusable `when` expression objects.
