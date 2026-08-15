package runtime

import encodedvalue "github.com/cloudboss/unobin/pkg/encoding/value"

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
