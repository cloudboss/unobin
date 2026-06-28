package cfg

import (
	"errors"
	"fmt"
	"reflect"
	"time"
)

// ValidateConfigurationType verifies that every reachable field in a fresh
// New() instance is either a cfg wrapper or an ordinary config field type. Call
// this from a library's unit tests to catch misuse at go-test time. The runtime
// calls it at library load to fail fast on a misdeclared configuration.
func ValidateConfigurationType(ct Registration) error {
	if ct == nil {
		return errors.New("ConfigurationType is nil")
	}
	inst := ct.NewAny()
	if inst == nil {
		return errors.New("ConfigurationType.New returned nil")
	}
	t := reflect.TypeOf(inst)
	if t.Kind() != reflect.Pointer || t.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("ConfigurationType.New must return a pointer to a struct; got %s",
			t)
	}
	return walkStruct(t.Elem(), "", map[reflect.Type]bool{})
}

func walkStruct(t reflect.Type, path string, visited map[reflect.Type]bool) error {
	if visited[t] {
		return nil
	}
	visited[t] = true
	defer delete(visited, t)

	var errs []error
	for f := range t.Fields() {
		if !f.IsExported() {
			continue
		}
		fieldPath := f.Name
		if path != "" {
			fieldPath = path + "." + f.Name
		}
		if f.Anonymous {
			errs = append(errs, fmt.Errorf(
				"field %s: anonymous field %s is not supported; use a named field",
				fieldPath, f.Type))
			continue
		}
		if err := validateFieldType(f.Type, fieldPath, visited); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func validateFieldType(t reflect.Type, path string, visited map[reflect.Type]bool) error {
	if t.Kind() == reflect.Pointer {
		return validateFieldType(t.Elem(), path, visited)
	}
	if implementsValue(t) || isPlainScalar(t) {
		return nil
	}
	switch t.Kind() {
	case reflect.Interface:
		if t.NumMethod() == 0 {
			return nil
		}
	case reflect.Slice:
		return validateFieldType(t.Elem(), path+"[]", visited)
	case reflect.Map:
		if t.Key().Kind() != reflect.String {
			return fmt.Errorf("field %s: map key type %s is not supported", path, t.Key())
		}
		return validateFieldType(t.Elem(), path+"{}", visited)
	case reflect.Struct:
		return walkStruct(t, path, visited)
	}
	return fmt.Errorf("type of field %s is %s, but is not supported", path, t)
}

func isPlainScalar(t reflect.Type) bool {
	if t == durationType {
		return true
	}
	switch t.Kind() {
	case reflect.String, reflect.Bool, reflect.Int64, reflect.Float64:
		return true
	}
	return false
}

func implementsValue(t reflect.Type) bool {
	return t.Implements(valueType) || reflect.PointerTo(t).Implements(valueType)
}

var (
	valueType    = reflect.TypeFor[Value]()
	durationType = reflect.TypeFor[time.Duration]()
)
