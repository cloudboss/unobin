package runtime

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

func (e *Executor) prepareDesiredInputs(
	n *Node, inputs map[string]any, unresolved map[string][]string,
) error {
	if err := e.applyInputDefaults(n, inputs, unresolved); err != nil {
		return err
	}
	lib := e.librariesFor(n)[n.Alias]
	if lib == nil || lib.Schema == nil {
		return nil
	}
	schema := lib.Schema.ForType(n.Category(), n.Export())
	if schema == nil || len(schema.EvaluatedInputs) == 0 {
		return nil
	}
	optional := collectOptionalInputPaths(lib.Defaults[string(n.Category())+"."+n.Export()])
	for _, name := range slices.Sorted(maps.Keys(schema.EvaluatedInputs)) {
		value, present := inputs[name]
		if err := optional.checkField(schema.EvaluatedInputs[name], value, present, name); err != nil {
			return err
		}
	}
	return nil
}

type optionalInputPaths map[string]bool

func collectOptionalInputPaths(specs []lang.DefaultSpec) optionalInputPaths {
	var paths optionalInputPaths
	for _, spec := range specs {
		if path, ok := strings.CutPrefix(spec.Field, "input."); ok && spec.Optional {
			if paths == nil {
				paths = make(optionalInputPaths)
			}
			paths[path] = true
		}
	}
	return paths
}

func (optional optionalInputPaths) checkField(
	t typecheck.Type, value any, present bool, path string,
) error {
	if t.Kind == typecheck.Unknown || IsPendingValue(value) {
		return nil
	}
	if !present {
		if t.Kind == typecheck.Optional || optional[path] {
			return nil
		}
		return fmt.Errorf("field %q: required but not provided", path)
	}
	if value == nil {
		if typecheck.Assignable(t, typecheck.TNull()) {
			return nil
		}
		return fmt.Errorf("field %q: required but is null", path)
	}
	return optional.checkContents(t.Unwrap(), value, path)
}

func (optional optionalInputPaths) checkContents(t typecheck.Type, value any, path string) error {
	switch t.Kind {
	case typecheck.Object:
		if object, ok := value.(map[string]any); ok {
			if !t.Open {
				for _, name := range slices.Sorted(maps.Keys(object)) {
					if _, declared := t.Field(name); !declared {
						return fmt.Errorf("unknown field %q", path+"."+name)
					}
				}
			}
			for _, field := range t.Fields {
				fieldValue, present := object[field.Name]
				fieldType := field.Type
				if field.Optional {
					fieldType = typecheck.TOptional(fieldType)
				}
				if err := optional.checkField(
					fieldType, fieldValue, present, path+"."+field.Name,
				); err != nil {
					return err
				}
			}
		}
	case typecheck.List:
		if items, ok := value.([]any); ok && t.Elem != nil {
			for i, item := range items {
				if err := optional.checkField(
					*t.Elem, item, true, fmt.Sprintf("%s[%d]", path, i),
				); err != nil {
					return err
				}
			}
		}
	case typecheck.Map:
		if items, ok := value.(map[string]any); ok && t.Elem != nil {
			for _, key := range slices.Sorted(maps.Keys(items)) {
				if err := optional.checkField(
					*t.Elem, items[key], true, fmt.Sprintf("%s[%q]", path, key),
				); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
