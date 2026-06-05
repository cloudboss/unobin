package local

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/goschema"
	"github.com/cloudboss/unobin/pkg/lang"
)

// TestSchemaDeclaresDefaults reads this library the way the compiler
// does and asserts the declared defaults, so an extraction warning or
// a divergence between code and declaration fails here first.
func TestSchemaDeclaresDefaults(t *testing.T) {
	schema, warnings, err := goschema.Read(".")
	require.NoError(t, err)
	require.Empty(t, warnings)

	require.Equal(t, []lang.DefaultSpec{
		{Field: "var.mode", Value: "420"},
		{Field: "var.create-directory", Optional: true},
	}, schema.Resources["file"].Defaults)
}
