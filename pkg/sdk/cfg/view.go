package cfg

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/cloudboss/unobin/pkg/encoding/ub"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

// LibraryConfigView is the config schema in the form used by input and
// type checking.
type LibraryConfigView struct {
	Identity     string
	Fields       []typecheck.ObjectField
	Defaults     []lang.DefaultSpec
	Empty        bool
	SchemaDigest string
}

// View converts a config registration into the schema view used by
// library-config inputs. A nil registration returns the zero view; an empty
// struct registration returns Empty=true and a stable digest.
func View(ct Registration) (LibraryConfigView, error) {
	if ct == nil {
		return LibraryConfigView{}, nil
	}
	inst := ct.NewAny()
	v := reflect.ValueOf(inst)
	if !v.IsValid() {
		return LibraryConfigView{}, nil
	}
	orig := v.Type()
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return LibraryConfigView{}, nil
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return LibraryConfigView{}, fmt.Errorf(
			"View: New must return a pointer to a struct; got %s", orig)
	}
	fields, defaults, err := viewStruct(v, nil, map[reflect.Type]bool{})
	if err != nil {
		return LibraryConfigView{}, err
	}
	out := LibraryConfigView{
		Identity: configIdentity(v.Type()),
		Fields:   fields,
		Defaults: defaults,
		Empty:    len(fields) == 0,
	}
	out.SchemaDigest = DigestView(out.Fields, out.Defaults, nil)
	return out, nil
}

func configIdentity(t reflect.Type) string {
	if t == nil {
		return ""
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Name() == "" || t.PkgPath() == "" {
		return ""
	}
	return t.PkgPath() + "." + t.Name()
}

func viewStruct(
	v reflect.Value,
	path []string,
	visiting map[reflect.Type]bool,
) ([]typecheck.ObjectField, []lang.DefaultSpec, error) {
	if v.Kind() != reflect.Struct {
		return nil, nil, nil
	}
	t := v.Type()
	if visiting[t] {
		return nil, nil, nil
	}
	visiting[t] = true
	defer delete(visiting, t)

	fields := make([]typecheck.ObjectField, 0, t.NumField())
	var defaults []lang.DefaultSpec
	for f := range t.Fields() {
		if !f.IsExported() {
			continue
		}
		if f.Anonymous {
			return nil, nil, fmt.Errorf(
				"field %s: anonymous field %s is not supported; use a named field",
				fieldPath(path, lang.PascalToKebab(f.Name)), f.Type)
		}
		if _, ok := inputFieldName(f); !ok {
			continue
		}
		field, fieldDefaults, err := viewField(f, v.FieldByIndex(f.Index), path, visiting)
		if err != nil {
			return nil, nil, err
		}
		fields = append(fields, field)
		defaults = append(defaults, fieldDefaults...)
	}
	return fields, defaults, nil
}

func viewField(
	f reflect.StructField,
	fv reflect.Value,
	path []string,
	visiting map[reflect.Type]bool,
) (typecheck.ObjectField, []lang.DefaultSpec, error) {
	name, ok := inputFieldName(f)
	if !ok {
		return typecheck.ObjectField{}, nil, nil
	}
	fieldPathParts := append(append([]string{}, path...), name)
	ft := f.Type
	optional := false
	pointerValue := reflect.Value{}
	if ft.Kind() == reflect.Pointer {
		optional = true
		ft = ft.Elem()
		if fv.IsValid() && !fv.IsNil() {
			pointerValue = fv.Elem()
		}
	}

	valueForType := fv
	if optional {
		valueForType = pointerValue
	}
	fieldType, defaults, err := viewType(ft, valueForType, fieldPathParts, visiting)
	if err != nil {
		return typecheck.ObjectField{}, nil, err
	}
	field := typecheck.ObjectField{Name: name, Type: fieldType, Optional: optional}
	if def, ok, err := wrapperDefaultSpec(ft, pointerValue, fieldPathParts, optional); err != nil {
		return typecheck.ObjectField{}, nil, err
	} else if ok {
		field.Defaulted = true
		defaults = append([]lang.DefaultSpec{def}, defaults...)
	}
	return field, defaults, nil
}

func viewType(
	t reflect.Type,
	v reflect.Value,
	path []string,
	visiting map[reflect.Type]bool,
) (typecheck.Type, []lang.DefaultSpec, error) {
	if t.Kind() == reflect.Pointer {
		inner, defaults, err := viewType(t.Elem(), reflect.Value{}, path, visiting)
		return typecheck.TOptional(inner), defaults, err
	}
	if implementsValue(t) {
		return viewWrapperType(t, v, path, visiting)
	}
	if t == durationType {
		return typecheck.TInteger(), nil, nil
	}
	switch t.Kind() {
	case reflect.String:
		return typecheck.TString(), nil, nil
	case reflect.Bool:
		return typecheck.TBoolean(), nil, nil
	case reflect.Int64:
		return typecheck.TInteger(), nil, nil
	case reflect.Float64:
		return typecheck.TNumber(), nil, nil
	case reflect.Interface:
		if t.NumMethod() == 0 {
			return typecheck.TOpaque(), nil, nil
		}
	case reflect.Slice:
		elem, _, err := viewType(t.Elem(), reflect.Value{}, path, visiting)
		if err != nil {
			return typecheck.TUnknown(), nil, err
		}
		return typecheck.TList(elem), nil, nil
	case reflect.Map:
		if t.Key().Kind() != reflect.String {
			break
		}
		elem, _, err := viewType(t.Elem(), reflect.Value{}, path, visiting)
		if err != nil {
			return typecheck.TUnknown(), nil, err
		}
		return typecheck.TMap(elem), nil, nil
	case reflect.Struct:
		if !v.IsValid() {
			v = reflect.New(t).Elem()
		}
		fields, defaults, err := viewStruct(v, path, visiting)
		if err != nil {
			return typecheck.TUnknown(), nil, err
		}
		return typecheck.TObject(fields), defaults, nil
	}
	return typecheck.TUnknown(), nil, fmt.Errorf(
		"field %s: unsupported type %s", fieldPath(path, ""), t)
}

func viewWrapperType(
	t reflect.Type,
	v reflect.Value,
	path []string,
	visiting map[reflect.Type]bool,
) (typecheck.Type, []lang.DefaultSpec, error) {
	if t.Implements(objectKindType) {
		valueField, ok := t.FieldByName("Value")
		if !ok || valueField.Type.Kind() != reflect.Struct {
			return typecheck.TUnknown(), nil, fmt.Errorf(
				"field %s: Object[T] requires T to be a struct", fieldPath(path, ""))
		}
		iv := reflect.Value{}
		if v.IsValid() {
			iv = v.FieldByName("Value")
		}
		if !iv.IsValid() {
			iv = reflect.New(valueField.Type).Elem()
		}
		fields, defaults, err := viewStruct(iv, path, visiting)
		if err != nil {
			return typecheck.TUnknown(), nil, err
		}
		return typecheck.TObject(fields), defaults, nil
	}
	if t.Implements(listKindType) {
		elem, ok := t.FieldByName("Element")
		if !ok {
			return typecheck.TUnknown(), nil, nil
		}
		et, _, err := viewType(elem.Type, reflect.Value{}, path, visiting)
		if err != nil {
			return typecheck.TUnknown(), nil, err
		}
		return typecheck.TList(et), nil, nil
	}
	if t.Implements(mapKindType) {
		elem, ok := t.FieldByName("Element")
		if !ok {
			return typecheck.TUnknown(), nil, nil
		}
		et, _, err := viewType(elem.Type, reflect.Value{}, path, visiting)
		if err != nil {
			return typecheck.TUnknown(), nil, err
		}
		return typecheck.TMap(et), nil, nil
	}
	switch t {
	case stringType:
		return typecheck.TString(), nil, nil
	case integerType:
		return typecheck.TInteger(), nil, nil
	case numberType:
		return typecheck.TNumber(), nil, nil
	case booleanType:
		return typecheck.TBoolean(), nil, nil
	}
	return typecheck.TUnknown(), nil, nil
}

func wrapperDefaultSpec(
	t reflect.Type,
	v reflect.Value,
	path []string,
	optional bool,
) (lang.DefaultSpec, bool, error) {
	if !optional || !v.IsValid() || !implementsValue(t) || t.Implements(objectKindType) {
		return lang.DefaultSpec{}, false, nil
	}
	value, ok, err := wrapperDefaultValue(t, v)
	if err != nil || !ok {
		return lang.DefaultSpec{}, false, err
	}
	return lang.DefaultSpec{Field: "input." + strings.Join(path, "."), Value: lang.Render(value)},
		true, nil
}

func wrapperDefaultValue(t reflect.Type, v reflect.Value) (any, bool, error) {
	switch t {
	case stringType:
		return v.FieldByName("Default").String(), true, nil
	case integerType:
		return v.FieldByName("Default").Int(), true, nil
	case numberType:
		return v.FieldByName("Default").Float(), true, nil
	case booleanType:
		return v.FieldByName("Default").Bool(), true, nil
	}
	if t.Implements(listKindType) {
		def := v.FieldByName("Default")
		if def.Len() == 0 {
			return nil, false, nil
		}
		out := make([]any, 0, def.Len())
		for i := range def.Len() {
			item, err := wrapperValue(def.Index(i))
			if err != nil {
				return nil, false, err
			}
			out = append(out, item)
		}
		return out, true, nil
	}
	if t.Implements(mapKindType) {
		def := v.FieldByName("Default")
		if def.Len() == 0 {
			return nil, false, nil
		}
		out := make(map[string]any, def.Len())
		for _, key := range def.MapKeys() {
			item, err := wrapperValue(def.MapIndex(key))
			if err != nil {
				return nil, false, err
			}
			out[key.String()] = item
		}
		return out, true, nil
	}
	return nil, false, nil
}

func wrapperValue(v reflect.Value) (any, error) {
	if !v.IsValid() || !implementsValue(v.Type()) {
		return nil, fmt.Errorf("unsupported default value %s", v.Type())
	}
	switch v.Type() {
	case stringType:
		return v.FieldByName("Value").String(), nil
	case integerType:
		return v.FieldByName("Value").Int(), nil
	case numberType:
		return v.FieldByName("Value").Float(), nil
	case booleanType:
		return v.FieldByName("Value").Bool(), nil
	}
	return nil, fmt.Errorf("unsupported default value %s", v.Type())
}

// DigestView returns a deterministic digest for a config schema view.
func DigestView(
	fields []typecheck.ObjectField,
	defaults []lang.DefaultSpec,
	constraints []lang.ConstraintSpec,
) string {
	h := sha256.New()
	writeViewFields(h, fields)
	for _, def := range defaults {
		fmt.Fprintf(h, "default:%s=%s:%t\n", def.Field, def.Value, def.Optional)
	}
	for _, constraint := range constraints {
		fmt.Fprintf(h, "constraint-kind:%s\n", constraint.Kind)
		for _, field := range constraint.Fields {
			fmt.Fprintf(h, "constraint-field:%s\n", field)
		}
		fmt.Fprintf(h, "constraint-when:%s\n", constraint.When)
		fmt.Fprintf(h, "constraint-require:%s\n", constraint.Require)
		fmt.Fprintf(h, "constraint-message:%s\n", constraint.Message)
		fmt.Fprintf(h, "constraint-for-each:%s\n", constraint.ForEach)
		for _, level := range constraint.ForEachLevels {
			fmt.Fprintf(h, "constraint-for-each-level:%s=%s\n", level.Name, level.In)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func writeViewFields(w io.Writer, fields []typecheck.ObjectField) {
	for _, field := range fields {
		fmt.Fprintf(w, "field:%s:%s:%t:%t\n",
			field.Name, field.Type.String(), field.Optional, field.Defaulted)
		if field.Type.Kind == typecheck.Object {
			writeViewFields(w, field.Type.Fields)
		}
	}
}

func inputFieldName(f reflect.StructField) (string, bool) {
	tag := ub.ParseTag(f.Tag.Get("ub"))
	if tag.Skip {
		return "", false
	}
	if tag.Name != "" {
		return tag.Name, true
	}
	return lang.PascalToKebab(f.Name), true
}

func fieldPath(path []string, name string) string {
	parts := path
	if name != "" {
		parts = append(append([]string{}, path...), name)
	}
	if len(parts) == 0 {
		return "<root>"
	}
	return strings.Join(parts, ".")
}
