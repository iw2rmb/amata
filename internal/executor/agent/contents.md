[agent.go](agent.go) Shared agent executor that runs providers and captures outputs into step artifacts.
[agent_test.go](agent_test.go) Behavioral tests for agent executor defaults, schema handling, and artifact persistence.
[capture.go](capture.go) Stream capture utility that owns stdout/stderr artifact files during agent execution.
[config.go](config.go) Resolution logic for provider request config, defaults merging, and structured output schema setup.
[json.go](json.go) JSON parsing and prompt/env helpers for structured agent output handling.
