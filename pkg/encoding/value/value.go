package value

import (
	"fmt"
	"math"
	"slices"
)

type Kind string

const (
	KindAbsent  Kind = "absent"
	KindNull    Kind = "null"
	KindBoolean Kind = "boolean"
	KindString  Kind = "string"
	KindInteger Kind = "integer"
	KindNumber  Kind = "number"
	KindList    Kind = "list"
	KindMap     Kind = "map"
	KindObject  Kind = "object"
	KindPending Kind = "pending"
)

type Value struct {
	kind    Kind
	boolean bool
	text    string
	integer int64
	number  float64
	items   []Value
	named   []namedValue
	refs    []string
}

type namedValue struct {
	name  string
	value Value
}

func Absent() Value {
	return Value{kind: KindAbsent}
}

func Null() Value {
	return Value{kind: KindNull}
}

func Boolean(v bool) Value {
	return Value{kind: KindBoolean, boolean: v}
}

func String(v string) Value {
	return Value{kind: KindString, text: v}
}

func Integer(v int64) Value {
	return Value{kind: KindInteger, integer: v}
}

func Number(v float64) (Value, error) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return Value{}, fmt.Errorf("number must be finite")
	}
	return Value{kind: KindNumber, number: v}, nil
}

func List(items []Value) (Value, error) {
	items = append([]Value{}, items...)
	for i, item := range items {
		if err := item.validate(fmt.Sprintf("$.items[%d]", i)); err != nil {
			return Value{}, err
		}
	}
	return Value{kind: KindList, items: items}, nil
}

func Map(entries map[string]Value) (Value, error) {
	named, err := sortedNamedValues("entries", entries)
	if err != nil {
		return Value{}, err
	}
	return Value{kind: KindMap, named: named}, nil
}

func Object(fields map[string]Value) (Value, error) {
	named, err := sortedNamedValues("fields", fields)
	if err != nil {
		return Value{}, err
	}
	return Value{kind: KindObject, named: named}, nil
}

func Pending(refs []string) (Value, error) {
	if len(refs) == 0 {
		return Value{}, fmt.Errorf("at least one pending reference is required")
	}
	refs = slices.Clone(refs)
	slices.Sort(refs)
	refs = slices.Compact(refs)
	if slices.Contains(refs, "") {
		return Value{}, fmt.Errorf("pending reference is empty")
	}
	return Value{kind: KindPending, refs: refs}, nil
}

func (v Value) Kind() Kind {
	return v.kind
}

func (v Value) Boolean() (bool, bool) {
	return v.boolean, v.kind == KindBoolean
}

func (v Value) String() (string, bool) {
	return v.text, v.kind == KindString
}

func (v Value) Integer() (int64, bool) {
	return v.integer, v.kind == KindInteger
}

func (v Value) Number() (float64, bool) {
	return v.number, v.kind == KindNumber
}

func (v Value) Items() ([]Value, bool) {
	if v.kind != KindList {
		return nil, false
	}
	return slices.Clone(v.items), true
}

func (v Value) MapEntries() (map[string]Value, bool) {
	if v.kind != KindMap {
		return nil, false
	}
	return namedValueMap(v.named), true
}

func (v Value) ObjectFields() (map[string]Value, bool) {
	if v.kind != KindObject {
		return nil, false
	}
	return namedValueMap(v.named), true
}

func (v Value) PendingRefs() ([]string, bool) {
	if v.kind != KindPending {
		return nil, false
	}
	return slices.Clone(v.refs), true
}

func (v Value) HasPending() bool {
	switch v.kind {
	case KindPending:
		return true
	case KindList:
		for _, item := range v.items {
			if item.HasPending() {
				return true
			}
		}
	case KindMap, KindObject:
		for _, item := range v.named {
			if item.value.HasPending() {
				return true
			}
		}
	}
	return false
}

func sortedNamedValues(member string, values map[string]Value) ([]namedValue, error) {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	slices.Sort(names)

	named := make([]namedValue, 0, len(names))
	for i, name := range names {
		v := values[name]
		if err := v.validate(fmt.Sprintf("$.%s[%d].value", member, i)); err != nil {
			return nil, err
		}
		named = append(named, namedValue{name: name, value: v})
	}
	return named, nil
}

func namedValueMap(values []namedValue) map[string]Value {
	out := make(map[string]Value, len(values))
	for _, item := range values {
		out[item.name] = item.value
	}
	return out
}

func (v Value) validate(path string) error {
	switch v.kind {
	case KindAbsent, KindNull, KindBoolean, KindString, KindInteger:
		return nil
	case KindNumber:
		if math.IsNaN(v.number) || math.IsInf(v.number, 0) {
			return fmt.Errorf("%s: number must be finite", path)
		}
		return nil
	case KindList:
		for i, item := range v.items {
			if err := item.validate(fmt.Sprintf("%s.items[%d]", path, i)); err != nil {
				return err
			}
		}
		return nil
	case KindMap, KindObject:
		member := "entries"
		label := "key"
		if v.kind == KindObject {
			member = "fields"
			label = "name"
		}
		for i, item := range v.named {
			if i > 0 && item.name <= v.named[i-1].name {
				return fmt.Errorf("%s.%s[%d].%s: names are not unique and sorted",
					path, member, i, label)
			}
			if err := item.value.validate(
				fmt.Sprintf("%s.%s[%d].value", path, member, i),
			); err != nil {
				return err
			}
		}
		return nil
	case KindPending:
		if len(v.refs) == 0 {
			return fmt.Errorf("%s.refs: at least one reference is required", path)
		}
		for i, ref := range v.refs {
			if ref == "" {
				return fmt.Errorf("%s.refs[%d]: reference is empty", path, i)
			}
			if i > 0 && ref <= v.refs[i-1] {
				return fmt.Errorf("%s.refs[%d]: references are not unique and sorted", path, i)
			}
		}
		return nil
	case "":
		return fmt.Errorf("%s.kind: kind is required", path)
	default:
		return fmt.Errorf("%s.kind: unknown kind %q", path, v.kind)
	}
}
