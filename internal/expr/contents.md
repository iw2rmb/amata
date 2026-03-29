[object.go](object.go) Defines the custom Starlark-backed object type used for expression context maps, attribute access, iteration, and freezing.
[runtime.go](runtime.go) Implements expression evaluation and recursive value resolution between Go values, templates, shorthand paths, and Starlark types.
[runtime_test.go](runtime_test.go) Tests expression shorthand, escaping, template rendering, expression-object evaluation, and recursive resolution behavior.
