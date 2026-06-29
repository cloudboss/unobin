package runtime

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
)

// applyInputDefaults fills the node type's declared defaults into the
// evaluated inputs. unresolved names the fields still waiting on an
// upstream output, which keep their pending placeholder rather than
// taking a default. A node without a library record or declared
// defaults changes nothing.
func (e *Executor) applyInputDefaults(
	n *Node, inputs map[string]any, unresolved map[string][]string,
) error {
	lib, ok := e.librariesFor(n)[n.Alias]
	if !ok || lib == nil {
		return nil
	}
	specs := lib.Defaults[string(n.Kind)+"."+n.Type]
	if len(specs) == 0 {
		return nil
	}
	if err := rejectNullForNonPointerDefaults(n, lib, inputs, specs); err != nil {
		return err
	}
	return overlayDefaults(inputs, specs, unresolved)
}

func rejectNullForNonPointerDefaults(
	n *Node, lib *Library, inputs map[string]any, specs []lang.DefaultSpec,
) error {
	inputType := defaultReceiverType(n, lib)
	if inputType == nil {
		return nil
	}
	for _, spec := range specs {
		if spec.Optional {
			continue
		}
		path, ok := strings.CutPrefix(spec.Field, "input.")
		if !ok {
			continue
		}
		segments := strings.Split(path, ".")
		value, ok := defaultInputValue(inputs, segments)
		if !ok || value != nil {
			continue
		}
		fieldType, ok := defaultFieldType(inputType, segments)
		if !ok || nullableDefaultFieldType(fieldType) {
			continue
		}
		return fmt.Errorf("field %q: required but is null", strings.Join(segments, "."))
	}
	return nil
}

func defaultReceiverType(n *Node, lib *Library) reflect.Type {
	var receiver any
	switch n.Kind {
	case NodeResource:
		if reg := lib.Resources[n.Type]; reg != nil {
			receiver = reg.NewReceiver()
		}
	case NodeDataSource:
		if reg := lib.DataSources[n.Type]; reg != nil {
			receiver = reg.NewReceiver()
		}
	case NodeAction:
		if reg := lib.Actions[n.Type]; reg != nil {
			receiver = reg.NewReceiver()
		}
	}
	if receiver == nil {
		return nil
	}
	t := reflect.TypeOf(receiver)
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	return t
}

func defaultInputValue(inputs map[string]any, segments []string) (any, bool) {
	var current any = inputs
	for _, segment := range segments {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		var present bool
		current, present = object[segment]
		if !present {
			return nil, false
		}
	}
	return current, true
}

func defaultFieldType(t reflect.Type, segments []string) (reflect.Type, bool) {
	for i, segment := range segments {
		structType := defaultStructType(t)
		if structType == nil {
			return nil, false
		}
		var found bool
		for field := range structType.Fields() {
			if field.PkgPath != "" {
				continue
			}
			if defaultFieldSquashed(field) {
				if fieldType, ok := defaultFieldType(field.Type, segments[i:]); ok {
					return fieldType, true
				}
				continue
			}
			key, skip := ubFieldKey(field)
			if skip || key != segment {
				continue
			}
			if i == len(segments)-1 {
				return field.Type, true
			}
			t = field.Type
			found = true
			break
		}
		if !found {
			return nil, false
		}
	}
	return nil, false
}

func defaultStructType(t reflect.Type) reflect.Type {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	return t
}

func defaultFieldSquashed(field reflect.StructField) bool {
	for _, part := range strings.Split(field.Tag.Get("ub"), ",")[1:] {
		if strings.TrimSpace(part) == "squash" {
			return true
		}
	}
	return false
}

func nullableDefaultFieldType(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Interface, reflect.Pointer:
		return true
	default:
		return false
	}
}

// overlayDefaults fills declared Value defaults into a body's evaluated
// inputs. A default applies when its field is left out and is not still
// waiting on an upstream output; a value the body set stays, null and
// the zero value included. A dotted field applies only when every
// parent object is present, so an absent optional object is not
// invented; an Optional marker fills nothing. The literal source in a
// spec evaluates with an empty context, and one that does not reduce
// reports an error naming the field.
func overlayDefaults(
	inputs map[string]any, specs []lang.DefaultSpec, unresolved map[string][]string,
) error {
	for _, s := range specs {
		if s.Optional {
			continue
		}
		path, ok := strings.CutPrefix(s.Field, "input.")
		if !ok {
			continue
		}
		segments := strings.Split(path, ".")
		if _, pending := unresolved[segments[0]]; pending {
			continue
		}
		target := inputs
		for _, parent := range segments[:len(segments)-1] {
			child, ok := target[parent].(map[string]any)
			if !ok {
				target = nil
				break
			}
			target = child
		}
		if target == nil {
			continue
		}
		leaf := segments[len(segments)-1]
		if _, ok := target[leaf]; ok {
			continue
		}
		val, err := evalDefaultLiteral(path, s.Value)
		if err != nil {
			return err
		}
		target[leaf] = val
	}
	return nil
}

// evalDefaultLiteral reduces a default's literal source to its value.
func evalDefaultLiteral(field, src string) (any, error) {
	expr, err := lang.ParseExpr("default", []byte(src))
	if err != nil {
		return nil, fmt.Errorf("default for %q: %v", field, err)
	}
	v, err := Eval(expr, &EvalContext{})
	if err != nil {
		return nil, fmt.Errorf("default for %q: %v", field, err)
	}
	return v, nil
}
