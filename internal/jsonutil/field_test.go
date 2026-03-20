package jsonutil

import (
	"reflect"
	"testing"
)

func TestMapField(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		value   any
		key     string
		want    map[string]any
		wantOK  bool
	}{
		{name: "present", value: map[string]any{"m": map[string]any{"k": "v"}}, key: "m", want: map[string]any{"k": "v"}, wantOK: true},
		{name: "missing key", value: map[string]any{}, key: "m", want: nil, wantOK: false},
		{name: "wrong type", value: map[string]any{"m": "string"}, key: "m", want: nil, wantOK: false},
		{name: "nil value", value: nil, key: "m", want: nil, wantOK: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := MapField(tc.value, tc.key)
			if ok != tc.wantOK || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("MapField = (%v, %v), want (%v, %v)", got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestSliceField(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		value  any
		key    string
		want   []any
		wantOK bool
	}{
		{name: "present", value: map[string]any{"s": []any{1, 2}}, key: "s", want: []any{1, 2}, wantOK: true},
		{name: "missing key", value: map[string]any{}, key: "s", want: nil, wantOK: false},
		{name: "wrong type", value: map[string]any{"s": "string"}, key: "s", want: nil, wantOK: false},
		{name: "nil value", value: nil, key: "s", want: nil, wantOK: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := SliceField(tc.value, tc.key)
			if ok != tc.wantOK || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("SliceField = (%v, %v), want (%v, %v)", got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestStringField(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		value  any
		key    string
		want   string
		wantOK bool
	}{
		{name: "present", value: map[string]any{"k": "hello"}, key: "k", want: "hello", wantOK: true},
		{name: "missing key", value: map[string]any{}, key: "k", want: "", wantOK: false},
		{name: "wrong type", value: map[string]any{"k": 42}, key: "k", want: "", wantOK: false},
		{name: "nil value", value: nil, key: "k", want: "", wantOK: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := StringField(tc.value, tc.key)
			if ok != tc.wantOK || got != tc.want {
				t.Fatalf("StringField = (%q, %v), want (%q, %v)", got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestBoolField(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		value  any
		key    string
		want   bool
		wantOK bool
	}{
		{name: "true", value: map[string]any{"b": true}, key: "b", want: true, wantOK: true},
		{name: "false", value: map[string]any{"b": false}, key: "b", want: false, wantOK: true},
		{name: "missing key", value: map[string]any{}, key: "b", want: false, wantOK: false},
		{name: "wrong type", value: map[string]any{"b": "yes"}, key: "b", want: false, wantOK: false},
		{name: "nil value", value: nil, key: "b", want: false, wantOK: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := BoolField(tc.value, tc.key)
			if ok != tc.wantOK || got != tc.want {
				t.Fatalf("BoolField = (%v, %v), want (%v, %v)", got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestIntField(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		value  any
		key    string
		want   int
		wantOK bool
	}{
		{name: "int", value: map[string]any{"n": 42}, key: "n", want: 42, wantOK: true},
		{name: "int32", value: map[string]any{"n": int32(42)}, key: "n", want: 42, wantOK: true},
		{name: "int64", value: map[string]any{"n": int64(42)}, key: "n", want: 42, wantOK: true},
		{name: "float64", value: map[string]any{"n": float64(42)}, key: "n", want: 42, wantOK: true},
		{name: "missing key", value: map[string]any{}, key: "n", want: 0, wantOK: false},
		{name: "wrong type", value: map[string]any{"n": "42"}, key: "n", want: 0, wantOK: false},
		{name: "nil value", value: nil, key: "n", want: 0, wantOK: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := IntField(tc.value, tc.key)
			if ok != tc.wantOK || got != tc.want {
				t.Fatalf("IntField = (%d, %v), want (%d, %v)", got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestStringSliceField(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		value any
		key   string
		want  []string
	}{
		{name: "string slice", value: map[string]any{"s": []string{"a", "b"}}, key: "s", want: []string{"a", "b"}},
		{name: "any slice", value: map[string]any{"s": []any{"a", "b"}}, key: "s", want: []string{"a", "b"}},
		{name: "any slice skips non-string", value: map[string]any{"s": []any{"a", 42, "b"}}, key: "s", want: []string{"a", "b"}},
		{name: "missing key", value: map[string]any{}, key: "s", want: nil},
		{name: "wrong type", value: map[string]any{"s": "not-a-slice"}, key: "s", want: nil},
		{name: "nil value", value: nil, key: "s", want: nil},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := StringSliceField(tc.value, tc.key)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("StringSliceField = %#v, want %#v", got, tc.want)
			}
		})
	}
}
