package runtime

import "github.com/cloudboss/unobin/pkg/structdecode"

// Decode fills v's exported fields from the inputs map using `ub`
// struct tags. A field's key is the tag's name, or the kebab-cased
// field name when the tag has no name (or no tag at all). String
// values like "30s" decode into time.Duration fields. v must be a
// non-nil pointer to a struct.
func Decode(v any, inputs map[string]any) error {
	return structdecode.Decode(v, inputs)
}
