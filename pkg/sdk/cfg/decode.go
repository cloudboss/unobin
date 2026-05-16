package cfg

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
)

// Decode populates a fresh instance returned by ct.New() with values
// from raw. Field names match in kebab form (AssumeRole reads from
// "assume-role"). A non-pointer wrapper field is required; a pointer
// to a wrapper is optional and falls back to the wrapper's Default
// when the key is absent. An unknown key in raw is an error.
//
// This first cut supports atomic wrappers (String, Integer, Number,
// Boolean) and nested struct recursion. Collection and Object
// wrappers are not yet handled.
func Decode(ct *ConfigurationType, raw map[string]any) (any, error) {
	if ct == nil || ct.New == nil {
		return nil, errors.New("Decode: ConfigurationType has no New")
	}
	inst := ct.New()
	v := reflect.ValueOf(inst)
	if v.Kind() != reflect.Pointer || v.Elem().Kind() != reflect.Struct {
		return nil, fmt.Errorf(
			"Decode: New must return a pointer to a struct; got %s", v.Type())
	}
	errs := &errList{}
	decodeStruct(v.Elem(), raw, "", errs)
	if !errs.ok() {
		return nil, errs.err()
	}
	return inst, nil
}

type errList struct {
	msgs []string
}

func (e *errList) addf(format string, args ...any) {
	e.msgs = append(e.msgs, fmt.Sprintf(format, args...))
}

func (e *errList) ok() bool { return len(e.msgs) == 0 }

func (e *errList) err() error {
	return errors.New(strings.Join(e.msgs, "; "))
}

func decodeStruct(s reflect.Value, raw map[string]any, path string, errs *errList) {
	t := s.Type()
	seen := make(map[string]bool)
	for f := range t.Fields() {
		if !f.IsExported() {
			continue
		}
		name := lang.PascalToKebab(f.Name)
		seen[name] = true
		fieldPath := joinPath(path, name)
		rawVal, present := raw[name]
		decodeField(
			s.FieldByIndex(f.Index), f.Type, rawVal, present, false, fieldPath, errs)
	}
	for k := range raw {
		if !seen[k] {
			errs.addf("unknown key %s", joinPath(path, k))
		}
	}
}

func decodeField(
	v reflect.Value,
	t reflect.Type,
	raw any,
	present, optional bool,
	path string,
	errs *errList,
) {
	if t.Kind() == reflect.Pointer {
		if v.IsNil() {
			if !present {
				return
			}
			v.Set(reflect.New(t.Elem()))
		}
		decodeField(v.Elem(), t.Elem(), raw, present, true, path, errs)
		return
	}
	if implementsValue(t) {
		decodeWrapper(v, t, raw, present, optional, path, errs)
		return
	}
	if t.Kind() == reflect.Struct {
		var sub map[string]any
		if present {
			m, ok := raw.(map[string]any)
			if !ok {
				errs.addf("field %s: expected a map, got %T", path, raw)
				return
			}
			sub = m
		}
		decodeStruct(v, sub, path, errs)
		return
	}
	errs.addf("field %s: unsupported type %s", path, t)
}

func decodeWrapper(
	v reflect.Value,
	t reflect.Type,
	raw any,
	present, optional bool,
	path string,
	errs *errList,
) {
	switch t {
	case stringType:
		decodeString(v, raw, present, optional, path, errs)
	case integerType:
		decodeInteger(v, raw, present, optional, path, errs)
	case numberType:
		decodeNumber(v, raw, present, optional, path, errs)
	case booleanType:
		decodeBoolean(v, raw, present, optional, path, errs)
	default:
		errs.addf(
			"field %s: wrapper %s is not yet supported by the decoder", path, t)
	}
}

var (
	stringType  = reflect.TypeFor[String]()
	integerType = reflect.TypeFor[Integer]()
	numberType  = reflect.TypeFor[Number]()
	booleanType = reflect.TypeFor[Boolean]()
)

func decodeString(
	v reflect.Value,
	raw any,
	present, optional bool,
	path string,
	errs *errList,
) {
	w := v.Addr().Interface().(*String)
	if !present {
		if !optional {
			errs.addf("field %s: required", path)
			return
		}
		w.Value = w.Default
	} else {
		s, ok := raw.(string)
		if !ok {
			errs.addf("field %s: expected string, got %T", path, raw)
			return
		}
		w.Value = s
	}
	runValidate(w.Validate, w.Value, path, errs)
}

func decodeInteger(
	v reflect.Value,
	raw any,
	present, optional bool,
	path string,
	errs *errList,
) {
	w := v.Addr().Interface().(*Integer)
	if !present {
		if !optional {
			errs.addf("field %s: required", path)
			return
		}
		w.Value = w.Default
	} else {
		switch n := raw.(type) {
		case int64:
			w.Value = n
		case int:
			w.Value = int64(n)
		case int32:
			w.Value = int64(n)
		default:
			errs.addf("field %s: expected integer, got %T", path, raw)
			return
		}
	}
	runValidate(w.Validate, w.Value, path, errs)
}

func decodeNumber(
	v reflect.Value,
	raw any,
	present, optional bool,
	path string,
	errs *errList,
) {
	w := v.Addr().Interface().(*Number)
	if !present {
		if !optional {
			errs.addf("field %s: required", path)
			return
		}
		w.Value = w.Default
	} else {
		switch n := raw.(type) {
		case float64:
			w.Value = n
		case float32:
			w.Value = float64(n)
		case int64:
			w.Value = float64(n)
		case int:
			w.Value = float64(n)
		default:
			errs.addf("field %s: expected number, got %T", path, raw)
			return
		}
	}
	runValidate(w.Validate, w.Value, path, errs)
}

func decodeBoolean(
	v reflect.Value,
	raw any,
	present, optional bool,
	path string,
	errs *errList,
) {
	w := v.Addr().Interface().(*Boolean)
	if !present {
		if !optional {
			errs.addf("field %s: required", path)
			return
		}
		w.Value = w.Default
	} else {
		b, ok := raw.(bool)
		if !ok {
			errs.addf("field %s: expected boolean, got %T", path, raw)
			return
		}
		w.Value = b
	}
}

func runValidate(v Validator, value any, path string, errs *errList) {
	if v == nil {
		return
	}
	if err := v.Check(value); err != nil {
		errs.addf("field %s: %s", path, err)
	}
}

func joinPath(parent, child string) string {
	if parent == "" {
		return child
	}
	return parent + "." + child
}
