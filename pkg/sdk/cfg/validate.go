package cfg

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
)

// ValidateConfigurationType verifies that every reachable field in
// a fresh New() instance is a wrapper from this package or a struct
// whose fields recurse to wrappers. Call this from a module's unit
// tests to catch misuse at go-test time. The runtime calls it at
// module load to fail fast on a misdeclared configuration.
func ValidateConfigurationType(ct *ConfigurationType) error {
	if ct == nil {
		return errors.New("ConfigurationType is nil")
	}
	if ct.New == nil {
		return errors.New("ConfigurationType.New is nil")
	}
	inst := ct.New()
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

	var errs []string
	for f := range t.Fields() {
		if !f.IsExported() {
			continue
		}
		ft := f.Type
		if ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		fieldPath := f.Name
		if path != "" {
			fieldPath = path + "." + f.Name
		}
		if implementsValue(ft) {
			continue
		}
		if ft.Kind() == reflect.Struct {
			if err := walkStruct(ft, fieldPath, visited); err != nil {
				errs = append(errs, err.Error())
			}
			continue
		}
		errs = append(errs, fmt.Sprintf(
			"type of field %s is %s, but must be a Value or a struct with Value fields",
			fieldPath, f.Type))
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func implementsValue(t reflect.Type) bool {
	return t.Implements(valueType) || reflect.PointerTo(t).Implements(valueType)
}

var valueType = reflect.TypeFor[Value]()
