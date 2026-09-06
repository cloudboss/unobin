package runner

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/cloudboss/unobin/pkg/runtime"
)

type displayValue struct {
	value          runtime.EncodedValue
	sensitivePaths []string
	path           string
}

type sensitiveDisplayValue string

func displayObject(value runtime.EncodedValue, paths []string) map[string]any {
	fields, ok := value.ObjectFields()
	if !ok {
		return nil
	}
	result := make(map[string]any, len(fields))
	for name, field := range fields {
		if field.Kind() != runtime.EncodedValueAbsent {
			result[name] = displayValue{value: field, sensitivePaths: paths, path: displayPointer("", name)}
		}
	}
	return result
}

func displayPointer(parent, name string) string {
	return parent + "/" + strings.NewReplacer("~", "~0", "/", "~1").Replace(name)
}

func (v displayValue) sameValue(other displayValue) bool {
	return reflect.DeepEqual(v.value, other.value)
}

func (v displayValue) native() any {
	if slices.Contains(v.sensitivePaths, v.path) || slices.Contains(v.sensitivePaths, "") {
		return sensitiveDisplayValue(sensitivePlaceholder)
	}
	child := func(value runtime.EncodedValue, name string) any {
		return (displayValue{value: value, sensitivePaths: v.sensitivePaths,
			path: displayPointer(v.path, name)}).native()
	}
	switch v.value.Kind() {
	case runtime.EncodedValueAbsent, runtime.EncodedValueNull:
		return nil
	case runtime.EncodedValueBoolean:
		result, _ := v.value.Boolean()
		return result
	case runtime.EncodedValueString:
		result, _ := v.value.String()
		return result
	case runtime.EncodedValueInteger:
		result, _ := v.value.Integer()
		return result
	case runtime.EncodedValueNumber:
		result, _ := v.value.Number()
		return result
	case runtime.EncodedValuePending:
		refs, _ := v.value.PendingRefs()
		return runtime.PendingValue{Refs: refs}
	case runtime.EncodedValueList:
		items, _ := v.value.Items()
		result := make([]any, len(items))
		for i, item := range items {
			result[i] = child(item, strconv.Itoa(i))
		}
		return result
	case runtime.EncodedValueMap, runtime.EncodedValueObject:
		fields, _ := v.value.ObjectFields()
		if v.value.Kind() == runtime.EncodedValueMap {
			fields, _ = v.value.MapEntries()
		}
		result := make(map[string]any, len(fields))
		for name, value := range fields {
			if value.Kind() != runtime.EncodedValueAbsent {
				result[name] = child(value, name)
			}
		}
		return result
	default:
		panic(fmt.Sprintf("invalid display value kind %q", v.value.Kind()))
	}
}

func nativeDisplayObject(value runtime.EncodedValue, paths []string) map[string]any {
	fields := displayObject(value, paths)
	result := make(map[string]any, len(fields))
	for name, value := range fields {
		result[name] = value.(displayValue).native()
	}
	return result
}
