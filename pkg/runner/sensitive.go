package runner

import "github.com/cloudboss/unobin/pkg/lang"

// sensitivePlaceholder is the literal renderers print in place of a
// masked value.
const sensitivePlaceholder = "***"

// rootSensitiveOutputs returns the set of root output names declared
// with `@sensitive: true` in the factory source's `outputs:` block.
// Returns an empty set for a nil file or a file with no outputs
// block.
func rootSensitiveOutputs(f *lang.File) map[string]bool {
	return lang.SensitiveOutputs(lang.TopLevelBlock(f, "outputs"))
}
