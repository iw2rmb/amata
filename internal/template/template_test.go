package template

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
)

func TestRenderReturnsRawValuesForWholeExpressions(t *testing.T) {
	t.Parallel()

	value := map[string]any{
		"approved": true,
		"items":    []any{"x", int64(3)},
	}

	testCases := []struct {
		name string
		text string
		want any
	}{
		{
			name: "whole expression returns raw value",
			text: "{{ ctx.prev.value }}",
			want: value,
		},
		{
			name: "strings interpolate scalar values",
			text: "hello {{ ctx.params.name }} #{{ ctx.params.count }}",
			want: "hello roadmap #2",
		},
		{
			name: "strings interpolate json values",
			text: "items={{ ctx.prev.value[\"items\"] }}",
			want: `items=["x",3]`,
		},
		{
			name: "plain strings stay literal",
			text: "literal text",
			want: "literal text",
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := Render(testCase.text, func(expression string) (any, error) {
				switch expression {
				case "ctx.prev.value":
					return value, nil
				case "ctx.prev.value[\"items\"]":
					return value["items"], nil
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
			if !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("Render() = %#v, want %#v", got, testCase.want)
			}
		})
	}
}

func TestRenderReturnsExpressionContextOnTemplateEvaluationError(t *testing.T) {
	t.Parallel()

	_, err := Render("prefix {{ ctx.items[0] }} suffix", func(expression string) (any, error) {
		return nil, fmt.Errorf("unknown binary op: string + object")
	})
	if err == nil {
		t.Fatalf("Render() error = nil, want expression failure")
	}

	var expressionErr *ExpressionError
	if !errors.As(err, &expressionErr) {
		t.Fatalf("Render() error type = %T, want *ExpressionError", err)
	}
	if expressionErr.Index != 0 {
		t.Fatalf("ExpressionError.Index = %d, want 0", expressionErr.Index)
	}
	if expressionErr.Expression != "ctx.items[0]" {
		t.Fatalf("ExpressionError.Expression = %q, want ctx.items[0]", expressionErr.Expression)
	}
	if got := expressionErr.Cause.Error(); got != "unknown binary op: string + object" {
		t.Fatalf("ExpressionError.Cause = %q", got)
	}
}
