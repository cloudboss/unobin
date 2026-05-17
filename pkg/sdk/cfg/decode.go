package cfg

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
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
				errs.addf("field %s: expected a map, got %s", path,
					lang.TypeMessage(raw))
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
	if t.Implements(objectKindType) {
		decodeObject(v, raw, present, optional, path, errs)
		return
	}
	if t.Implements(listKindType) {
		decodeList(v, raw, present, optional, path, errs)
		return
	}
	if t.Implements(mapKindType) {
		decodeMap(v, raw, present, optional, path, errs)
		return
	}
	if t.Implements(setKindType) {
		decodeSet(v, raw, present, optional, path, errs)
		return
	}
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

// objectKind, listKind, mapKind, and setKind let the decoder
// identify generic wrappers at runtime; each generic instantiation
// has a distinct reflect.Type that direct equality cannot catch.
type (
	objectKind interface{ isUbObject() }
	listKind   interface{ isUbList() }
	mapKind    interface{ isUbMap() }
	setKind    interface{ isUbSet() }
)

var (
	stringType     = reflect.TypeFor[String]()
	integerType    = reflect.TypeFor[Integer]()
	numberType     = reflect.TypeFor[Number]()
	booleanType    = reflect.TypeFor[Boolean]()
	objectKindType = reflect.TypeFor[objectKind]()
	listKindType   = reflect.TypeFor[listKind]()
	mapKindType    = reflect.TypeFor[mapKind]()
	setKindType    = reflect.TypeFor[setKind]()
)

func decodeObject(
	v reflect.Value,
	raw any,
	present, optional bool,
	path string,
	errs *errList,
) {
	if !present {
		if !optional {
			errs.addf("field %s: required", path)
		}
		return
	}
	sub, ok := raw.(map[string]any)
	if !ok {
		errs.addf("field %s: expected a map, got %s", path, lang.TypeMessage(raw))
		return
	}
	inner := v.FieldByName("Value")
	if inner.Kind() != reflect.Struct {
		errs.addf(
			"field %s: Object[T] requires T to be a struct, got %s",
			path, inner.Type())
		return
	}
	decodeStruct(inner, sub, path, errs)

	validateField := v.FieldByName("Validate")
	if !validateField.IsNil() {
		runValidate(
			validateField.Interface().(Validator), inner.Interface(), path, errs)
	}
}

func decodeList(
	v reflect.Value,
	raw any,
	present, optional bool,
	path string,
	errs *errList,
) {
	valueField := v.FieldByName("Value")
	defaultField := v.FieldByName("Default")
	element := v.FieldByName("Element")
	if !present {
		if !optional {
			errs.addf("field %s: required", path)
			return
		}
		if defaultField.Len() > 0 {
			valueField.Set(defaultField)
		}
		return
	}
	arr, ok := raw.([]any)
	if !ok {
		errs.addf("field %s: expected a list, got %s", path, lang.TypeMessage(raw))
		return
	}
	out := reflect.MakeSlice(valueField.Type(), 0, len(arr))
	for i, item := range arr {
		elem := reflect.New(element.Type()).Elem()
		elem.Set(element)
		itemPath := fmt.Sprintf("%s[%d]", path, i)
		decodeField(elem, element.Type(), item, true, false, itemPath, errs)
		out = reflect.Append(out, elem)
	}
	valueField.Set(out)

	validateField := v.FieldByName("Validate")
	if !validateField.IsNil() {
		runValidate(
			validateField.Interface().(Validator), valueField.Interface(), path, errs)
	}
}

func decodeMap(
	v reflect.Value,
	raw any,
	present, optional bool,
	path string,
	errs *errList,
) {
	valueField := v.FieldByName("Value")
	defaultField := v.FieldByName("Default")
	element := v.FieldByName("Element")
	if !present {
		if !optional {
			errs.addf("field %s: required", path)
			return
		}
		if defaultField.Len() > 0 {
			valueField.Set(defaultField)
		}
		return
	}
	m, ok := raw.(map[string]any)
	if !ok {
		errs.addf("field %s: expected a map, got %s", path, lang.TypeMessage(raw))
		return
	}
	out := reflect.MakeMapWithSize(valueField.Type(), len(m))
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		elem := reflect.New(element.Type()).Elem()
		elem.Set(element)
		itemPath := fmt.Sprintf("%s[%q]", path, k)
		decodeField(elem, element.Type(), m[k], true, false, itemPath, errs)
		out.SetMapIndex(reflect.ValueOf(k), elem)
	}
	valueField.Set(out)

	validateField := v.FieldByName("Validate")
	if !validateField.IsNil() {
		runValidate(
			validateField.Interface().(Validator), valueField.Interface(), path, errs)
	}
}

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
			errs.addf("field %s: expected a string, got %s", path,
				lang.TypeMessage(raw))
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
			errs.addf("field %s: expected an integer, got %s", path,
				lang.TypeMessage(raw))
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
			errs.addf("field %s: expected a number, got %s", path,
				lang.TypeMessage(raw))
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
			errs.addf("field %s: expected a boolean, got %s", path,
				lang.TypeMessage(raw))
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

// decodeSet copies the list decode flow plus a duplicate check
// against each element's Value field. The element type's Value must
// be comparable; non-scalar element types like Object[T] or List[T]
// produce a clear load-time error rather than a panic.
func decodeSet(
	v reflect.Value,
	raw any,
	present, optional bool,
	path string,
	errs *errList,
) {
	valueField := v.FieldByName("Value")
	defaultField := v.FieldByName("Default")
	element := v.FieldByName("Element")
	if !present {
		if !optional {
			errs.addf("field %s: required", path)
			return
		}
		if defaultField.Len() > 0 {
			valueField.Set(defaultField)
		}
		return
	}
	arr, ok := raw.([]any)
	if !ok {
		errs.addf("field %s: expected a list, got %s", path, lang.TypeMessage(raw))
		return
	}
	innerField, ok := element.Type().FieldByName("Value")
	if !ok {
		errs.addf(
			"field %s: set element type %s has no Value field",
			path, element.Type())
		return
	}
	if !innerField.Type.Comparable() {
		errs.addf(
			"field %s: set element value type %s is not comparable; "+
				"only sets of scalar wrappers are supported",
			path, innerField.Type)
		return
	}
	out := reflect.MakeSlice(valueField.Type(), 0, len(arr))
	seen := make(map[any]struct{}, len(arr))
	for i, item := range arr {
		elem := reflect.New(element.Type()).Elem()
		elem.Set(element)
		itemPath := fmt.Sprintf("%s[%d]", path, i)
		decodeField(elem, element.Type(), item, true, false, itemPath, errs)
		key := elem.FieldByName("Value").Interface()
		if _, dup := seen[key]; dup {
			errs.addf("field %s: duplicate element %v", itemPath, key)
			continue
		}
		seen[key] = struct{}{}
		out = reflect.Append(out, elem)
	}
	valueField.Set(out)

	validate := v.FieldByName("Validate")
	if !validate.IsNil() {
		runValidate(
			validate.Interface().(Validator), valueField.Interface(), path, errs)
	}
}

func joinPath(parent, child string) string {
	if parent == "" {
		return child
	}
	return parent + "." + child
}
