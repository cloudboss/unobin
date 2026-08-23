package runtime

import (
	"fmt"
	"maps"
	"slices"

	encodedvalue "github.com/cloudboss/unobin/pkg/encoding/value"
)

type EncodedValue = encodedvalue.Value
type EncodedValueKind = encodedvalue.Kind

const (
	EncodedValueAbsent  EncodedValueKind = encodedvalue.KindAbsent
	EncodedValueNull    EncodedValueKind = encodedvalue.KindNull
	EncodedValueBoolean EncodedValueKind = encodedvalue.KindBoolean
	EncodedValueString  EncodedValueKind = encodedvalue.KindString
	EncodedValueInteger EncodedValueKind = encodedvalue.KindInteger
	EncodedValueNumber  EncodedValueKind = encodedvalue.KindNumber
	EncodedValueList    EncodedValueKind = encodedvalue.KindList
	EncodedValueMap     EncodedValueKind = encodedvalue.KindMap
	EncodedValueObject  EncodedValueKind = encodedvalue.KindObject
	EncodedValuePending EncodedValueKind = encodedvalue.KindPending
)

func AbsentValue() EncodedValue {
	return encodedvalue.Absent()
}

func NullValue() EncodedValue {
	return encodedvalue.Null()
}

func BooleanValue(v bool) EncodedValue {
	return encodedvalue.Boolean(v)
}

func StringValue(v string) EncodedValue {
	return encodedvalue.String(v)
}

func IntegerValue(v int64) EncodedValue {
	return encodedvalue.Integer(v)
}

func NumberValue(v float64) (EncodedValue, error) {
	return encodedvalue.Number(v)
}

func ListValue(items []EncodedValue) (EncodedValue, error) {
	return encodedvalue.List(items)
}

func MapValue(entries map[string]EncodedValue) (EncodedValue, error) {
	return encodedvalue.Map(entries)
}

func ObjectValue(fields map[string]EncodedValue) (EncodedValue, error) {
	return encodedvalue.Object(fields)
}

func PendingEncodedValue(refs []string) (EncodedValue, error) {
	return encodedvalue.Pending(refs)
}

func DecodeEncodedValue(data []byte) (EncodedValue, error) {
	return encodedvalue.Decode(data)
}

func decodeConcreteObjectFields(
	fields map[string]EncodedValue,
	subject string,
) (map[string]any, error) {
	result := make(map[string]any, len(fields))
	for _, name := range slices.Sorted(maps.Keys(fields)) {
		value, present, err := decodeConcreteValue(fields[name], subject)
		if err != nil {
			return nil, fmt.Errorf("%s field %q: %w", subject, name, err)
		}
		if present {
			result[name] = value
		}
	}
	return result, nil
}

func decodeConcreteValue(
	value EncodedValue,
	subject string,
) (any, bool, error) {
	switch value.Kind() {
	case EncodedValueAbsent:
		return nil, false, nil
	case EncodedValueNull:
		return nil, true, nil
	case EncodedValueBoolean:
		result, _ := value.Boolean()
		return result, true, nil
	case EncodedValueString:
		result, _ := value.String()
		return result, true, nil
	case EncodedValueInteger:
		result, _ := value.Integer()
		return result, true, nil
	case EncodedValueNumber:
		result, _ := value.Number()
		return result, true, nil
	case EncodedValueList:
		items, _ := value.Items()
		result := make([]any, len(items))
		for i, item := range items {
			decoded, present, err := decodeConcreteValue(item, subject)
			if err != nil {
				return nil, false, err
			}
			if !present {
				return nil, false, fmt.Errorf("list element %d is absent", i)
			}
			result[i] = decoded
		}
		return result, true, nil
	case EncodedValueMap:
		entries, _ := value.MapEntries()
		result := make(map[string]any, len(entries))
		for _, key := range slices.Sorted(maps.Keys(entries)) {
			decoded, present, err := decodeConcreteValue(entries[key], subject)
			if err != nil {
				return nil, false, err
			}
			if !present {
				return nil, false, fmt.Errorf("map value %q is absent", key)
			}
			result[key] = decoded
		}
		return result, true, nil
	case EncodedValueObject:
		fields, _ := value.ObjectFields()
		result, err := decodeConcreteObjectFields(fields, subject)
		return result, true, err
	case EncodedValuePending:
		return nil, false, fmt.Errorf("%s value must be concrete", subject)
	default:
		return nil, false, fmt.Errorf("%s value kind is invalid", subject)
	}
}
