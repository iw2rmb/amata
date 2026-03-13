package expr

import (
	"fmt"
	"sort"
	"strings"

	templateapi "auto/internal/template"

	"go.starlark.net/starlark"
)

type Runtime struct {
	ctx map[string]any
}

func NewRuntime(ctx map[string]any) Runtime {
	return Runtime{
		ctx: cloneMap(ctx),
	}
}

func (r Runtime) Eval(expression string) (any, error) {
	globals, err := toGlobals(r.ctx)
	if err != nil {
		return nil, err
	}

	value, err := starlark.Eval(&starlark.Thread{Name: "expr"}, "<expr>", expression, globals)
	if err != nil {
		return nil, err
	}

	return fromStarlark(value)
}

func (r Runtime) Resolve(value any) (any, error) {
	switch typed := value.(type) {
	case map[string]any:
		if expression, ok, err := expressionString(typed); err != nil {
			return nil, err
		} else if ok {
			return r.Eval(expression)
		}

		resolved := make(map[string]any, len(typed))
		keys := sortedKeys(typed)
		for _, key := range keys {
			value, err := r.Resolve(typed[key])
			if err != nil {
				return nil, fmt.Errorf("%s: %w", key, err)
			}
			resolved[key] = value
		}
		return resolved, nil
	case []any:
		resolved := make([]any, len(typed))
		for index, item := range typed {
			value, err := r.Resolve(item)
			if err != nil {
				return nil, fmt.Errorf("[%d]: %w", index, err)
			}
			resolved[index] = value
		}
		return resolved, nil
	case string:
		return r.resolveString(typed)
	default:
		return value, nil
	}
}

func (r Runtime) ResolveString(value any) (string, error) {
	resolved, err := r.Resolve(value)
	if err != nil {
		return "", err
	}

	text, ok := resolved.(string)
	if !ok {
		return "", fmt.Errorf("must resolve to a string")
	}
	if text == "" {
		return "", fmt.Errorf("must not be empty")
	}
	return text, nil
}

func (r Runtime) WithBindings(bindings map[string]any) Runtime {
	next := cloneMap(r.ctx)
	baseCtx, ok := next["ctx"].(map[string]any)
	if !ok {
		baseCtx = map[string]any{}
	}
	baseCtx = cloneMap(baseCtx)
	keys := sortedKeys(bindings)
	for _, key := range keys {
		baseCtx[key] = cloneJSONValue(bindings[key])
	}
	next["ctx"] = baseCtx
	return Runtime{ctx: next}
}

func (r Runtime) resolveString(value string) (any, error) {
	if strings.HasPrefix(value, "$$") {
		return value[1:], nil
	}
	if strings.HasPrefix(value, "$.") {
		return r.Eval("ctx" + strings.TrimPrefix(value, "$"))
	}
	if strings.Contains(value, "{{") {
		return templateapi.Render(value, r.Eval)
	}
	return value, nil
}

func expressionString(value map[string]any) (string, bool, error) {
	if len(value) != 1 {
		return "", false, nil
	}

	raw, ok := value["expr"]
	if !ok {
		return "", false, nil
	}

	expression, ok := raw.(string)
	if !ok {
		return "", true, fmt.Errorf("expr must be a string")
	}
	if strings.TrimSpace(expression) == "" {
		return "", true, fmt.Errorf("expr must not be empty")
	}
	return expression, true, nil
}

func toGlobals(ctx map[string]any) (starlark.StringDict, error) {
	globals := starlark.StringDict{}
	keys := sortedKeys(ctx)
	for _, key := range keys {
		value, err := toStarlark(cloneJSONValue(ctx[key]))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		globals[key] = value
	}
	for _, value := range globals {
		value.Freeze()
	}
	return globals, nil
}

func toStarlark(value any) (starlark.Value, error) {
	switch typed := value.(type) {
	case nil:
		return starlark.None, nil
	case bool:
		return starlark.Bool(typed), nil
	case string:
		return starlark.String(typed), nil
	case int:
		return starlark.MakeInt(typed), nil
	case int8:
		return starlark.MakeInt64(int64(typed)), nil
	case int16:
		return starlark.MakeInt64(int64(typed)), nil
	case int32:
		return starlark.MakeInt64(int64(typed)), nil
	case int64:
		return starlark.MakeInt64(typed), nil
	case uint:
		return starlark.MakeUint64(uint64(typed)), nil
	case uint8:
		return starlark.MakeUint64(uint64(typed)), nil
	case uint16:
		return starlark.MakeUint64(uint64(typed)), nil
	case uint32:
		return starlark.MakeUint64(uint64(typed)), nil
	case uint64:
		return starlark.MakeUint64(typed), nil
	case float32:
		return starlark.Float(typed), nil
	case float64:
		return starlark.Float(typed), nil
	case []any:
		items := make([]starlark.Value, len(typed))
		for index, item := range typed {
			value, err := toStarlark(item)
			if err != nil {
				return nil, fmt.Errorf("[%d]: %w", index, err)
			}
			items[index] = value
		}
		return starlark.NewList(items), nil
	case map[string]any:
		fields := map[string]starlark.Value{}
		keys := sortedKeys(typed)
		for _, key := range keys {
			value, err := toStarlark(typed[key])
			if err != nil {
				return nil, fmt.Errorf("%s: %w", key, err)
			}
			fields[key] = value
		}
		return newObject(fields), nil
	default:
		return nil, fmt.Errorf("unsupported type %T", value)
	}
}

func fromStarlark(value starlark.Value) (any, error) {
	switch typed := value.(type) {
	case nil:
		return nil, nil
	case starlark.NoneType:
		return nil, nil
	case starlark.Bool:
		return bool(typed), nil
	case starlark.String:
		return typed.GoString(), nil
	case starlark.Int:
		if signed, ok := typed.Int64(); ok {
			return signed, nil
		}
		if unsigned, ok := typed.Uint64(); ok {
			return unsigned, nil
		}
		return nil, fmt.Errorf("integer is out of range")
	case starlark.Float:
		return float64(typed), nil
	case *starlark.List:
		resolved := make([]any, typed.Len())
		for index := range typed.Len() {
			value, err := fromStarlark(typed.Index(index))
			if err != nil {
				return nil, fmt.Errorf("[%d]: %w", index, err)
			}
			resolved[index] = value
		}
		return resolved, nil
	case starlark.Tuple:
		resolved := make([]any, len(typed))
		for index, item := range typed {
			value, err := fromStarlark(item)
			if err != nil {
				return nil, fmt.Errorf("[%d]: %w", index, err)
			}
			resolved[index] = value
		}
		return resolved, nil
	case *object:
		resolved := make(map[string]any, len(typed.fields))
		keys := typed.keys()
		for _, key := range keys {
			value, err := fromStarlark(typed.fields[key])
			if err != nil {
				return nil, fmt.Errorf("%s: %w", key, err)
			}
			resolved[key] = value
		}
		return resolved, nil
	case *starlark.Dict:
		resolved := map[string]any{}
		for _, item := range typed.Items() {
			key, ok := starlark.AsString(item[0])
			if !ok {
				return nil, fmt.Errorf("dict key %s is not a string", item[0].Type())
			}
			value, err := fromStarlark(item[1])
			if err != nil {
				return nil, fmt.Errorf("%s: %w", key, err)
			}
			resolved[key] = value
		}
		return resolved, nil
	default:
		return nil, fmt.Errorf("unsupported result type %s", value.Type())
	}
}

func cloneMap(source map[string]any) map[string]any {
	if source == nil {
		return map[string]any{}
	}
	cloned := make(map[string]any, len(source))
	keys := sortedKeys(source)
	for _, key := range keys {
		cloned[key] = cloneJSONValue(source[key])
	}
	return cloned
}

func cloneJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneMap(typed)
	case []any:
		cloned := make([]any, len(typed))
		for index, item := range typed {
			cloned[index] = cloneJSONValue(item)
		}
		return cloned
	default:
		return value
	}
}

func sortedKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
