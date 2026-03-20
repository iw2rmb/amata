package jsonutil

// MapField extracts a map[string]any field from a JSON-like value by key.
func MapField(value any, key string) (map[string]any, bool) {
	fields, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	raw, ok := fields[key]
	if !ok {
		return nil, false
	}
	current, ok := raw.(map[string]any)
	return current, ok
}

// SliceField extracts a []any field from a JSON-like value by key.
func SliceField(value any, key string) ([]any, bool) {
	fields, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	raw, ok := fields[key]
	if !ok {
		return nil, false
	}
	current, ok := raw.([]any)
	return current, ok
}

// StringField extracts a string field from a JSON-like value by key.
func StringField(value any, key string) (string, bool) {
	fields, ok := value.(map[string]any)
	if !ok {
		return "", false
	}
	raw, ok := fields[key]
	if !ok {
		return "", false
	}
	text, ok := raw.(string)
	return text, ok
}

// BoolField extracts a bool field from a JSON-like value by key.
func BoolField(value any, key string) (bool, bool) {
	fields, ok := value.(map[string]any)
	if !ok {
		return false, false
	}
	raw, ok := fields[key]
	if !ok {
		return false, false
	}
	current, ok := raw.(bool)
	return current, ok
}

// IntField extracts an int field from a JSON-like value by key,
// handling int, int32, int64, and float64 source types.
func IntField(value any, key string) (int, bool) {
	fields, ok := value.(map[string]any)
	if !ok {
		return 0, false
	}
	raw, ok := fields[key]
	if !ok {
		return 0, false
	}

	switch current := raw.(type) {
	case int:
		return current, true
	case int32:
		return int(current), true
	case int64:
		return int(current), true
	case float64:
		return int(current), true
	default:
		return 0, false
	}
}

// StringSliceField extracts a []string field from a JSON-like value by key,
// handling both []string and []any source types.
func StringSliceField(value any, key string) []string {
	fields, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	raw, ok := fields[key]
	if !ok {
		return nil
	}

	switch current := raw.(type) {
	case []string:
		return append([]string(nil), current...)
	case []any:
		values := make([]string, 0, len(current))
		for _, item := range current {
			text, ok := item.(string)
			if !ok {
				continue
			}
			values = append(values, text)
		}
		return values
	default:
		return nil
	}
}
