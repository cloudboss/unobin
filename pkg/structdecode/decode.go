// Package structdecode decodes UB object values into ordinary Go structs.
package structdecode

import (
	"fmt"
	"reflect"

	"github.com/go-viper/mapstructure/v2"

	"github.com/cloudboss/unobin/pkg/lang"
)

// Decode fills v's exported fields from inputs using ub struct tags. A field's
// key is the tag name, or the kebab-cased field name when the tag has no name.
// String values like "30s" decode into time.Duration fields. v must be a
// non-nil pointer to a struct.
func Decode(v any, inputs map[string]any) error {
	if v == nil {
		return fmt.Errorf("decode: nil destination")
	}
	if err := validateDestination(v); err != nil {
		return err
	}
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		Result:           v,
		TagName:          "ub",
		MatchName:        matchUBName,
		ErrorUnused:      true,
		DecodeHook:       mapstructure.StringToTimeDurationHookFunc(),
		WeaklyTypedInput: false,
	})
	if err != nil {
		return err
	}
	return decoder.Decode(inputs)
}

func validateDestination(v any) error {
	t := reflect.TypeOf(v)
	if t == nil || t.Kind() != reflect.Pointer || t.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("decode: destination must be a non-nil pointer to a struct")
	}
	if reflect.ValueOf(v).IsNil() {
		return fmt.Errorf("decode: destination must be a non-nil pointer to a struct")
	}
	return nil
}

// matchUBName matches a map key to a struct field by unobin's kebab convention.
// fieldName is the tag name when present, otherwise the Go field name.
func matchUBName(mapKey, fieldName string) bool {
	return mapKey == lang.PascalToKebab(fieldName)
}
