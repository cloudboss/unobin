package ub

import (
	"cmp"
	"encoding/base64"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Marshaler is implemented by types that supply their own unobin
// representation. The returned bytes are a single UB expression
// inserted verbatim into the output; callers control quoting,
// escaping, and structure.
type Marshaler interface {
	MarshalUB() ([]byte, error)
}

var (
	marshalerType = reflect.TypeFor[Marshaler]()
	timeTimeType  = reflect.TypeFor[time.Time]()
	durationType  = reflect.TypeFor[time.Duration]()
)

// Marshal returns v as a one-line UB expression.
func Marshal(v any) ([]byte, error) {
	e := &encoder{}
	if err := e.encode(reflect.ValueOf(v)); err != nil {
		return nil, err
	}
	return e.buf, nil
}

// MarshalIndent returns v as a multi-line UB expression. prefix is
// prepended to every line except the first; indent is the per-depth
// indent string. Atomic values render the same in either form.
func MarshalIndent(v any, prefix, indent string) ([]byte, error) {
	e := &encoder{prefix: prefix, indent: indent, pretty: true}
	if err := e.encode(reflect.ValueOf(v)); err != nil {
		return nil, err
	}
	return e.buf, nil
}

type encoder struct {
	buf    []byte
	prefix string
	indent string
	pretty bool
	depth  int
}

func (e *encoder) encode(v reflect.Value) error {
	if !v.IsValid() {
		e.buf = append(e.buf, "null"...)
		return nil
	}
	if v.Type().Implements(marshalerType) {
		return e.encodeMarshaler(v)
	}
	switch v.Type() {
	case timeTimeType:
		return e.encodeString(v.Interface().(time.Time).Format(time.RFC3339Nano))
	case durationType:
		return e.encodeDuration(v.Interface().(time.Duration))
	}
	return e.encodeKind(v)
}

func (e *encoder) encodeMarshaler(v reflect.Value) error {
	if (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) && v.IsNil() {
		e.buf = append(e.buf, "null"...)
		return nil
	}
	b, err := v.Interface().(Marshaler).MarshalUB()
	if err != nil {
		return err
	}
	e.buf = append(e.buf, b...)
	return nil
}

func (e *encoder) encodeKind(v reflect.Value) error {
	switch v.Kind() {
	case reflect.Bool:
		if v.Bool() {
			e.buf = append(e.buf, "true"...)
		} else {
			e.buf = append(e.buf, "false"...)
		}
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		e.buf = strconv.AppendInt(e.buf, v.Int(), 10)
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		e.buf = strconv.AppendUint(e.buf, v.Uint(), 10)
		return nil
	case reflect.Float32, reflect.Float64:
		bits := 64
		if v.Kind() == reflect.Float32 {
			bits = 32
		}
		e.buf = strconv.AppendFloat(e.buf, v.Float(), 'g', -1, bits)
		return nil
	case reflect.String:
		return e.encodeString(v.String())
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			e.buf = append(e.buf, "null"...)
			return nil
		}
		return e.encode(v.Elem())
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return e.encodeBytes(v.Bytes())
		}
		return e.encodeList(v)
	case reflect.Array:
		return e.encodeList(v)
	case reflect.Map:
		return e.encodeMap(v)
	case reflect.Struct:
		return e.encodeStruct(v)
	}
	return fmt.Errorf("ub: cannot marshal %s", v.Type())
}

func (e *encoder) encodeString(s string) error {
	e.buf = append(e.buf, '\'')
	for _, r := range s {
		switch r {
		case '\'':
			e.buf = append(e.buf, '\\', '\'')
		case '\\':
			e.buf = append(e.buf, '\\', '\\')
		case '\n':
			e.buf = append(e.buf, '\\', 'n')
		case '\r':
			e.buf = append(e.buf, '\\', 'r')
		case '\t':
			e.buf = append(e.buf, '\\', 't')
		default:
			e.buf = append(e.buf, string(r)...)
		}
	}
	e.buf = append(e.buf, '\'')
	return nil
}

func (e *encoder) encodeBytes(b []byte) error {
	return e.encodeString(base64.StdEncoding.EncodeToString(b))
}

// encodeDuration emits the duration's Go string form with the
// non-ASCII micro sign rewritten to 'u' so output stays pure ASCII
// and parses back as a UB string literal.
func (e *encoder) encodeDuration(d time.Duration) error {
	return e.encodeString(strings.ReplaceAll(d.String(), "µ", "u"))
}

func (e *encoder) encodeList(v reflect.Value) error {
	n := v.Len()
	if n == 0 {
		e.buf = append(e.buf, "[]"...)
		return nil
	}
	if e.pretty {
		e.buf = append(e.buf, '[', '\n')
		e.depth++
		for i := range n {
			e.writeIndent()
			if err := e.encode(v.Index(i)); err != nil {
				return err
			}
			e.buf = append(e.buf, ',', '\n')
		}
		e.depth--
		e.writeIndent()
		e.buf = append(e.buf, ']')
		return nil
	}
	e.buf = append(e.buf, '[')
	for i := range n {
		if i > 0 {
			e.buf = append(e.buf, ',', ' ')
		}
		if err := e.encode(v.Index(i)); err != nil {
			return err
		}
	}
	e.buf = append(e.buf, ']')
	return nil
}

func (e *encoder) encodeMap(v reflect.Value) error {
	if v.Len() == 0 {
		e.buf = append(e.buf, "{}"...)
		return nil
	}
	if v.Type().Key().Kind() != reflect.String {
		return fmt.Errorf("ub: map key must be string, got %s", v.Type().Key())
	}
	type kv struct {
		key string
		val reflect.Value
	}
	pairs := make([]kv, 0, v.Len())
	iter := v.MapRange()
	for iter.Next() {
		pairs = append(pairs, kv{key: iter.Key().String(), val: iter.Value()})
	}
	slices.SortFunc(pairs, func(a, b kv) int { return cmp.Compare(a.key, b.key) })

	if e.pretty {
		e.buf = append(e.buf, '{', '\n')
		e.depth++
		for _, p := range pairs {
			e.writeIndent()
			e.buf = append(e.buf, quoteKey(p.key)...)
			e.buf = append(e.buf, ':', ' ')
			if err := e.encode(p.val); err != nil {
				return err
			}
			e.buf = append(e.buf, '\n')
		}
		e.depth--
		e.writeIndent()
		e.buf = append(e.buf, '}')
		return nil
	}
	e.buf = append(e.buf, '{', ' ')
	for i, p := range pairs {
		if i > 0 {
			e.buf = append(e.buf, ',', ' ')
		}
		e.buf = append(e.buf, quoteKey(p.key)...)
		e.buf = append(e.buf, ':', ' ')
		if err := e.encode(p.val); err != nil {
			return err
		}
	}
	e.buf = append(e.buf, ' ', '}')
	return nil
}

func (e *encoder) encodeStruct(v reflect.Value) error {
	type pair struct {
		name string
		val  reflect.Value
	}
	t := v.Type()
	pairs := make([]pair, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if !sf.IsExported() {
			continue
		}
		tag := ParseTag(sf.Tag.Get("ub"))
		if tag.Skip {
			continue
		}
		fv := v.Field(i)
		if tag.Omitempty && isEmptyValue(fv) {
			continue
		}
		pairs = append(pairs, pair{name: tag.FieldName(sf.Name), val: fv})
	}
	if len(pairs) == 0 {
		e.buf = append(e.buf, "{}"...)
		return nil
	}
	if e.pretty {
		e.buf = append(e.buf, '{', '\n')
		e.depth++
		for _, p := range pairs {
			e.writeIndent()
			e.buf = append(e.buf, quoteKey(p.name)...)
			e.buf = append(e.buf, ':', ' ')
			if err := e.encode(p.val); err != nil {
				return err
			}
			e.buf = append(e.buf, '\n')
		}
		e.depth--
		e.writeIndent()
		e.buf = append(e.buf, '}')
		return nil
	}
	e.buf = append(e.buf, '{', ' ')
	for i, p := range pairs {
		if i > 0 {
			e.buf = append(e.buf, ',', ' ')
		}
		e.buf = append(e.buf, quoteKey(p.name)...)
		e.buf = append(e.buf, ':', ' ')
		if err := e.encode(p.val); err != nil {
			return err
		}
	}
	e.buf = append(e.buf, ' ', '}')
	return nil
}

func (e *encoder) writeIndent() {
	e.buf = append(e.buf, e.prefix...)
	for i := 0; i < e.depth; i++ {
		e.buf = append(e.buf, e.indent...)
	}
}

func isEmptyValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Slice, reflect.Map, reflect.String, reflect.Array:
		return v.Len() == 0
	case reflect.Pointer, reflect.Interface:
		return v.IsNil()
	}
	return v.IsZero()
}

func quoteKey(k string) string {
	if isKebabIdent(k) {
		return k
	}
	var b strings.Builder
	b.Grow(len(k) + 2)
	b.WriteByte('\'')
	for _, r := range k {
		switch r {
		case '\'':
			b.WriteString(`\'`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('\'')
	return b.String()
}

func isKebabIdent(s string) bool {
	if s == "" {
		return false
	}
	i := 0
	if s[0] == '@' {
		i = 1
	}
	if i >= len(s) || !isLetter(s[i]) {
		return false
	}
	for j := i + 1; j < len(s); j++ {
		c := s[j]
		if !isLetter(c) && !isDigit(c) && c != '-' {
			return false
		}
	}
	last := s[len(s)-1]
	return isLetter(last) || isDigit(last)
}

func isLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}
