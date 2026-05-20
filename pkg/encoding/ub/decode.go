package ub

import (
	"encoding/base64"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/cloudboss/unobin/pkg/lang/parse"
)

// Unmarshaler is implemented by types that parse their own unobin
// representation. UnmarshalUB receives the bytes of the value as they
// appeared in the input (with surrounding whitespace trimmed by the
// parser) and populates the receiver.
type Unmarshaler interface {
	UnmarshalUB(data []byte) error
}

var unmarshalerType = reflect.TypeOf((*Unmarshaler)(nil)).Elem()

// Unmarshal parses data as a UB expression and stores the result in
// the value pointed to by v. v must be a non-nil pointer. The decode
// rules mirror the encoder's: bool, integer, float, string, list,
// map, struct, time.Time, time.Duration, and []byte all decode
// reflectively. Types implementing Unmarshaler receive the value's UB
// form (re-rendered from the parsed AST, so callers see a canonical
// representation rather than the input's original whitespace) at
// every level: top, struct field, map value, slice element.
func Unmarshal(data []byte, v any) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return errors.New("ub: Unmarshal requires a non-nil pointer")
	}
	expr, err := parse.ParseExpr("", data)
	if err != nil {
		return err
	}
	return decodeValue(expr, rv.Elem())
}

func decodeValue(e parse.Expr, dst reflect.Value) error {
	for dst.Kind() == reflect.Ptr {
		if _, isNull := e.(*parse.NullLit); isNull {
			dst.Set(reflect.Zero(dst.Type()))
			return nil
		}
		if dst.IsNil() {
			dst.Set(reflect.New(dst.Type().Elem()))
		}
		dst = dst.Elem()
	}

	if ok, err := tryUnmarshaler(e, dst); ok {
		return err
	}

	switch dst.Type() {
	case timeTimeType:
		return decodeTime(e, dst)
	case durationType:
		return decodeDuration(e, dst)
	}

	if dst.Kind() == reflect.Interface && dst.NumMethod() == 0 {
		v, err := naturalValue(e)
		if err != nil {
			return err
		}
		if v == nil {
			dst.Set(reflect.Zero(dst.Type()))
		} else {
			dst.Set(reflect.ValueOf(v))
		}
		return nil
	}

	switch n := e.(type) {
	case *parse.NullLit:
		dst.Set(reflect.Zero(dst.Type()))
		return nil
	case *parse.BoolLit:
		if dst.Kind() != reflect.Bool {
			return mismatch("bool", dst)
		}
		dst.SetBool(n.Value)
		return nil
	case *parse.NumberLit:
		return decodeNumber(n, dst)
	case *parse.StringLit:
		return decodeString(n, dst)
	case *parse.ArrayLit:
		return decodeArray(n, dst)
	case *parse.ObjectLit:
		return decodeObject(n, dst)
	}
	return fmt.Errorf("ub: cannot decode %T into %s", e, dst.Type())
}

func tryUnmarshaler(e parse.Expr, dst reflect.Value) (bool, error) {
	var u Unmarshaler
	switch {
	case dst.CanAddr() && dst.Addr().Type().Implements(unmarshalerType):
		u = dst.Addr().Interface().(Unmarshaler)
	case dst.Type().Implements(unmarshalerType):
		u = dst.Interface().(Unmarshaler)
	default:
		return false, nil
	}
	v, err := naturalValue(e)
	if err != nil {
		return true, err
	}
	bytes, err := Marshal(v)
	if err != nil {
		return true, err
	}
	return true, u.UnmarshalUB(bytes)
}

func decodeNumber(n *parse.NumberLit, dst reflect.Value) error {
	switch dst.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if n.IsFloat {
			return fmt.Errorf("ub: cannot decode float %s into %s", n.Value, dst.Type())
		}
		if dst.OverflowInt(n.ParsedInt) {
			return fmt.Errorf("ub: %s overflows %s", n.Value, dst.Type())
		}
		dst.SetInt(n.ParsedInt)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if n.IsFloat {
			return fmt.Errorf("ub: cannot decode float %s into %s", n.Value, dst.Type())
		}
		if n.ParsedInt < 0 {
			return fmt.Errorf("ub: cannot decode negative %s into %s", n.Value, dst.Type())
		}
		u := uint64(n.ParsedInt)
		if dst.OverflowUint(u) {
			return fmt.Errorf("ub: %s overflows %s", n.Value, dst.Type())
		}
		dst.SetUint(u)
	case reflect.Float32, reflect.Float64:
		var f float64
		if n.IsFloat {
			f = n.ParsedFloat
		} else {
			f = float64(n.ParsedInt)
		}
		if dst.OverflowFloat(f) {
			return fmt.Errorf("ub: %s overflows %s", n.Value, dst.Type())
		}
		dst.SetFloat(f)
	default:
		return mismatch("number", dst)
	}
	return nil
}

func decodeString(s *parse.StringLit, dst reflect.Value) error {
	if dst.Kind() == reflect.Slice && dst.Type().Elem().Kind() == reflect.Uint8 {
		b, err := base64.StdEncoding.DecodeString(s.Value)
		if err != nil {
			return fmt.Errorf("ub: invalid base64: %w", err)
		}
		dst.SetBytes(b)
		return nil
	}
	if dst.Kind() != reflect.String {
		return mismatch("string", dst)
	}
	dst.SetString(s.Value)
	return nil
}

func decodeArray(a *parse.ArrayLit, dst reflect.Value) error {
	switch dst.Kind() {
	case reflect.Slice:
		out := reflect.MakeSlice(dst.Type(), len(a.Elements), len(a.Elements))
		for i, el := range a.Elements {
			if err := decodeValue(el, out.Index(i)); err != nil {
				return err
			}
		}
		dst.Set(out)
		return nil
	case reflect.Array:
		if len(a.Elements) > dst.Len() {
			return fmt.Errorf(
				"ub: array of length %d does not fit %s",
				len(a.Elements), dst.Type())
		}
		for i, el := range a.Elements {
			if err := decodeValue(el, dst.Index(i)); err != nil {
				return err
			}
		}
		return nil
	}
	return mismatch("array", dst)
}

func decodeObject(o *parse.ObjectLit, dst reflect.Value) error {
	switch dst.Kind() {
	case reflect.Map:
		if dst.Type().Key().Kind() != reflect.String {
			return fmt.Errorf("ub: map key must be string, got %s", dst.Type().Key())
		}
		m := reflect.MakeMapWithSize(dst.Type(), len(o.Fields))
		for _, f := range o.Fields {
			v := reflect.New(dst.Type().Elem()).Elem()
			if err := decodeValue(f.Value, v); err != nil {
				return err
			}
			m.SetMapIndex(reflect.ValueOf(fieldKeyName(f)), v)
		}
		dst.Set(m)
		return nil
	case reflect.Struct:
		return decodeStruct(o, dst)
	}
	return mismatch("object", dst)
}

func decodeStruct(o *parse.ObjectLit, dst reflect.Value) error {
	fields := structFieldsByName(dst.Type())
	for _, f := range o.Fields {
		name := fieldKeyName(f)
		idx, ok := fields[name]
		if !ok {
			continue
		}
		if err := decodeValue(f.Value, dst.Field(idx)); err != nil {
			return err
		}
	}
	return nil
}

func structFieldsByName(t reflect.Type) map[string]int {
	out := make(map[string]int, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if !sf.IsExported() {
			continue
		}
		name, _, skip := parseTag(sf.Tag.Get("ub"), sf.Name)
		if skip {
			continue
		}
		out[name] = i
	}
	return out
}

func fieldKeyName(f *parse.Field) string {
	if f.Key.Kind == parse.FieldString {
		return f.Key.String
	}
	return f.Key.Name
}

func naturalValue(e parse.Expr) (any, error) {
	switch n := e.(type) {
	case *parse.NullLit:
		return nil, nil
	case *parse.BoolLit:
		return n.Value, nil
	case *parse.NumberLit:
		if n.IsFloat {
			return n.ParsedFloat, nil
		}
		return n.ParsedInt, nil
	case *parse.StringLit:
		return n.Value, nil
	case *parse.ArrayLit:
		out := make([]any, len(n.Elements))
		for i, el := range n.Elements {
			v, err := naturalValue(el)
			if err != nil {
				return nil, err
			}
			out[i] = v
		}
		return out, nil
	case *parse.ObjectLit:
		out := make(map[string]any, len(n.Fields))
		for _, f := range n.Fields {
			v, err := naturalValue(f.Value)
			if err != nil {
				return nil, err
			}
			out[fieldKeyName(f)] = v
		}
		return out, nil
	}
	return nil, fmt.Errorf("ub: cannot decode %T into any", e)
}

func decodeTime(e parse.Expr, dst reflect.Value) error {
	s, ok := e.(*parse.StringLit)
	if !ok {
		return fmt.Errorf("ub: cannot decode %T into time.Time", e)
	}
	t, err := time.Parse(time.RFC3339Nano, s.Value)
	if err != nil {
		return fmt.Errorf("ub: invalid time %q: %w", s.Value, err)
	}
	dst.Set(reflect.ValueOf(t))
	return nil
}

func decodeDuration(e parse.Expr, dst reflect.Value) error {
	s, ok := e.(*parse.StringLit)
	if !ok {
		return fmt.Errorf("ub: cannot decode %T into time.Duration", e)
	}
	d, err := time.ParseDuration(s.Value)
	if err != nil {
		return fmt.Errorf("ub: invalid duration %q: %w", s.Value, err)
	}
	dst.Set(reflect.ValueOf(d))
	return nil
}

func mismatch(have string, dst reflect.Value) error {
	return fmt.Errorf("ub: cannot decode %s into %s", have, dst.Type())
}
