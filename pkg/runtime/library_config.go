package runtime

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

func decodeLibraryConfig(lib *Library, raw map[string]any) (any, error) {
	if lib == nil || lib.Configuration == nil {
		return nil, errors.New("library declares no configuration")
	}
	values := cloneConfigMap(raw)
	if schema := lib.Schema; schema != nil && schema.HasConfiguration {
		if err := applyConfigDefaults(values, schema.ConfigurationDefaults); err != nil {
			return nil, err
		}
		if len(schema.ConfigurationFields) > 0 {
			if err := checkConfigObject(schema.ConfigurationFields, values, schema); err != nil {
				return nil, err
			}
		}
		if err := checkConfigConstraints(schema.ConfigurationConstraints, values); err != nil {
			return nil, err
		}
	}
	decoded, err := cfg.Decode(lib.Configuration, values)
	if err != nil {
		return nil, err
	}
	return decoded, nil
}

func applyConfigDefaults(values map[string]any, specs []lang.DefaultSpec) error {
	for _, spec := range specs {
		if spec.Optional {
			continue
		}
		path, ok := strings.CutPrefix(spec.Field, "input.")
		if !ok || path == "" {
			continue
		}
		expr, err := lang.ParseExpr("default", []byte(spec.Value))
		if err != nil {
			return fmt.Errorf("field %q: invalid default: %v", path, err)
		}
		value, err := Eval(expr, &EvalContext{})
		if err != nil {
			return fmt.Errorf("field %q: invalid default: %v", path, err)
		}
		setConfigDefault(values, strings.Split(path, "."), value)
	}
	return nil
}

func setConfigDefault(values map[string]any, path []string, value any) {
	if len(path) == 0 {
		return
	}
	target := values
	for _, parent := range path[:len(path)-1] {
		child, ok := target[parent]
		if !ok {
			next := map[string]any{}
			target[parent] = next
			target = next
			continue
		}
		next, ok := child.(map[string]any)
		if !ok {
			return
		}
		target = next
	}
	leaf := path[len(path)-1]
	if _, ok := target[leaf]; ok {
		return
	}
	target[leaf] = value
}

func checkConfigObject(
	fields []typecheck.ObjectField,
	values map[string]any,
	schema *LibrarySchema,
) error {
	declared := map[string]typecheck.ObjectField{}
	for _, field := range fields {
		declared[field.Name] = field
		value, present := values[field.Name]
		if !present {
			if field.Optional || field.Defaulted || configFieldOptional(field.Name, schema) {
				continue
			}
			return fmt.Errorf("field %q: required but not provided", field.Name)
		}
		if value == nil {
			if field.Optional {
				continue
			}
			return fmt.Errorf("field %q: required but is null", field.Name)
		}
		if err := checkConfigValue(field.Type, value); err != nil {
			return fmt.Errorf("field %q: %w", field.Name, err)
		}
	}
	for name := range values {
		if _, ok := declared[name]; !ok {
			return fmt.Errorf("unknown field %q", name)
		}
	}
	return nil
}

func configFieldOptional(name string, schema *LibrarySchema) bool {
	if schema == nil {
		return false
	}
	want := "input." + name
	for _, spec := range schema.ConfigurationDefaults {
		if spec.Optional && spec.Field == want {
			return true
		}
	}
	return false
}

func checkConfigValue(t typecheck.Type, value any) error {
	if t.Kind == typecheck.Optional {
		if value == nil {
			return nil
		}
		t = t.Unwrap()
	}
	switch t.Kind {
	case typecheck.Unknown, typecheck.Opaque:
		return nil
	case typecheck.String:
		if _, ok := value.(string); ok {
			return nil
		}
		return fmt.Errorf("expected string, got %s", lang.TypeMessage(value))
	case typecheck.Integer:
		if _, ok := value.(int64); ok {
			return nil
		}
		return fmt.Errorf("expected integer, got %s", lang.TypeMessage(value))
	case typecheck.Number:
		switch value.(type) {
		case int64, float64:
			return nil
		}
		return fmt.Errorf("expected number, got %s", lang.TypeMessage(value))
	case typecheck.Boolean:
		if _, ok := value.(bool); ok {
			return nil
		}
		return fmt.Errorf("expected boolean, got %s", lang.TypeMessage(value))
	case typecheck.Null:
		if value == nil {
			return nil
		}
		return fmt.Errorf("expected null, got %s", lang.TypeMessage(value))
	case typecheck.List:
		items, ok := value.([]any)
		if !ok {
			return fmt.Errorf("expected list, got %s", lang.TypeMessage(value))
		}
		for i, item := range items {
			if err := checkConfigValue(elemType(t), item); err != nil {
				return fmt.Errorf("element %d: %w", i, err)
			}
		}
		return nil
	case typecheck.Map:
		items, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("expected map, got %s", lang.TypeMessage(value))
		}
		keys := slices.Sorted(maps.Keys(items))
		for _, key := range keys {
			if err := checkConfigValue(elemType(t), items[key]); err != nil {
				return fmt.Errorf("key %q: %w", key, err)
			}
		}
		return nil
	case typecheck.Object, typecheck.LibraryConfig:
		obj, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("expected object, got %s", lang.TypeMessage(value))
		}
		return checkConfigObject(t.Fields, obj, nil)
	case typecheck.Tuple:
		items, ok := value.([]any)
		if !ok {
			return fmt.Errorf("expected tuple, got %s", lang.TypeMessage(value))
		}
		if len(items) != len(t.Elems) {
			return fmt.Errorf("expected tuple of %d elements, got %d", len(t.Elems), len(items))
		}
		for i, item := range items {
			if err := checkConfigValue(t.Elems[i], item); err != nil {
				return fmt.Errorf("element %d: %w", i, err)
			}
		}
		return nil
	case typecheck.Union:
		for _, member := range t.Elems {
			if err := checkConfigValue(member, value); err == nil {
				return nil
			}
		}
		return fmt.Errorf("value does not match any union member")
	}
	return nil
}

func elemType(t typecheck.Type) typecheck.Type {
	if t.Elem == nil {
		return typecheck.TUnknown()
	}
	return *t.Elem
}

func checkConfigConstraints(specs []lang.ConstraintSpec, values map[string]any) error {
	if len(specs) == 0 {
		return nil
	}
	entries, perr := lang.ParseSpecs(specs)
	if perr.Len() > 0 {
		return perr.Err()
	}
	eval := func(expr lang.Expr, binds []lang.EachBinding) (any, error) {
		ctx := &EvalContext{Inputs: values, MissingAsNull: true}
		ApplyBindings(ctx, binds)
		return Eval(expr, ctx)
	}
	errs := lang.CheckConstraintEntries(entries, values, eval, lang.DisplayRooted)
	if errs.Len() > 0 {
		return errs.Err()
	}
	return nil
}

func cloneConfigMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = cloneConfigValue(value)
	}
	return out
}

func cloneConfigValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		return cloneConfigMap(v)
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = cloneConfigValue(item)
		}
		return out
	}
	return value
}
