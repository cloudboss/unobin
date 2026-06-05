package core

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/goschema"
	"github.com/cloudboss/unobin/pkg/lang"
)

// TestSchemaDeclaresDefaults reads this library the way the compiler
// does and asserts each action's declared defaults, so an extraction
// warning or a divergence between code and declaration fails here
// first.
func TestSchemaDeclaresDefaults(t *testing.T) {
	schema, warnings, err := goschema.Read(".")
	require.NoError(t, err)
	require.Empty(t, warnings)

	require.Equal(t, []lang.DefaultSpec{
		{Field: "var.environment", Optional: true},
		{Field: "var.working-dir", Optional: true},
	}, schema.Actions["command"].Defaults)

	require.Equal(t, []lang.DefaultSpec{
		{Field: "var.shell", Value: "'sh'"},
		{Field: "var.environment", Optional: true},
		{Field: "var.working-dir", Optional: true},
	}, schema.Actions["script"].Defaults)

	require.Equal(t, []lang.DefaultSpec{
		{Field: "var.method", Value: "'GET'"},
		{Field: "var.headers", Optional: true},
		{Field: "var.body", Optional: true},
		{Field: "var.timeout", Optional: true},
	}, schema.Actions["http"].Defaults)

	require.Equal(t, []lang.DefaultSpec{
		{Field: "var.interval", Value: "1000000000"},
		{Field: "var.timeout", Value: "300000000000"},
		{Field: "var.environment", Optional: true},
		{Field: "var.working-dir", Optional: true},
	}, schema.Actions["wait-for"].Defaults)
}
