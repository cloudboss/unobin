package runtime

import (
	"fmt"
	"math"
	"reflect"
	"slices"

	"github.com/cloudboss/unobin/pkg/asset"
	"github.com/cloudboss/unobin/pkg/encoding/ub"
)

func prepareResourceInputs[In any](values map[string]any) (EncodedValue, In, error) {
	var zero In
	root := reflect.TypeFor[In]()
	if err := validateResourceValueRoot(root, false); err != nil {
		return EncodedValue{}, zero, err
	}
	encoded, err := encodeResourceObject(root, values, "")
	if err != nil {
		return EncodedValue{}, zero, err
	}
	inputs, err := decodeResourceInputValue[In](encoded, true)
	if err != nil {
		return EncodedValue{}, zero, err
	}
	return encoded, inputs, nil
}

func decodeResourceInputs[In any](value EncodedValue) (In, error) {
	return decodeResourceInputValue[In](value, false)
}

func encodeResourceOutputs[Out any](outputs Out) (EncodedValue, error) {
	root := reflect.TypeFor[Out]()
	if err := validateResourceValueRoot(root, true); err != nil {
		return EncodedValue{}, err
	}
	value := reflect.ValueOf(outputs)
	if value.IsNil() {
		return EncodedValue{}, fmt.Errorf("resource outputs must not be nil")
	}
	return encodeResourceValue(root.Elem(), value.Elem().Interface(), "")
}

func decodeResourceOutputs[Out any](value EncodedValue) (Out, error) {
	var zero Out
	root := reflect.TypeFor[Out]()
	if err := validateResourceValueRoot(root, true); err != nil {
		return zero, err
	}
	decoded, present, err := decodeResourceValue(root.Elem(), value, false, false, "")
	if err != nil {
		return zero, err
	}
	if !present {
		return zero, fmt.Errorf("resource outputs must be an object")
	}

	pointer := reflect.New(root.Elem())
	fields, ok := decoded.(map[string]any)
	if !ok {
		return zero, fmt.Errorf("resource outputs must be an object")
	}
	if err := Decode(pointer.Interface(), fields); err != nil {
		return zero, fmt.Errorf("decode resource outputs: %w", err)
	}
	if pointer.Type() != root {
		pointer = pointer.Convert(root)
	}
	return pointer.Interface().(Out), nil
}

func decodeResourceInputValue[In any](value EncodedValue, allowPending bool) (In, error) {
	var inputs In
	root := reflect.TypeFor[In]()
	if err := validateResourceValueRoot(root, false); err != nil {
		return inputs, err
	}
	decoded, present, err := decodeResourceValue(root, value, allowPending, false, "")
	if err != nil {
		return inputs, err
	}
	if !present {
		return inputs, fmt.Errorf("resource inputs must be an object")
	}
	fields, ok := decoded.(map[string]any)
	if !ok {
		return inputs, fmt.Errorf("resource inputs must be an object")
	}
	if err := Decode(&inputs, fields); err != nil {
		return inputs, fmt.Errorf("decode resource inputs: %w", err)
	}
	return inputs, nil
}

func validateResourceValueRoot(root reflect.Type, output bool) error {
	if output {
		if root.Kind() != reflect.Pointer || root.Elem().Kind() != reflect.Struct {
			return fmt.Errorf("output root must be a pointer to a struct; got %s", root)
		}
		root = root.Elem()
	} else if root.Kind() != reflect.Struct {
		return fmt.Errorf("input root must be a struct; got %s", root)
	}
	fields, err := resourceStructFields(root)
	if err != nil {
		return err
	}
	for _, field := range fields {
		if _, err := fieldValueSchema(
			field.typ,
			map[reflect.Type]bool{root: true},
		); err != nil {
			return err
		}
	}
	return nil
}

type resourceStructField struct {
	index int
	name  string
	typ   reflect.Type
}

func resourceStructFields(root reflect.Type) ([]resourceStructField, error) {
	fields := make([]resourceStructField, 0, root.NumField())
	names := map[string]bool{}
	for i := range root.NumField() {
		field := root.Field(i)
		if !field.IsExported() {
			continue
		}
		tag := ub.ParseTag(field.Tag.Get("ub"))
		if tag.Skip {
			continue
		}
		name := tag.FieldName(field.Name)
		if names[name] {
			return nil, fmt.Errorf("field name %q is ambiguous in %s", name, root)
		}
		names[name] = true
		if field.Type.Size() == 0 {
			return nil, fmt.Errorf("zero-sized field %q in %s", name, root)
		}
		fields = append(fields, resourceStructField{
			index: i,
			name:  name,
			typ:   field.Type,
		})
	}
	return fields, nil
}

func encodeResourceObject(
	expected reflect.Type,
	values map[string]any,
	path string,
) (EncodedValue, error) {
	fields, err := resourceStructFields(expected)
	if err != nil {
		return EncodedValue{}, err
	}
	known := make(map[string]bool, len(fields))
	encoded := make(map[string]EncodedValue, len(fields))
	for _, field := range fields {
		known[field.name] = true
		value, ok := values[field.name]
		if !ok {
			encoded[field.name] = AbsentValue()
			continue
		}
		fieldPath := resourceValuePath(path, field.name)
		encoded[field.name], err = encodeResourceValue(field.typ, value, fieldPath)
		if err != nil {
			return EncodedValue{}, err
		}
	}
	if unknown := firstUnknownResourceField(values, known); unknown != "" {
		return EncodedValue{}, resourceValueError(
			path,
			"unknown field %q",
			unknown,
		)
	}
	return ObjectValue(encoded)
}

func encodeResourceStruct(
	expected reflect.Type,
	value reflect.Value,
	path string,
) (EncodedValue, error) {
	fields, err := resourceStructFields(expected)
	if err != nil {
		return EncodedValue{}, err
	}
	encoded := make(map[string]EncodedValue, len(fields))
	for _, field := range fields {
		fieldPath := resourceValuePath(path, field.name)
		encoded[field.name], err = encodeResourceValue(
			field.typ,
			value.Field(field.index).Interface(),
			fieldPath,
		)
		if err != nil {
			return EncodedValue{}, err
		}
	}
	return ObjectValue(encoded)
}

func encodeResourceValue(
	expected reflect.Type,
	input any,
	path string,
) (EncodedValue, error) {
	if pending, ok := input.(PendingValue); ok {
		value, err := PendingEncodedValue(pending.Refs)
		if err != nil {
			return EncodedValue{}, resourceValueError(path, "%v", err)
		}
		return value, nil
	}
	if expected.Kind() == reflect.Slice && expected.Elem().Kind() == reflect.Uint8 {
		if token, ok := resourceAssetToken(input); ok {
			return StringValue(token), nil
		}
	}
	if input == nil {
		if expected.Kind() == reflect.Pointer {
			return NullValue(), nil
		}
		return EncodedValue{}, resourceValueError(
			path,
			"null is invalid for %s",
			expected,
		)
	}

	value := reflect.ValueOf(input)
	for value.Kind() == reflect.Interface {
		if value.IsNil() {
			return encodeResourceValue(expected, nil, path)
		}
		value = value.Elem()
	}
	if expected.Kind() == reflect.Pointer {
		if value.Kind() == reflect.Pointer {
			if value.IsNil() {
				return NullValue(), nil
			}
			value = value.Elem()
		}
		return encodeResourceValue(expected.Elem(), value.Interface(), path)
	}
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return EncodedValue{}, resourceValueError(
				path,
				"null is invalid for %s",
				expected,
			)
		}
		value = value.Elem()
	}

	switch expected.Kind() {
	case reflect.Bool:
		if value.Kind() != reflect.Bool {
			return EncodedValue{}, resourceExpectedValueError(path, "boolean")
		}
		return BooleanValue(value.Bool()), nil
	case reflect.String:
		if value.Kind() != reflect.String {
			return EncodedValue{}, resourceExpectedValueError(path, "string")
		}
		return StringValue(value.String()), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		integer, err := resourceInteger(expected, value, path)
		if err != nil {
			return EncodedValue{}, err
		}
		return IntegerValue(integer), nil
	case reflect.Float32, reflect.Float64:
		number, err := resourceNumber(expected, value, path)
		if err != nil {
			return EncodedValue{}, err
		}
		result, err := NumberValue(number)
		if err != nil {
			return EncodedValue{}, resourceValueError(path, "%v", err)
		}
		return result, nil
	case reflect.Array, reflect.Slice:
		return encodeResourceList(expected, value, path)
	case reflect.Map:
		return encodeResourceMap(expected, value, path)
	case reflect.Struct:
		switch value.Kind() {
		case reflect.Map:
			mapped, err := resourceStringMap(value, path)
			if err != nil {
				return EncodedValue{}, err
			}
			return encodeResourceObject(expected, mapped, path)
		case reflect.Struct:
			if value.Type() != expected {
				return EncodedValue{}, resourceExpectedValueError(path, expected.String())
			}
			return encodeResourceStruct(expected, value, path)
		default:
			return EncodedValue{}, resourceExpectedValueError(path, "object")
		}
	default:
		return EncodedValue{}, resourceValueError(path, "unsupported type %s", expected)
	}
}

func encodeResourceList(
	expected reflect.Type,
	value reflect.Value,
	path string,
) (EncodedValue, error) {
	if value.Kind() != reflect.Array && value.Kind() != reflect.Slice {
		return EncodedValue{}, resourceExpectedValueError(path, "list")
	}
	if expected.Kind() == reflect.Array && value.Len() != expected.Len() {
		return EncodedValue{}, resourceValueError(
			path,
			"expected list length %d",
			expected.Len(),
		)
	}
	items := make([]EncodedValue, value.Len())
	for i := range value.Len() {
		itemPath := fmt.Sprintf("%s[%d]", path, i)
		item, err := encodeResourceValue(expected.Elem(), value.Index(i).Interface(), itemPath)
		if err != nil {
			return EncodedValue{}, err
		}
		items[i] = item
	}
	return ListValue(items)
}

func encodeResourceMap(
	expected reflect.Type,
	value reflect.Value,
	path string,
) (EncodedValue, error) {
	if value.Kind() != reflect.Map || value.Type().Key().Kind() != reflect.String {
		return EncodedValue{}, resourceExpectedValueError(path, "map")
	}
	keys := value.MapKeys()
	slices.SortFunc(keys, func(a, b reflect.Value) int {
		switch {
		case a.String() < b.String():
			return -1
		case a.String() > b.String():
			return 1
		default:
			return 0
		}
	})
	entries := make(map[string]EncodedValue, len(keys))
	for _, key := range keys {
		entryPath := resourceValuePath(path, key.String())
		entry, err := encodeResourceValue(
			expected.Elem(),
			value.MapIndex(key).Interface(),
			entryPath,
		)
		if err != nil {
			return EncodedValue{}, err
		}
		entries[key.String()] = entry
	}
	return MapValue(entries)
}

func resourceInteger(
	expected reflect.Type,
	value reflect.Value,
	path string,
) (int64, error) {
	var integer int64
	switch value.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		integer = value.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		unsigned := value.Uint()
		if unsigned > math.MaxInt64 {
			return 0, resourceValueError(path, "integer exceeds int64")
		}
		integer = int64(unsigned)
	default:
		return 0, resourceExpectedValueError(path, "integer")
	}

	zero := reflect.New(expected).Elem()
	if expected.Kind() >= reflect.Int && expected.Kind() <= reflect.Int64 {
		if zero.OverflowInt(integer) {
			return 0, resourceValueError(path, "integer overflows %s", expected)
		}
		return integer, nil
	}
	if integer < 0 || zero.OverflowUint(uint64(integer)) {
		return 0, resourceValueError(path, "integer overflows %s", expected)
	}
	return integer, nil
}

func resourceNumber(
	expected reflect.Type,
	value reflect.Value,
	path string,
) (float64, error) {
	var number float64
	switch value.Kind() {
	case reflect.Float32, reflect.Float64:
		number = value.Float()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		number = float64(value.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		number = float64(value.Uint())
	default:
		return 0, resourceExpectedValueError(path, "number")
	}
	if math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, resourceValueError(path, "number must be finite")
	}
	zero := reflect.New(expected).Elem()
	if zero.OverflowFloat(number) {
		return 0, resourceValueError(path, "number overflows %s", expected)
	}
	if expected.Kind() == reflect.Float32 {
		number = float64(float32(number))
	}
	return number, nil
}

func decodeResourceValue(
	expected reflect.Type,
	value EncodedValue,
	allowPending bool,
	allowAbsent bool,
	path string,
) (any, bool, error) {
	switch value.Kind() {
	case EncodedValueAbsent:
		if allowAbsent {
			return nil, false, nil
		}
		return nil, false, resourceValueError(path, "absent value is invalid here")
	case EncodedValuePending:
		if !allowPending {
			return nil, false, resourceValueError(path, "pending value is not concrete")
		}
		return resourceZeroValue(expected), true, nil
	case EncodedValueNull:
		if expected.Kind() != reflect.Pointer {
			return nil, false, resourceValueError(
				path,
				"null is invalid for %s",
				expected,
			)
		}
		return nil, true, nil
	}

	if expected.Kind() == reflect.Pointer {
		decoded, _, err := decodeResourceValue(
			expected.Elem(),
			value,
			allowPending,
			false,
			path,
		)
		return decoded, true, err
	}

	switch expected.Kind() {
	case reflect.Bool:
		decoded, ok := value.Boolean()
		if !ok {
			return nil, false, resourceExpectedValueError(path, "boolean")
		}
		return decoded, true, nil
	case reflect.String:
		decoded, ok := value.String()
		if !ok {
			return nil, false, resourceExpectedValueError(path, "string")
		}
		return decoded, true, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		decoded, ok := value.Integer()
		if !ok {
			return nil, false, resourceExpectedValueError(path, "integer")
		}
		integer, err := resourceInteger(expected, reflect.ValueOf(decoded), path)
		return integer, true, err
	case reflect.Float32, reflect.Float64:
		decoded, ok := value.Number()
		if !ok {
			return nil, false, resourceExpectedValueError(path, "number")
		}
		number, err := resourceNumber(expected, reflect.ValueOf(decoded), path)
		return number, true, err
	case reflect.Array, reflect.Slice:
		return decodeResourceList(expected, value, allowPending, path)
	case reflect.Map:
		return decodeResourceMap(expected, value, allowPending, path)
	case reflect.Struct:
		return decodeResourceObject(expected, value, allowPending, path)
	default:
		return nil, false, resourceValueError(path, "unsupported type %s", expected)
	}
}

func decodeResourceList(
	expected reflect.Type,
	value EncodedValue,
	allowPending bool,
	path string,
) (any, bool, error) {
	items, ok := value.Items()
	if !ok {
		return nil, false, resourceExpectedValueError(path, "list")
	}
	if expected.Kind() == reflect.Array && len(items) != expected.Len() {
		return nil, false, resourceValueError(
			path,
			"expected list length %d",
			expected.Len(),
		)
	}
	decoded := make([]any, len(items))
	for i, item := range items {
		itemPath := fmt.Sprintf("%s[%d]", path, i)
		itemValue, present, err := decodeResourceValue(
			expected.Elem(),
			item,
			allowPending,
			false,
			itemPath,
		)
		if err != nil {
			return nil, false, err
		}
		if !present {
			return nil, false, resourceValueError(itemPath, "absent value is invalid here")
		}
		decoded[i] = itemValue
	}
	return decoded, true, nil
}

func decodeResourceMap(
	expected reflect.Type,
	value EncodedValue,
	allowPending bool,
	path string,
) (any, bool, error) {
	entries, ok := value.MapEntries()
	if !ok {
		return nil, false, resourceExpectedValueError(path, "map")
	}
	decoded := make(map[string]any, len(entries))
	for _, key := range sortedResourceKeys(entries) {
		entryPath := resourceValuePath(path, key)
		entry, present, err := decodeResourceValue(
			expected.Elem(),
			entries[key],
			allowPending,
			false,
			entryPath,
		)
		if err != nil {
			return nil, false, err
		}
		if !present {
			return nil, false, resourceValueError(
				entryPath,
				"absent value is invalid here",
			)
		}
		decoded[key] = entry
	}
	return decoded, true, nil
}

func decodeResourceObject(
	expected reflect.Type,
	value EncodedValue,
	allowPending bool,
	path string,
) (any, bool, error) {
	encoded, ok := value.ObjectFields()
	if !ok {
		return nil, false, resourceExpectedValueError(path, "object")
	}
	fields, err := resourceStructFields(expected)
	if err != nil {
		return nil, false, err
	}
	known := make(map[string]bool, len(fields))
	decoded := make(map[string]any, len(fields))
	for _, field := range fields {
		known[field.name] = true
		fieldValue, ok := encoded[field.name]
		if !ok {
			return nil, false, resourceValueError(
				path,
				"field %q is missing",
				field.name,
			)
		}
		fieldPath := resourceValuePath(path, field.name)
		result, present, err := decodeResourceValue(
			field.typ,
			fieldValue,
			allowPending,
			true,
			fieldPath,
		)
		if err != nil {
			return nil, false, err
		}
		if present {
			decoded[field.name] = result
		}
	}
	if unknown := firstUnknownResourceField(encoded, known); unknown != "" {
		return nil, false, resourceValueError(
			path,
			"unknown field %q",
			unknown,
		)
	}
	return decoded, true, nil
}

func resourceStringMap(value reflect.Value, path string) (map[string]any, error) {
	if value.Type().Key().Kind() != reflect.String {
		return nil, resourceExpectedValueError(path, "object")
	}
	out := make(map[string]any, value.Len())
	iterator := value.MapRange()
	for iterator.Next() {
		out[iterator.Key().String()] = iterator.Value().Interface()
	}
	return out, nil
}

func firstUnknownResourceField[T any](values map[string]T, known map[string]bool) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		if !known[key] {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	if len(keys) == 0 {
		return ""
	}
	return keys[0]
}

func sortedResourceKeys(values map[string]EncodedValue) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func resourceZeroValue(expected reflect.Type) any {
	if expected.Kind() == reflect.Pointer || expected.Kind() == reflect.Map ||
		expected.Kind() == reflect.Slice {
		return nil
	}
	return canonicalize(reflect.Zero(expected))
}

func resourceValuePath(parent, name string) string {
	if parent == "" {
		return name
	}
	return parent + "." + name
}

func resourceExpectedValueError(path, expected string) error {
	return resourceValueError(path, "expected %s", expected)
}

func resourceValueError(path, format string, args ...any) error {
	message := fmt.Sprintf(format, args...)
	if path == "" {
		return fmt.Errorf("%s", message)
	}
	return fmt.Errorf("field %q: %s", path, message)
}

func resourceAssetToken(value any) (string, bool) {
	reflected := reflect.ValueOf(value)
	if !reflected.IsValid() || reflected.Kind() != reflect.String {
		return "", false
	}
	token := reflected.String()
	_, ok := asset.ParseReference(token)
	return token, ok
}
