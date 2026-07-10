package cmdout

import (
	"bytes"
	"encoding"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/cloudboss/unobin/pkg/encoding/ub"
)

var (
	jsonMarshalerType   = reflect.TypeFor[json.Marshaler]()
	textMarshalerType   = reflect.TypeFor[encoding.TextMarshaler]()
	unobinMarshalerType = reflect.TypeFor[ub.Marshaler]()
	jsonNumberType      = reflect.TypeFor[json.Number]()
	timeType            = reflect.TypeFor[time.Time]()
	durationType        = reflect.TypeFor[time.Duration]()
)

func EncodeDocument(format Format, value any) ([]byte, error) {
	return encodeMachine(format, value)
}

func WriteDocument(out io.Writer, format Format, value any) error {
	encoded, err := EncodeDocument(format, value)
	if err != nil {
		return err
	}
	return writeEncoded(out, encoded)
}

func EncodeRecord(format Format, value any) ([]byte, error) {
	return encodeMachine(format, value)
}

func WriteRecord(out io.Writer, format Format, value any) error {
	encoded, err := EncodeRecord(format, value)
	if err != nil {
		return err
	}
	return writeEncoded(out, encoded)
}

func encodeMachine(format Format, value any) ([]byte, error) {
	if !format.Machine() {
		return nil, fmt.Errorf("cmdout: format %q is not a machine format", format)
	}
	if err := validateMachineValue(reflect.ValueOf(value), "$", map[valueIdentity]bool{}); err != nil {
		return nil, err
	}
	switch format {
	case FormatJSON:
		var out bytes.Buffer
		encoder := json.NewEncoder(&out)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(value); err != nil {
			return nil, err
		}
		return out.Bytes(), nil
	case FormatUnobin:
		out, err := ub.Marshal(value)
		if err != nil {
			return nil, err
		}
		return append(out, '\n'), nil
	default:
		panic("unreachable machine format")
	}
}

func writeEncoded(out io.Writer, encoded []byte) error {
	written, err := out.Write(encoded)
	if err != nil {
		return err
	}
	if written != len(encoded) {
		return io.ErrShortWrite
	}
	return nil
}

type valueIdentity struct {
	typ     reflect.Type
	pointer uintptr
}

func validateMachineValue(
	value reflect.Value,
	path string,
	active map[valueIdentity]bool,
) error {
	if !value.IsValid() {
		return nil
	}
	if value.Kind() == reflect.Interface {
		if value.IsNil() {
			return nil
		}
		return validateMachineValue(value.Elem(), path, active)
	}
	typ := value.Type()
	if typ == timeType || typ.Kind() == reflect.Pointer && typ.Elem() == timeType {
		return nil
	}
	if typ == jsonNumberType {
		return machineValueError(path, "json.Number must be converted to int64 or float64")
	}
	if typ == durationType {
		return machineValueError(path, "time.Duration must be converted to a public value")
	}
	if hasCustomMarshaler(typ) {
		return machineValueError(path, "type %s implements format-specific marshaling", typ)
	}

	switch value.Kind() {
	case reflect.Bool, reflect.String:
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Uintptr:
		return nil
	case reflect.Float32, reflect.Float64:
		if math.IsNaN(value.Float()) || math.IsInf(value.Float(), 0) {
			return machineValueError(path, "number must be finite")
		}
		return nil
	case reflect.Pointer:
		if value.IsNil() {
			return nil
		}
		return validateReference(value, path, active, func() error {
			return validateMachineValue(value.Elem(), path, active)
		})
	case reflect.Slice:
		if value.IsNil() || value.Len() == 0 {
			return nil
		}
		return validateReference(value, path, active, func() error {
			return validateSequence(value, path, active)
		})
	case reflect.Array:
		return validateSequence(value, path, active)
	case reflect.Map:
		return validateMap(value, path, active)
	case reflect.Struct:
		return validateStruct(value, path, active)
	case reflect.Chan, reflect.Complex64, reflect.Complex128, reflect.Func,
		reflect.UnsafePointer:
		return machineValueError(path, "type %s is not supported", typ)
	default:
		return machineValueError(path, "type %s is not supported", typ)
	}
}

func validateReference(
	value reflect.Value,
	path string,
	active map[valueIdentity]bool,
	visit func() error,
) error {
	identity := valueIdentity{typ: value.Type(), pointer: value.Pointer()}
	if active[identity] {
		return machineValueError(path, "cycles are not supported")
	}
	active[identity] = true
	defer delete(active, identity)
	return visit()
}

func validateSequence(value reflect.Value, path string, active map[valueIdentity]bool) error {
	for i := range value.Len() {
		if err := validateMachineValue(
			value.Index(i), fmt.Sprintf("%s[%d]", path, i), active,
		); err != nil {
			return err
		}
	}
	return nil
}

func validateMap(value reflect.Value, path string, active map[valueIdentity]bool) error {
	if value.Type().Key().Kind() != reflect.String {
		return machineValueError(path, "map key type %s is not string", value.Type().Key())
	}
	if value.IsNil() || value.Len() == 0 {
		return nil
	}
	return validateReference(value, path, active, func() error {
		keys := value.MapKeys()
		slices.SortFunc(keys, func(a, b reflect.Value) int {
			return strings.Compare(a.String(), b.String())
		})
		normalized := map[string]string{}
		for _, key := range keys {
			raw := key.String()
			valid := strings.ToValidUTF8(raw, "\uFFFD")
			if previous, ok := normalized[valid]; ok && previous != raw {
				return machineValueError(
					path,
					"map keys %q and %q collide after invalid UTF-8 replacement",
					previous,
					raw,
				)
			}
			normalized[valid] = raw
		}
		for _, key := range keys {
			if err := validateMachineValue(
				value.MapIndex(key), fmt.Sprintf("%s[%q]", path, key.String()), active,
			); err != nil {
				return err
			}
		}
		return nil
	})
}

func validateStruct(value reflect.Value, path string, active map[valueIdentity]bool) error {
	typ := value.Type()
	for i := range typ.NumField() {
		field := typ.Field(i)
		if !field.IsExported() || skippedByBothEncoders(field) {
			continue
		}
		if err := validateMachineValue(
			value.Field(i), path+"."+field.Name, active,
		); err != nil {
			return err
		}
	}
	return nil
}

func skippedByBothEncoders(field reflect.StructField) bool {
	return field.Tag.Get("json") == "-" && field.Tag.Get("ub") == "-"
}

func hasCustomMarshaler(typ reflect.Type) bool {
	if typ.Implements(jsonMarshalerType) || typ.Implements(textMarshalerType) ||
		typ.Implements(unobinMarshalerType) {
		return true
	}
	if typ.Kind() == reflect.Pointer {
		return false
	}
	pointer := reflect.PointerTo(typ)
	return pointer.Implements(jsonMarshalerType) ||
		pointer.Implements(textMarshalerType) ||
		pointer.Implements(unobinMarshalerType)
}

func machineValueError(path, format string, args ...any) error {
	return fmt.Errorf("machine value %s: %s", path, fmt.Sprintf(format, args...))
}
