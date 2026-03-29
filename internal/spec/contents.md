[includes.go](includes.go) Composes specs by resolving `!include` references with JSON-pointer fragments and cycle detection.
[provider_response_schema_validation.go](provider_response_schema_validation.go) Validates `codex` step `response.schema` documents across nested flows against provider schema constraints.
[spec.go](spec.go) Core workflow spec model, YAML decode/load pipeline, and top-level validation entrypoints.
[spec_test.go](spec_test.go) End-to-end spec loading tests for valid documents, schema validation failures, and shorthand acceptance.
[step_schemas.go](step_schemas.go) Embedded JSON Schema compilation and recursive built-in step validation for top-level and nested flows.
[step_unmarshal.go](step_unmarshal.go) Custom step YAML unmarshalling that supports shorthand forms and special decoding for response/switch fields.
