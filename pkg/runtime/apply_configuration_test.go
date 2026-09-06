package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

type runtimeReferenceConfiguration struct {
	Tags  map[string]string
	Items []string
}

type runtimeNullableConfiguration struct {
	Profile *string
}

func referenceConfigLibrary(schema *LibrarySchema) *Library {
	return &Library{
		Configuration: &cfg.ConfigurationType[*runtimeReferenceConfiguration]{
			New: func() *runtimeReferenceConfiguration {
				return &runtimeReferenceConfiguration{}
			},
		},
		Schema: schema,
	}
}

func nullableConfigLibrary(schema *LibrarySchema) *Library {
	return &Library{
		Configuration: &cfg.ConfigurationType[*runtimeNullableConfiguration]{
			New: func() *runtimeNullableConfiguration {
				return &runtimeNullableConfiguration{}
			},
		},
		Schema: schema,
	}
}

func referenceConfigSchema(defaultTags string, constrained bool) *LibrarySchema {
	fields := []typecheck.ObjectField{
		{Name: "tags", Type: typecheck.TMap(typecheck.TString()), Defaulted: true},
		{Name: "items", Type: typecheck.TList(typecheck.TString()), Defaulted: true},
	}
	defaults := []lang.DefaultSpec{
		{Field: "input.tags", Value: defaultTags},
		{Field: "input.items", Value: "['a', 'b']"},
	}
	var constraints []lang.ConstraintSpec
	if constrained {
		constraints = []lang.ConstraintSpec{{
			Kind:    "predicate",
			When:    "true",
			Require: "(@core.length(input.tags) >= 1)",
			Message: "tags are required",
		}}
	}
	return &LibrarySchema{
		HasConfiguration:         true,
		ConfigurationFields:      fields,
		ConfigurationDefaults:    defaults,
		ConfigurationConstraints: constraints,
		ConfigurationDigest:      cfg.DigestView(fields, defaults, constraints),
	}
}

func nullableConfigSchema(defaulted bool, constraints []lang.ConstraintSpec) *LibrarySchema {
	fields := []typecheck.ObjectField{{
		Name: "profile", Type: typecheck.TString(), Optional: true, Defaulted: defaulted,
	}}
	var defaults []lang.DefaultSpec
	if defaulted {
		defaults = []lang.DefaultSpec{{Field: "input.profile", Value: "'dev'"}}
	}
	return &LibrarySchema{
		HasConfiguration:         true,
		ConfigurationFields:      fields,
		ConfigurationDefaults:    defaults,
		ConfigurationConstraints: constraints,
		ConfigurationDigest:      cfg.DigestView(fields, defaults, constraints),
	}
}

func TestDecodeLibraryConfigAppliesReferenceDefaults(t *testing.T) {
	lib := referenceConfigLibrary(referenceConfigSchema("{ env: 'dev' }", false))

	gotAny, err := decodeLibraryConfig(lib, map[string]any{})

	require.NoError(t, err)
	got := gotAny.(*runtimeReferenceConfiguration)
	require.Equal(t, map[string]string{"env": "dev"}, got.Tags)
	require.Equal(t, []string{"a", "b"}, got.Items)
}

func TestDecodeLibraryConfigKeepsReferenceValues(t *testing.T) {
	lib := referenceConfigLibrary(referenceConfigSchema("{ env: 'dev' }", false))

	gotAny, err := decodeLibraryConfig(lib, map[string]any{
		"tags":  map[string]any{"env": "prod"},
		"items": []any{"x"},
	})

	require.NoError(t, err)
	got := gotAny.(*runtimeReferenceConfiguration)
	require.Equal(t, map[string]string{"env": "prod"}, got.Tags)
	require.Equal(t, []string{"x"}, got.Items)
}

func TestDecodeLibraryConfigAppliesNullableDefault(t *testing.T) {
	lib := nullableConfigLibrary(nullableConfigSchema(true, nil))

	gotAny, err := decodeLibraryConfig(lib, map[string]any{})

	require.NoError(t, err)
	got := gotAny.(*runtimeNullableConfiguration)
	require.NotNil(t, got.Profile)
	require.Equal(t, "dev", *got.Profile)
}

func TestDecodeLibraryConfigKeepsNullableNull(t *testing.T) {
	lib := nullableConfigLibrary(nullableConfigSchema(true, nil))

	gotAny, err := decodeLibraryConfig(lib, map[string]any{"profile": nil})

	require.NoError(t, err)
	got := gotAny.(*runtimeNullableConfiguration)
	require.Nil(t, got.Profile)
}

func TestDecodeLibraryConfigConstraintsSeeNullableDefault(t *testing.T) {
	constraints := []lang.ConstraintSpec{{
		Kind: "predicate", When: "true", Require: "input.profile != null",
		Message: "profile is required",
	}}
	lib := nullableConfigLibrary(nullableConfigSchema(true, constraints))

	_, err := decodeLibraryConfig(lib, map[string]any{})

	require.NoError(t, err)
}

func TestDecodeLibraryConfigConstraintsSeeNullableNull(t *testing.T) {
	constraints := []lang.ConstraintSpec{{
		Kind: "predicate", When: "true", Require: "input.profile == null",
		Message: "profile must be null",
	}}
	lib := nullableConfigLibrary(nullableConfigSchema(true, constraints))

	_, err := decodeLibraryConfig(lib, map[string]any{"profile": nil})

	require.NoError(t, err)
}

func TestDecodeLibraryConfigKeepsNullPointerWithoutDefault(t *testing.T) {
	lib := nullableConfigLibrary(nullableConfigSchema(false, nil))

	gotAny, err := decodeLibraryConfig(lib, map[string]any{"profile": nil})

	require.NoError(t, err)
	got := gotAny.(*runtimeNullableConfiguration)
	require.Nil(t, got.Profile)
}

func TestDecodeLibraryConfigRejectsNullReferenceValue(t *testing.T) {
	lib := referenceConfigLibrary(referenceConfigSchema("{ env: 'dev' }", false))

	_, err := decodeLibraryConfig(lib, map[string]any{"tags": nil})

	require.Error(t, err)
	require.Contains(t, err.Error(), `field "tags": required but is null`)
}

func TestDecodeLibraryConfigConstraintsSeeReferenceDefaults(t *testing.T) {
	lib := referenceConfigLibrary(referenceConfigSchema("{ env: 'dev' }", true))

	_, err := decodeLibraryConfig(lib, map[string]any{})

	require.NoError(t, err)
}

func TestSyntaxValidationRejectsOldConfigurationMeta(t *testing.T) {
	src := ubtest.ReadFixture(t,
		"testdata/ub/apply-configuration/invalid/old-configuration-meta.ub")
	f, err := syntax.ParseSource("factory.ub", []byte(src))
	require.NoError(t, err)
	errs := syntax.ValidateFile(f)
	require.Error(t, errs.Err())
	require.Contains(t, errs.Err().Error(), `meta key "@configuration" is not allowed`)
}
