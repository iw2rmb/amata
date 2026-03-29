[registry.go](registry.go) Builds and validates response schemas by normalizing workflow refs, expanding provider documents, and compiling JSON Schema validators.
[registry_test.go](registry_test.go) Tests schema registry compilation and provider-document expansion, including missing refs and unsupported keyword validation cases.
[source.go](source.go) Resolves, loads, and classifies response schema file paths from spec values.
