package template

import (
	"reflect"
	"testing"
)

func TestRenderReturnsRawValuesForWholeExpressions(t *testing.T) {
	t.Parallel()

	value := map[string]any{
		"approved": true,
		"items":    []any{"x", int64(3)},
	}

	eval := func(expression string) (any, error) {
		if expression != `ctx.prev.value` {
			t.Fatalf("expression = %q, want ctx.prev.value", expression)
		}
		return value, nil
	}

	got, err := Render("{{ ctx.prev.value }}", eval)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if !reflect.DeepEqual(got, value) {
		t.Fatalf("Render() = %#v, want %#v", got, value)
	}
}

func TestRenderInterpolatesIntoStrings(t *testing.T) {
	t.Parallel()

	got, err := Render("hello {{ ctx.params.name }} #{{ ctx.params.count }}", func(expression string) (any, error) {
		switch expression {
		case "ctx.params.name":
			return "roadmap", nil
		case "ctx.params.count":
			return int64(2), nil
		default:
			t.Fatalf("unexpected expression %q", expression)
			return nil, nil
		}
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if got != "hello roadmap #2" {
		t.Fatalf("Render() = %#v, want %q", got, "hello roadmap #2")
	}
}
