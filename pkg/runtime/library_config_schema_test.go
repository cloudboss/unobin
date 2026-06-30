package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

func TestLibraryConfigSchemaFromLibrarySchemaUsesSchemaFields(t *testing.T) {
	fields := []typecheck.ObjectField{
		{Name: "region", Type: typecheck.TString(), Defaulted: true},
		{Name: "profile", Type: typecheck.TString(), Optional: true},
	}
	defaults := []lang.DefaultSpec{{Field: "input.region", Value: "'us-west-2'"}}
	constraints := []lang.ConstraintSpec{{
		Kind:    "predicate",
		When:    "true",
		Require: "(@core.length(input.region) >= 1)",
		Message: "region is required",
	}}
	digest := cfg.DigestView(fields, defaults, constraints)

	got, ok := LibraryConfigSchemaFromLibrarySchema("example.com/aws", &LibrarySchema{
		HasConfiguration:         true,
		ConfigurationFields:      fields,
		ConfigurationDefaults:    defaults,
		ConfigurationConstraints: constraints,
		ConfigurationDigest:      digest,
	})

	require.True(t, ok)
	require.Equal(t, LibraryConfigSchema{
		Path:        "example.com/aws",
		Fields:      fields,
		Defaults:    defaults,
		Constraints: constraints,
		Digest:      digest,
	}, got)
	require.Equal(t,
		typecheck.TLibraryConfig("example.com/aws", "example.com/aws", digest, fields),
		got.TypecheckType())
	require.Equal(t, lang.LibraryConfigSchema{
		Type: &lang.TypeObject{Fields: []*lang.TypeObjectField{
			{Name: "region", Type: &lang.TypeAtomic{Name: "string"}},
			{Name: "profile", Type: &lang.TypeOptional{Elem: &lang.TypeAtomic{Name: "string"}}},
		}},
		Defaults:    defaults,
		Constraints: constraints,
	}, got.LangSchema())
}

func TestLibraryConfigSchemaFromLibrarySchemaConvertsLegacyMap(t *testing.T) {
	got, ok := LibraryConfigSchemaFromLibrarySchema("example.com/aws", &LibrarySchema{
		HasConfiguration: true,
		Configuration: map[string]typecheck.Type{
			"region":  typecheck.TString(),
			"profile": typecheck.TOptional(typecheck.TString()),
		},
	})

	require.True(t, ok)
	require.Equal(t, []typecheck.ObjectField{
		{Name: "profile", Type: typecheck.TString(), Optional: true},
		{Name: "region", Type: typecheck.TString()},
	}, got.Fields)
	require.Equal(t, cfg.DigestView(got.Fields, nil, nil), got.Digest)
}

func TestLibraryConfigSchemaFromLibrarySchemaRejectsUnreadableSchema(t *testing.T) {
	_, ok := LibraryConfigSchemaFromLibrarySchema("example.com/aws", &LibrarySchema{
		HasConfiguration: true,
	})

	require.False(t, ok)
}

func TestLibraryConfigSchemaFromView(t *testing.T) {
	fields := []typecheck.ObjectField{{Name: "region", Type: typecheck.TString()}}
	view := cfg.LibraryConfigView{
		Fields:       fields,
		Defaults:     []lang.DefaultSpec{{Field: "input.region", Value: "'us-west-2'"}},
		Empty:        false,
		SchemaDigest: cfg.DigestView(fields, nil, nil),
	}

	got := LibraryConfigSchemaFromView("example.com/aws", view)

	require.Equal(t, "example.com/aws", got.Path)
	require.Equal(t, fields, got.Fields)
	require.Equal(t, view.Defaults, got.Defaults)
	require.Equal(t, view.SchemaDigest, got.Digest)
}

func TestLibraryConfigSchemaFromLibraryUsesRuntimeRegistration(t *testing.T) {
	type configSchema struct {
		Region cfg.String
	}
	lib := &Library{Configuration: &cfg.ConfigurationType[*configSchema]{
		New: func() *configSchema { return &configSchema{} },
	}}

	got, ok, err := LibraryConfigSchemaFromLibrary("example.com/aws", lib)

	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "example.com/aws", got.Path)
	require.Equal(t, []typecheck.ObjectField{{Name: "region", Type: typecheck.TString()}},
		got.Fields)
	require.NotEmpty(t, got.Digest)
}
