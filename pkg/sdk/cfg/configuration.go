package cfg

import "reflect"

// Registration is the type-erased view of a library's configuration.
// Library authors use ConfigurationType with a concrete type parameter;
// runtime packages consume this interface.
type Registration interface {
	DescriptionText() string
	NewAny() any
	ValueType() reflect.Type
	Empty() bool
}

// ConfigurationType registers a library's configuration. New returns
// a pointer to a fresh configuration struct. The decoder populates
// its fields from stack inputs. The struct's fields must be wrapper
// types from this package or nested structs whose fields are wrapper
// types; the schema walker rejects any other field type at library
// load. Description appears in the schema commands.
type ConfigurationType[C any] struct {
	Description string
	New         func() C
}

func (ct *ConfigurationType[C]) DescriptionText() string {
	if ct == nil {
		return ""
	}
	return ct.Description
}

func (ct *ConfigurationType[C]) NewAny() any {
	if ct == nil || ct.New == nil {
		return nil
	}
	return ct.New()
}

func (ct *ConfigurationType[C]) ValueType() reflect.Type {
	if ct == nil {
		return nil
	}
	if ct.New != nil {
		return reflect.TypeOf(ct.New())
	}
	var zero C
	return reflect.TypeOf(zero)
}

func (ct *ConfigurationType[C]) Empty() bool {
	t := ct.ValueType()
	if t == nil {
		return false
	}
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t.Kind() == reflect.Struct && t.NumField() == 0
}
