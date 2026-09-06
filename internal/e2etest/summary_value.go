package e2etest

import encodedvalue "github.com/cloudboss/unobin/pkg/encoding/value"

func summaryObject(value encodedvalue.Value) map[string]any {
	fields, ok := value.ObjectFields()
	if !ok {
		return nil
	}
	result := make(map[string]any, len(fields))
	for name, field := range fields {
		if field.Kind() != encodedvalue.KindAbsent {
			result[name] = summaryValue(field)
		}
	}
	return result
}

func summaryValue(value encodedvalue.Value) any {
	switch value.Kind() {
	case encodedvalue.KindAbsent, encodedvalue.KindNull:
		return nil
	case encodedvalue.KindBoolean:
		result, _ := value.Boolean()
		return result
	case encodedvalue.KindString:
		result, _ := value.String()
		return result
	case encodedvalue.KindInteger:
		result, _ := value.Integer()
		return result
	case encodedvalue.KindNumber:
		result, _ := value.Number()
		return result
	case encodedvalue.KindPending:
		refs, _ := value.PendingRefs()
		return map[string]any{"pending": refs}
	case encodedvalue.KindObject:
		return summaryObject(value)
	case encodedvalue.KindList:
		items, _ := value.Items()
		result := make([]any, len(items))
		for i, item := range items {
			result[i] = summaryValue(item)
		}
		return result
	case encodedvalue.KindMap:
		fields, _ := value.MapEntries()
		result := make(map[string]any, len(fields))
		for name, field := range fields {
			result[name] = summaryValue(field)
		}
		return result
	default:
		panic("invalid summary value")
	}
}
