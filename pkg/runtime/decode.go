package runtime

import (
	"fmt"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/go-viper/mapstructure/v2"
)

// Decode fills v's exported fields from the inputs map using `ub`
// struct tags. A field's key is the tag's name, or the kebab-cased
// field name when the tag has no name (or no tag at all). String
// values like "30s" decode into time.Duration fields. v must be a
// non-nil pointer to a struct.
func Decode(v any, inputs map[string]any) error {
	if v == nil {
		return fmt.Errorf("decode: nil destination")
	}
	if len(inputs) == 0 {
		return nil
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

// matchUBName matches a map key to a struct field by unobin's kebab
// convention. fieldName is the field's effective name: the `ub` tag's
// name when present, otherwise the Go field name. PascalToKebab is
// idempotent on an already-kebab name, so an explicit `ub:"aws-kms"`
// and an untagged AwsKms field both match the key "aws-kms".
func matchUBName(mapKey, fieldName string) bool {
	return mapKey == lang.PascalToKebab(fieldName)
}
