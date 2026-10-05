package goschema

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

func TestAnalyzeUsesDeclaringFileImports(t *testing.T) {
	analysis, err := Analyze("testdata/fileimports")
	require.NoError(t, err)
	require.Empty(t, analysis.Warnings)
	schema := analysis.Schema
	require.Equal(t, map[string]typecheck.Type{"name": typecheck.TString()},
		schema.DataSources["remote"].Inputs)
	require.Equal(t, map[string]typecheck.Type{"name": typecheck.TString()},
		schema.DataSources["remote"].Outputs)
	local := schema.DataSources["local"]
	require.Equal(t, map[string]typecheck.Type{
		"nested": typecheck.TObject([]typecheck.ObjectField{
			{Name: "count", Type: typecheck.TInteger()},
		}),
		"alias": typecheck.TObject([]typecheck.ObjectField{
			{Name: "label", Type: typecheck.TString()},
		}),
		"weird": typecheck.TObject([]typecheck.ObjectField{
			{Name: "code", Type: typecheck.TBytes()},
		}),
		"timeout":   typecheck.TInteger(),
		"primary":   typecheck.TOptional(typecheck.TString()),
		"secondary": typecheck.TOptional(typecheck.TString()),
	}, local.Inputs)
	require.Equal(t, map[string]typecheck.Type{
		"enabled": typecheck.TBoolean(), "secret": typecheck.TString(),
	}, local.Outputs)
	require.Equal(t, []string{"secret"}, local.SensitiveOutputs)
	require.Equal(t, []lang.DefaultSpec{
		{Field: "input.timeout", Value: "2000000000"},
	}, local.Defaults)
	require.Equal(t, []lang.ConstraintSpec{
		{Kind: "exactly-one-of", Fields: []string{"input.primary", "input.secondary"},
			Message: "choose a value"},
		{Kind: "predicate", When: "true", Require: "(input.nested.count > 0)"},
	}, local.Constraints)
	require.Equal(t, "example.com/fileimports/config.Config", schema.ConfigurationIdentity)
	require.Equal(t, map[string]typecheck.Type{
		"region":  typecheck.TString(),
		"profile": typecheck.TOptional(typecheck.TString()),
	}, schema.Configuration)
	require.Equal(t, []lang.DefaultSpec{
		{Field: "input.profile", Value: "'dev'"},
	}, schema.ConfigurationDefaults)
	index := analysis.Index
	requireLocationPrefix(t, index.InputTypes["data-source"]["remote"], "left.go", "Query")
	requireLocationPrefix(t, index.OutputTypes["data-source"]["local"],
		"b_fields.go", "LocalOutput")
	for _, field := range []struct {
		path   string
		file   string
		prefix string
	}{
		{path: "nested.count", file: "right.go", prefix: "Number"},
		{path: "alias.label", file: "left.go", prefix: "Value"},
		{path: "weird.code", file: "model.go", prefix: "Code"},
	} {
		requireLocationPrefix(t, index.InputFields["data-source"]["local"][field.path],
			field.file, field.prefix)
	}
	requireLocationPrefix(t, index.OutputFields["data-source"]["local"]["enabled"],
		"right.go", "Enabled")
	requireLocationPrefix(t, index.ConfigType, "a_config.go", "Config")
	requireLocationPrefix(t, index.ConfigFields["region"], "a_config.go", "Region")
}

func TestAnalyzeImportsIndependentOfFileOrder(t *testing.T) {
	want, err := Analyze("testdata/fileimports")
	require.NoError(t, err)
	dir := t.TempDir()
	require.NoError(t, os.CopyFS(dir, os.DirFS("testdata/fileimports")))
	for old, renamed := range map[string]string{
		"a_library.go": "z_library.go", "b_fields.go": "y_fields.go",
		"c_methods.go": "x_methods.go", "d_configuration.go": "w_configuration.go",
	} {
		require.NoError(t, os.Rename(filepath.Join(dir, old), filepath.Join(dir, renamed)))
	}
	got, err := Analyze(dir)
	require.NoError(t, err)
	require.Equal(t, want.Schema, got.Schema)
	require.Equal(t, want.Warnings, got.Warnings)
	requireLocationPrefix(t, got.Index.InputFields["data-source"]["local"]["nested.count"],
		"right.go", "Number")
	config, index, warnings, err := ReadLibraryConfigurationWithIndex(dir)
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.Equal(t, want.Schema.Configuration, config.Configuration)
	require.Equal(t, want.Schema.ConfigurationDefaults, config.ConfigurationDefaults)
	require.Equal(t, want.Schema.ConfigurationIdentity, config.ConfigurationIdentity)
	requireLocationPrefix(t, index.ConfigFields["profile"], "a_config.go", "Profile")
}
