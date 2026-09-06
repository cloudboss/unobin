package runtime

import (
	"fmt"
	"maps"
	"slices"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

func (d resolvedConfigurationDefinition) encodePlanningValue(
	values map[string]any,
) (EncodedValue, error) {
	values = cloneConfigMap(values)
	if err := applyConfigDefaults(values, d.schemaDefaults); err != nil {
		return EncodedValue{}, fmt.Errorf("configuration defaults: %w", err)
	}
	return encodePlanningConfigurationObject(d.schemaFields, values)
}

func encodePlanningConfigurationObject(
	fields []typecheck.ObjectField,
	values map[string]any,
) (EncodedValue, error) {
	declared := make(map[string]bool, len(fields))
	encoded := make(map[string]EncodedValue, len(fields))
	for _, field := range fields {
		declared[field.Name] = true
		value, present := values[field.Name]
		if !present {
			if field.Optional {
				encoded[field.Name] = AbsentValue()
				continue
			}
			return EncodedValue{}, fmt.Errorf(
				"field %q: required but not provided",
				field.Name,
			)
		}
		typ := field.Type
		if field.Optional {
			typ = typecheck.TOptional(typ)
		}
		item, err := encodePlanningValue(typ, value)
		if err != nil {
			return EncodedValue{}, fmt.Errorf("field %q: %w", field.Name, err)
		}
		encoded[field.Name] = item
	}
	for _, name := range slices.Sorted(maps.Keys(values)) {
		if !declared[name] {
			return EncodedValue{}, fmt.Errorf("unknown field %q", name)
		}
	}
	value, err := ObjectValue(encoded)
	if err != nil {
		return EncodedValue{}, fmt.Errorf("encode configuration object: %w", err)
	}
	return value, nil
}

func encodePlanningValue(
	typ typecheck.Type,
	value any,
) (EncodedValue, error) {
	if pending, ok := value.(PendingValue); ok {
		encoded, err := PendingEncodedValue(pending.Refs)
		if err != nil {
			return EncodedValue{}, fmt.Errorf("pending value: %w", err)
		}
		return encoded, nil
	}
	if typ.Kind == typecheck.Optional {
		if value == nil {
			return NullValue(), nil
		}
		typ = typ.Unwrap()
	}

	if token, ok := resourceAssetToken(value); ok &&
		(typ.Kind == typecheck.String || typ.Kind == typecheck.AssetPath || typ.Kind == typecheck.Bytes) {
		return StringValue(token), nil
	}
	switch typ.Kind {
	case typecheck.Bytes:
		if err := checkConfigValue(typ, value); err != nil {
			return EncodedValue{}, err
		}
		if _, ok := value.([]any); ok {
			return encodePlanningConfigurationList(typecheck.TInteger(), value)
		}
		content, ok := value.([]byte)
		if !ok {
			return EncodedValue{}, planningConfigurationTypeError("bytes", value)
		}
		items := make([]EncodedValue, len(content))
		for i, b := range content {
			items[i] = IntegerValue(int64(b))
		}
		return ListValue(items)
	case typecheck.Unknown, typecheck.Opaque:
		return encodeUntypedPlanningValue(value)
	case typecheck.String, typecheck.AssetPath:
		result, ok := value.(string)
		if !ok {
			return EncodedValue{}, planningConfigurationTypeError("string", value)
		}
		return StringValue(result), nil
	case typecheck.Integer:
		result, ok := value.(int64)
		if !ok {
			return EncodedValue{}, planningConfigurationTypeError("integer", value)
		}
		return IntegerValue(result), nil
	case typecheck.Number:
		switch result := value.(type) {
		case int64:
			return IntegerValue(result), nil
		case float64:
			encoded, err := NumberValue(result)
			if err != nil {
				return EncodedValue{}, err
			}
			return encoded, nil
		default:
			return EncodedValue{}, planningConfigurationTypeError("number", value)
		}
	case typecheck.Boolean:
		result, ok := value.(bool)
		if !ok {
			return EncodedValue{}, planningConfigurationTypeError("boolean", value)
		}
		return BooleanValue(result), nil
	case typecheck.Null:
		if value != nil {
			return EncodedValue{}, planningConfigurationTypeError("null", value)
		}
		return NullValue(), nil
	case typecheck.List:
		return encodePlanningConfigurationList(elemType(typ), value)
	case typecheck.Map:
		return encodePlanningConfigurationMap(elemType(typ), value)
	case typecheck.Object, typecheck.LibraryConfig:
		result, ok := value.(map[string]any)
		if !ok {
			return EncodedValue{}, planningConfigurationTypeError("object", value)
		}
		return encodePlanningConfigurationObject(typ.Fields, result)
	case typecheck.Tuple:
		return encodePlanningConfigurationTuple(typ.Elems, value)
	case typecheck.Union:
		for _, member := range typ.Elems {
			if err := checkConfigValue(member, value); err != nil {
				continue
			}
			return encodePlanningValue(member, value)
		}
		return EncodedValue{}, fmt.Errorf("value does not match any union member")
	default:
		return EncodedValue{}, fmt.Errorf("unsupported configuration type %v", typ.Kind)
	}
}

func encodePlanningConfigurationList(
	element typecheck.Type,
	value any,
) (EncodedValue, error) {
	items, ok := value.([]any)
	if !ok {
		return EncodedValue{}, planningConfigurationTypeError("list", value)
	}
	encoded := make([]EncodedValue, len(items))
	for i, item := range items {
		value, err := encodePlanningValue(element, item)
		if err != nil {
			return EncodedValue{}, fmt.Errorf("element %d: %w", i, err)
		}
		encoded[i] = value
	}
	result, err := ListValue(encoded)
	if err != nil {
		return EncodedValue{}, fmt.Errorf("encode configuration list: %w", err)
	}
	return result, nil
}

func encodePlanningConfigurationMap(
	element typecheck.Type,
	value any,
) (EncodedValue, error) {
	items, ok := value.(map[string]any)
	if !ok {
		return EncodedValue{}, planningConfigurationTypeError("map", value)
	}
	encoded := make(map[string]EncodedValue, len(items))
	for _, key := range slices.Sorted(maps.Keys(items)) {
		value, err := encodePlanningValue(element, items[key])
		if err != nil {
			return EncodedValue{}, fmt.Errorf("key %q: %w", key, err)
		}
		encoded[key] = value
	}
	result, err := MapValue(encoded)
	if err != nil {
		return EncodedValue{}, fmt.Errorf("encode configuration map: %w", err)
	}
	return result, nil
}

func encodePlanningConfigurationTuple(
	elements []typecheck.Type,
	value any,
) (EncodedValue, error) {
	items, ok := value.([]any)
	if !ok {
		return EncodedValue{}, planningConfigurationTypeError("tuple", value)
	}
	if len(items) != len(elements) {
		return EncodedValue{}, fmt.Errorf(
			"expected tuple of %d elements, got %d",
			len(elements),
			len(items),
		)
	}
	encoded := make([]EncodedValue, len(items))
	for i, item := range items {
		value, err := encodePlanningValue(elements[i], item)
		if err != nil {
			return EncodedValue{}, fmt.Errorf("element %d: %w", i, err)
		}
		encoded[i] = value
	}
	result, err := ListValue(encoded)
	if err != nil {
		return EncodedValue{}, fmt.Errorf("encode configuration tuple: %w", err)
	}
	return result, nil
}

func encodeUntypedPlanningValue(value any) (EncodedValue, error) {
	switch result := value.(type) {
	case nil:
		return NullValue(), nil
	case bool:
		return BooleanValue(result), nil
	case string:
		return StringValue(result), nil
	case int64:
		return IntegerValue(result), nil
	case float64:
		encoded, err := NumberValue(result)
		if err != nil {
			return EncodedValue{}, err
		}
		return encoded, nil
	case []any:
		return encodePlanningConfigurationList(typecheck.TUnknown(), result)
	case map[string]any:
		return encodePlanningConfigurationMap(typecheck.TUnknown(), result)
	default:
		return EncodedValue{}, fmt.Errorf(
			"unsupported planning value %s",
			lang.TypeMessage(value),
		)
	}
}

func planningConfigurationTypeError(expected string, value any) error {
	return fmt.Errorf("expected %s, got %s", expected, lang.TypeMessage(value))
}
