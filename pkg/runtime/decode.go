package runtime

import (
	"fmt"

	"github.com/go-viper/mapstructure/v2"
)

// Decode fills v's exported fields from the inputs map using
// `mapstructure` tags. String values like "30s" decode into
// time.Duration fields. v must be a non-nil pointer to a struct.
func Decode(v any, inputs map[string]any) error {
	if v == nil {
		return fmt.Errorf("decode: nil destination")
	}
	if len(inputs) == 0 {
		return nil
	}
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		Result:           v,
		ErrorUnused:      true,
		DecodeHook:       mapstructure.StringToTimeDurationHookFunc(),
		WeaklyTypedInput: false,
	})
	if err != nil {
		return err
	}
	return decoder.Decode(inputs)
}
