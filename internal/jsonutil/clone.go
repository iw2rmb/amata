package jsonutil

import "reflect"

func CloneValue(value any) any {
	switch typed := value.(type) {
	case nil:
		return nil
	case map[string]any:
		return CloneMap(typed)
	case []any:
		cloned := make([]any, len(typed))
		for index, item := range typed {
			cloned[index] = CloneValue(item)
		}
		return cloned
	}

	return cloneReflected(reflect.ValueOf(value))
}

func CloneMap(source map[string]any) map[string]any {
	if source == nil {
		return map[string]any{}
	}
	cloned := make(map[string]any, len(source))
	for key, value := range source {
		cloned[key] = CloneValue(value)
	}
	return cloned
}

func cloneReflected(value reflect.Value) any {
	if !value.IsValid() {
		return nil
	}

	switch value.Kind() {
	case reflect.Interface, reflect.Pointer:
		if value.IsNil() {
			return nil
		}
		return CloneValue(value.Elem().Interface())
	case reflect.Map:
		if value.Type().Key().Kind() != reflect.String {
			return value.Interface()
		}
		cloned := make(map[string]any, value.Len())
		iter := value.MapRange()
		for iter.Next() {
			cloned[iter.Key().String()] = CloneValue(iter.Value().Interface())
		}
		return cloned
	case reflect.Slice, reflect.Array:
		cloned := make([]any, value.Len())
		for index := 0; index < value.Len(); index++ {
			cloned[index] = CloneValue(value.Index(index).Interface())
		}
		return cloned
	default:
		return value.Interface()
	}
}
