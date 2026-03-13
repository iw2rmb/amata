package expr

import (
	"fmt"
	"sort"

	"go.starlark.net/starlark"
)

type object struct {
	fields map[string]starlark.Value
	frozen bool
}

func newObject(fields map[string]starlark.Value) *object {
	return &object{fields: fields}
}

func (o *object) String() string {
	return starlark.StringDict(o.fields).String()
}

func (o *object) Type() string {
	return "object"
}

func (o *object) Freeze() {
	if o.frozen {
		return
	}
	o.frozen = true
	for _, value := range o.fields {
		value.Freeze()
	}
}

func (o *object) Truth() starlark.Bool {
	return starlark.Bool(len(o.fields) > 0)
}

func (o *object) Hash() (uint32, error) {
	return 0, fmt.Errorf("unhashable type: %s", o.Type())
}

func (o *object) Attr(name string) (starlark.Value, error) {
	value, ok := o.fields[name]
	if !ok {
		return nil, nil
	}
	return value, nil
}

func (o *object) AttrNames() []string {
	return o.keys()
}

func (o *object) Get(key starlark.Value) (starlark.Value, bool, error) {
	name, ok := starlark.AsString(key)
	if !ok {
		return nil, false, nil
	}
	value, ok := o.fields[name]
	if !ok {
		return nil, false, nil
	}
	return value, true, nil
}

func (o *object) Iterate() starlark.Iterator {
	keys := o.keys()
	values := make([]starlark.Value, len(keys))
	for index, key := range keys {
		values[index] = starlark.String(key)
	}
	return starlark.NewList(values).Iterate()
}

func (o *object) Len() int {
	return len(o.fields)
}

func (o *object) keys() []string {
	keys := make([]string, 0, len(o.fields))
	for key := range o.fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
