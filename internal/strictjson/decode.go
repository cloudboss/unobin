package strictjson

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
)

type valueKind uint8

const (
	valueNull valueKind = iota
	valueObject
	valueArray
	valueScalar
)

type member struct {
	name  string
	value *jsonValue
}

type jsonValue struct {
	kind     valueKind
	raw      json.RawMessage
	members  []member
	elements []*jsonValue
}

type field struct {
	typeOf   reflect.Type
	required bool
}

type fieldSet struct {
	byName map[string]field
	order  []string
}

var jsonUnmarshalerType = reflect.TypeFor[json.Unmarshaler]()

func Validate(data []byte) error {
	_, err := parse(data, "$")
	return err
}

func Decode(data []byte, destination any) error {
	target := reflect.ValueOf(destination)
	if !target.IsValid() || target.Kind() != reflect.Pointer || target.IsNil() {
		return fmt.Errorf("destination must be a non-nil pointer")
	}

	parsed, err := parse(data, "$")
	if err != nil {
		return err
	}
	if err := validate(parsed, target.Type().Elem(), "$"); err != nil {
		return err
	}
	if err := json.Unmarshal(data, destination); err != nil {
		return fmt.Errorf("$: decode JSON: %w", err)
	}
	return nil
}

func parse(data []byte, path string) (*jsonValue, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("%s: decode JSON: %w", path, err)
	}

	parsed := &jsonValue{raw: append(json.RawMessage(nil), data...)}
	delimiter, composite := token.(json.Delim)
	if !composite {
		if token == nil {
			parsed.kind = valueNull
		} else {
			parsed.kind = valueScalar
		}
		if err := requireEOF(decoder, path); err != nil {
			return nil, err
		}
		return parsed, nil
	}

	switch delimiter {
	case '{':
		parsed.kind = valueObject
		seen := map[string]bool{}
		for decoder.More() {
			nameToken, err := decoder.Token()
			if err != nil {
				return nil, fmt.Errorf("%s: decode member name: %w", path, err)
			}
			name, ok := nameToken.(string)
			if !ok {
				return nil, fmt.Errorf("%s: member name is not a string", path)
			}
			memberPath := path + "." + name
			if seen[name] {
				return nil, fmt.Errorf("%s: duplicate member", memberPath)
			}
			seen[name] = true

			var raw json.RawMessage
			if err := decoder.Decode(&raw); err != nil {
				return nil, fmt.Errorf("%s: decode value: %w", memberPath, err)
			}
			child, err := parse(raw, memberPath)
			if err != nil {
				return nil, err
			}
			parsed.members = append(parsed.members, member{name: name, value: child})
		}
	case '[':
		parsed.kind = valueArray
		for decoder.More() {
			index := len(parsed.elements)
			elementPath := fmt.Sprintf("%s[%d]", path, index)
			var raw json.RawMessage
			if err := decoder.Decode(&raw); err != nil {
				return nil, fmt.Errorf("%s: decode value: %w", elementPath, err)
			}
			child, err := parse(raw, elementPath)
			if err != nil {
				return nil, err
			}
			parsed.elements = append(parsed.elements, child)
		}
	default:
		return nil, fmt.Errorf("%s: unexpected delimiter %q", path, delimiter)
	}

	if _, err := decoder.Token(); err != nil {
		return nil, fmt.Errorf("%s: decode JSON: %w", path, err)
	}
	if err := requireEOF(decoder, path); err != nil {
		return nil, err
	}
	return parsed, nil
}

func requireEOF(decoder *json.Decoder, path string) error {
	if _, err := decoder.Token(); err == io.EOF {
		return nil
	} else if err != nil {
		return fmt.Errorf("%s: decode JSON: %w", path, err)
	}
	return fmt.Errorf("%s: unexpected value after JSON value", path)
}

func validate(parsed *jsonValue, typeOf reflect.Type, path string) error {
	for typeOf.Kind() == reflect.Pointer {
		if parsed.kind == valueNull {
			return fmt.Errorf("%s: null is not allowed", path)
		}
		typeOf = typeOf.Elem()
	}
	if typeOf.Kind() == reflect.Interface {
		return validateScalar(parsed.raw, typeOf, path)
	}
	if parsed.kind == valueNull {
		return fmt.Errorf("%s: null is not allowed", path)
	}
	if reflect.PointerTo(typeOf).Implements(jsonUnmarshalerType) {
		return validateCustomValue(parsed.raw, typeOf, path)
	}
	if typeOf.Kind() == reflect.Slice && typeOf.Elem().Kind() == reflect.Uint8 {
		return validateScalar(parsed.raw, typeOf, path)
	}

	switch typeOf.Kind() {
	case reflect.Struct:
		return validateStruct(parsed, typeOf, path)
	case reflect.Array, reflect.Slice:
		return validateArray(parsed, typeOf.Elem(), path)
	case reflect.Map:
		return validateMap(parsed, typeOf, path)
	default:
		return validateScalar(parsed.raw, typeOf, path)
	}
}

func validateCustomValue(data []byte, typeOf reflect.Type, path string) error {
	destination := reflect.New(typeOf).Interface()
	if err := json.Unmarshal(data, destination); err != nil {
		return prefixPath(path, err)
	}
	return nil
}

func validateStruct(parsed *jsonValue, typeOf reflect.Type, path string) error {
	if parsed.kind != valueObject {
		return fmt.Errorf("%s: expected object", path)
	}
	fields := fieldsFor(typeOf)
	present := make(map[string]bool, len(parsed.members))
	for _, item := range parsed.members {
		field, ok := fields.byName[item.name]
		memberPath := path + "." + item.name
		if !ok {
			return fmt.Errorf("%s: unknown member", memberPath)
		}
		present[item.name] = true
		if err := validate(item.value, field.typeOf, memberPath); err != nil {
			return err
		}
	}
	for _, name := range fields.order {
		field := fields.byName[name]
		if field.required && !present[name] {
			return fmt.Errorf("%s.%s: member is required", path, name)
		}
	}
	return nil
}

func fieldsFor(typeOf reflect.Type) fieldSet {
	fields := fieldSet{
		byName: make(map[string]field, typeOf.NumField()),
		order:  make([]string, 0, typeOf.NumField()),
	}
	for structField := range typeOf.Fields() {
		if !structField.IsExported() {
			continue
		}
		name, options, _ := strings.Cut(structField.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = structField.Name
		}
		fields.byName[name] = field{
			typeOf:   structField.Type,
			required: !jsonOption(options, "omitempty"),
		}
		fields.order = append(fields.order, name)
	}
	return fields
}

func jsonOption(options, expected string) bool {
	for option := range strings.SplitSeq(options, ",") {
		if option == expected {
			return true
		}
	}
	return false
}

func validateArray(parsed *jsonValue, elementType reflect.Type, path string) error {
	if parsed.kind != valueArray {
		return fmt.Errorf("%s: expected array", path)
	}
	for i, element := range parsed.elements {
		if err := validate(element, elementType, fmt.Sprintf("%s[%d]", path, i)); err != nil {
			return err
		}
	}
	return nil
}

func validateMap(parsed *jsonValue, typeOf reflect.Type, path string) error {
	if typeOf.Key().Kind() != reflect.String {
		return fmt.Errorf("%s: map key must be a string", path)
	}
	if parsed.kind != valueObject {
		return fmt.Errorf("%s: expected object", path)
	}
	for _, item := range parsed.members {
		if err := validate(item.value, typeOf.Elem(), path+"."+item.name); err != nil {
			return err
		}
	}
	return nil
}

func validateScalar(data []byte, typeOf reflect.Type, path string) error {
	destination := reflect.New(typeOf)
	if err := json.Unmarshal(data, destination.Interface()); err != nil {
		return fmt.Errorf("%s: invalid value: %w", path, err)
	}
	switch typeOf.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		canonical := strconv.FormatInt(destination.Elem().Int(), 10)
		if string(data) != canonical {
			return fmt.Errorf("%s: noncanonical integer %q", path, data)
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		canonical := strconv.FormatUint(destination.Elem().Uint(), 10)
		if string(data) != canonical {
			return fmt.Errorf("%s: noncanonical integer %q", path, data)
		}
	case reflect.Float32, reflect.Float64:
		canonical := strconv.FormatFloat(
			destination.Elem().Float(),
			'g',
			-1,
			typeOf.Bits(),
		)
		if string(data) != canonical {
			return fmt.Errorf("%s: noncanonical number %q", path, data)
		}
	}
	return nil
}

func prefixPath(path string, err error) error {
	message := err.Error()
	if after, ok := strings.CutPrefix(message, "$"); ok {
		return fmt.Errorf("%s%s", path, after)
	}
	return fmt.Errorf("%s: %w", path, err)
}
