package runtime

import (
	"crypto/sha256"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/cloudboss/unobin/pkg/encoding/ub"
)

type fieldMetadata[Root any] struct {
	index        []int
	path         string
	rootType     reflect.Type
	valueType    reflect.Type
	schemaDigest string
	sensitive    bool
	resolve      func() (resolvedField, error)
}

type resolvedField struct {
	index        []int
	path         string
	rootType     reflect.Type
	valueType    reflect.Type
	schemaDigest string
	sensitive    bool
}

// InputDescriptor identifies one field in a resource input struct.
type InputDescriptor[In, Value any] struct {
	field fieldMetadata[In]
}

// OutputDescriptor identifies one field in a resource output struct.
type OutputDescriptor[Out, Value any] struct {
	field fieldMetadata[Out]
}

// AnyInputField retains the input root type while erasing the field value type.
type AnyInputField[In any] interface {
	inputField() fieldMetadata[In]
}

// AnyOutputField retains the output root type while erasing the field value type.
type AnyOutputField[Out any] interface {
	outputField() fieldMetadata[Out]
}

//nolint:unused // The private method limits AnyInputField implementations to this package.
func (d InputDescriptor[In, Value]) inputField() fieldMetadata[In] {
	return d.field
}

//nolint:unused // The private method limits AnyOutputField implementations to this package.
func (d OutputDescriptor[Out, Value]) outputField() fieldMetadata[Out] {
	return d.field
}

// InputField declares a field selected from a resource input struct.
func InputField[In, Value any](
	selectField func(*In) *Value,
) InputDescriptor[In, Value] {
	return InputDescriptor[In, Value]{
		field: fieldMetadata[In]{
			resolve: func() (resolvedField, error) {
				return resolveInputSelector(selectField)
			},
		},
	}
}

// OutputField declares a field selected from a resource output struct.
func OutputField[Out, Value any](
	selectField func(Out) *Value,
) OutputDescriptor[Out, Value] {
	return OutputDescriptor[Out, Value]{
		field: fieldMetadata[Out]{
			resolve: func() (resolvedField, error) {
				return resolveOutputSelector(selectField)
			},
		},
	}
}

func resolveInputField[In any](field AnyInputField[In]) (fieldMetadata[In], error) {
	if nilDescriptor(field) {
		return fieldMetadata[In]{}, fmt.Errorf("input descriptor is nil")
	}
	metadata := field.inputField()
	if metadata.resolve == nil {
		return fieldMetadata[In]{}, fmt.Errorf("input descriptor is empty")
	}
	resolved, err := metadata.resolve()
	if err != nil {
		return fieldMetadata[In]{}, err
	}
	return resolvedInputMetadata[In](resolved), nil
}

func resolveOutputField[Out any](field AnyOutputField[Out]) (fieldMetadata[Out], error) {
	if nilDescriptor(field) {
		return fieldMetadata[Out]{}, fmt.Errorf("output descriptor is nil")
	}
	metadata := field.outputField()
	if metadata.resolve == nil {
		return fieldMetadata[Out]{}, fmt.Errorf("output descriptor is empty")
	}
	resolved, err := metadata.resolve()
	if err != nil {
		return fieldMetadata[Out]{}, err
	}
	return resolvedOutputMetadata[Out](resolved), nil
}

func nilDescriptor(field any) bool {
	if field == nil {
		return true
	}
	v := reflect.ValueOf(field)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

func resolvedInputMetadata[In any](field resolvedField) fieldMetadata[In] {
	return fieldMetadata[In]{
		index:        slices.Clone(field.index),
		path:         field.path,
		rootType:     field.rootType,
		valueType:    field.valueType,
		schemaDigest: field.schemaDigest,
		sensitive:    field.sensitive,
	}
}

func resolvedOutputMetadata[Out any](field resolvedField) fieldMetadata[Out] {
	return fieldMetadata[Out]{
		index:        slices.Clone(field.index),
		path:         field.path,
		rootType:     field.rootType,
		valueType:    field.valueType,
		schemaDigest: field.schemaDigest,
		sensitive:    field.sensitive,
	}
}

func resolveInputSelector[In, Value any](
	selectField func(*In) *Value,
) (resolvedField, error) {
	rootType := reflect.TypeFor[In]()
	if rootType.Kind() != reflect.Struct {
		return resolvedField{}, fmt.Errorf("input root must be a struct; got %s", rootType)
	}
	if selectField == nil {
		return resolvedField{}, fmt.Errorf("input field selector is nil")
	}
	return resolveFieldTwice(
		"input",
		rootType,
		reflect.TypeFor[Value](),
		func() (fieldCandidate, error) {
			root := new(In)
			return selectFieldCandidate(
				"input",
				reflect.ValueOf(root),
				reflect.TypeFor[Value](),
				func() *Value { return selectField(root) },
			)
		},
	)
}

func resolveOutputSelector[Out, Value any](
	selectField func(Out) *Value,
) (resolvedField, error) {
	rootType := reflect.TypeFor[Out]()
	if rootType.Kind() != reflect.Pointer || rootType.Elem().Kind() != reflect.Struct {
		return resolvedField{}, fmt.Errorf(
			"output root must be a pointer to a struct; got %s", rootType,
		)
	}
	if selectField == nil {
		return resolvedField{}, fmt.Errorf("output field selector is nil")
	}
	return resolveFieldTwice(
		"output",
		rootType,
		reflect.TypeFor[Value](),
		func() (fieldCandidate, error) {
			root := reflect.New(rootType.Elem())
			argument := root
			if argument.Type() != rootType {
				argument = argument.Convert(rootType)
			}
			output := argument.Interface().(Out)
			return selectFieldCandidate(
				"output",
				root,
				reflect.TypeFor[Value](),
				func() *Value { return selectField(output) },
			)
		},
	)
}

func resolveFieldTwice(
	label string,
	rootType reflect.Type,
	valueType reflect.Type,
	resolve func() (fieldCandidate, error),
) (resolvedField, error) {
	first, err := resolve()
	if err != nil {
		return resolvedField{}, err
	}
	second, err := resolve()
	if err != nil {
		return resolvedField{}, err
	}
	if !slices.Equal(first.index, second.index) {
		return resolvedField{}, fmt.Errorf(
			"%s field selector selected different fields %q and %q",
			label,
			first.path,
			second.path,
		)
	}
	return resolvedField{
		index:        slices.Clone(first.index),
		path:         first.path,
		rootType:     rootType,
		valueType:    valueType,
		schemaDigest: first.schemaDigest,
		sensitive:    first.sensitive,
	}, nil
}

type fieldCandidate struct {
	address      uintptr
	fieldType    reflect.Type
	index        []int
	path         string
	sensitive    bool
	rejection    string
	schemaDigest string
}

func selectFieldCandidate[Value any](
	label string,
	root reflect.Value,
	valueType reflect.Type,
	selectField func() *Value,
) (fieldCandidate, error) {
	candidates := prepareFieldCandidates(root)
	selected, err := invokeFieldSelector(label, selectField)
	if err != nil {
		return fieldCandidate{}, err
	}

	matches := make([]fieldCandidate, 0, 1)
	for _, candidate := range candidates {
		if candidate.address == selected && candidate.fieldType == valueType {
			matches = append(matches, candidate)
		}
	}
	if len(matches) == 0 {
		return fieldCandidate{}, fmt.Errorf(
			"%s field selector does not select a field in %s", label, root.Type(),
		)
	}
	for _, match := range matches {
		if strings.Contains(match.rejection, "zero-sized field") {
			return fieldCandidate{}, fmt.Errorf(
				"%s field selector selected %s", label, match.rejection,
			)
		}
	}
	if len(matches) != 1 {
		return fieldCandidate{}, fmt.Errorf("%s field selector result is ambiguous", label)
	}

	match := matches[0]
	if match.rejection != "" {
		return fieldCandidate{}, fmt.Errorf(
			"%s field selector selected %s", label, match.rejection,
		)
	}
	for _, candidate := range candidates {
		if candidate.rejection == "" && candidate.path == match.path &&
			!slices.Equal(candidate.index, match.index) {
			return fieldCandidate{}, fmt.Errorf(
				"%s field path %q is ambiguous", label, match.path,
			)
		}
	}

	schema, err := fieldValueSchema(match.fieldType, map[reflect.Type]bool{})
	if err != nil {
		return fieldCandidate{}, fmt.Errorf(
			"%s field selector selected field %q: %w", label, match.path, err,
		)
	}
	digest := sha256.Sum256([]byte(schema))
	match.schemaDigest = fmt.Sprintf("%x", digest)
	return match, nil
}

func invokeFieldSelector[Value any](
	label string,
	selectField func() *Value,
) (address uintptr, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			address = 0
			err = fmt.Errorf("%s field selector panicked: %v", label, recovered)
		}
	}()
	selected := selectField()
	if selected == nil {
		return 0, fmt.Errorf("%s field selector returned nil", label)
	}
	return reflect.ValueOf(selected).Pointer(), nil
}

func prepareFieldCandidates(root reflect.Value) []fieldCandidate {
	candidates := []fieldCandidate{}
	visiting := map[reflect.Type]bool{root.Elem().Type(): true}
	collectFieldCandidates(
		root.Elem(),
		nil,
		nil,
		false,
		"",
		visiting,
		&candidates,
	)
	return candidates
}

func collectFieldCandidates(
	value reflect.Value,
	index []int,
	path []string,
	sensitive bool,
	rejection string,
	visiting map[reflect.Type]bool,
	candidates *[]fieldCandidate,
) {
	t := value.Type()
	for i := range t.NumField() {
		structField := t.Field(i)
		fieldValue := value.Field(i)
		tag := ub.ParseTag(structField.Tag.Get("ub"))
		name := tag.FieldName(structField.Name)
		fieldIndex := appendIndex(index, i)
		fieldPath := appendPath(path, name)
		fieldSensitive := sensitive || tag.Sensitive
		fieldRejection := rejection
		if fieldRejection == "" {
			switch {
			case !structField.IsExported():
				fieldRejection = fmt.Sprintf("unexported field %q", strings.Join(fieldPath, "."))
			case tag.Skip:
				fieldRejection = fmt.Sprintf("ignored field %q", strings.Join(fieldPath, "."))
			case structField.Type.Size() == 0:
				fieldRejection = fmt.Sprintf("zero-sized field %q", strings.Join(fieldPath, "."))
			}
		}

		*candidates = append(*candidates, fieldCandidate{
			address:   fieldValue.Addr().Pointer(),
			fieldType: structField.Type,
			index:     fieldIndex,
			path:      strings.Join(fieldPath, "."),
			sensitive: fieldSensitive,
			rejection: fieldRejection,
		})

		nested, nestedType, ok := nestedStructValue(fieldValue)
		if !ok || visiting[nestedType] {
			continue
		}
		visiting[nestedType] = true
		collectFieldCandidates(
			nested,
			fieldIndex,
			fieldPath,
			fieldSensitive,
			fieldRejection,
			visiting,
			candidates,
		)
		delete(visiting, nestedType)
	}
}

func nestedStructValue(field reflect.Value) (reflect.Value, reflect.Type, bool) {
	switch field.Kind() {
	case reflect.Struct:
		return field, field.Type(), true
	case reflect.Pointer:
		if field.Type().Elem().Kind() != reflect.Struct || !field.CanSet() {
			return reflect.Value{}, nil, false
		}
		if field.IsNil() {
			field.Set(reflect.New(field.Type().Elem()))
		}
		return field.Elem(), field.Type().Elem(), true
	default:
		return reflect.Value{}, nil, false
	}
}

func appendIndex(index []int, value int) []int {
	out := make([]int, len(index)+1)
	copy(out, index)
	out[len(index)] = value
	return out
}

func appendPath(path []string, value string) []string {
	out := make([]string, len(path)+1)
	copy(out, path)
	out[len(path)] = value
	return out
}

func fieldValueSchema(t reflect.Type, visiting map[reflect.Type]bool) (string, error) {
	if t.Kind() == reflect.Struct {
		return structValueSchema(t, visiting)
	}
	if visiting[t] {
		return "", fmt.Errorf("recursive type %s", t)
	}
	visiting[t] = true
	defer delete(visiting, t)

	if t.Kind() == reflect.Pointer {
		value, err := fieldValueSchema(t.Elem(), visiting)
		if err != nil {
			return "", err
		}
		return "optional(" + value + ")", nil
	}

	switch t.Kind() {
	case reflect.Bool:
		return "boolean", nil
	case reflect.String:
		return "string", nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer", nil
	case reflect.Float32, reflect.Float64:
		return "number", nil
	case reflect.Array, reflect.Slice:
		value, err := fieldValueSchema(t.Elem(), visiting)
		if err != nil {
			return "", err
		}
		return "list(" + value + ")", nil
	case reflect.Map:
		if t.Key().Kind() != reflect.String {
			return "", fmt.Errorf("unsupported map key type %s", t.Key())
		}
		value, err := fieldValueSchema(t.Elem(), visiting)
		if err != nil {
			return "", err
		}
		return "map(" + value + ")", nil
	default:
		return "", fmt.Errorf("unsupported type %s", t)
	}
}

func structValueSchema(t reflect.Type, visiting map[reflect.Type]bool) (string, error) {
	if visiting[t] {
		return "", fmt.Errorf("recursive type %s", t)
	}
	visiting[t] = true
	defer delete(visiting, t)

	var schema strings.Builder
	schema.WriteString("object{")
	names := map[string]bool{}
	for field := range t.Fields() {
		if !field.IsExported() {
			continue
		}
		tag := ub.ParseTag(field.Tag.Get("ub"))
		if tag.Skip {
			continue
		}
		name := tag.FieldName(field.Name)
		if names[name] {
			return "", fmt.Errorf("field name %q is ambiguous in %s", name, t)
		}
		names[name] = true
		if field.Type.Size() == 0 {
			return "", fmt.Errorf("zero-sized field %q in %s", name, t)
		}
		value, err := fieldValueSchema(field.Type, visiting)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&schema, "%d:%s=%s;", len(name), name, value)
	}
	if len(names) == 0 {
		return "", fmt.Errorf("unsupported type %s", t)
	}
	schema.WriteByte('}')
	return schema.String(), nil
}
