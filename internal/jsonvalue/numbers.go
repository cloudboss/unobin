// Package jsonvalue converts JSON values to the runtime's value types.
package jsonvalue

import (
	"encoding/json"
	"strings"
)

// CoerceNumbers updates decoded maps and lists in place. It restores integer
// tokens as int64 when they fit and decimal and exponent tokens as float64.
func CoerceNumbers(values map[string]any) map[string]any {
	for key, value := range values {
		values[key] = coerce(value)
	}
	return values
}

func coerce(value any) any {
	switch v := value.(type) {
	case json.Number:
		text := v.String()
		if !strings.ContainsAny(text, ".eE") {
			if integer, err := v.Int64(); err == nil {
				return integer
			}
		}
		if number, err := v.Float64(); err == nil {
			return number
		}
		return text
	case map[string]any:
		return CoerceNumbers(v)
	case []any:
		for i := range v {
			v[i] = coerce(v[i])
		}
	}
	return value
}
