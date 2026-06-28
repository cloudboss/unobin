package check

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/internal/ubtest"
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

func typeFixture(t testing.TB, name string) string {
	t.Helper()
	return ubtest.ReadValidFixture(t, "testdata/ub/types", name)
}

// localFileModule mirrors the input and output fields of the real
// `local.file` resource, declared defaults included, so the tests
// don't pull the libraries package as a dependency.
func localFileLibrary() *runtime.Library {
	return &runtime.Library{
		Schema: &runtime.LibrarySchema{
			Resources: map[string]*runtime.TypeSchema{
				"file": {
					Inputs: map[string]typecheck.Type{
						"path":             typecheck.TString(),
						"content":          typecheck.TString(),
						"mode":             typecheck.TInteger(),
						"create-directory": typecheck.TBoolean(),
					},
					Outputs: map[string]typecheck.Type{
						"path":   typecheck.TString(),
						"sha256": typecheck.TString(),
						"size":   typecheck.TInteger(),
					},
					Defaults: []lang.DefaultSpec{
						{Field: "input.mode", Value: "420"},
						{Field: "input.create-directory", Optional: true},
					},
				},
			},
		},
	}
}

func libraryConfigSchemaLibrary(digest string) *runtime.Library {
	fields := []typecheck.ObjectField{{Name: "region", Type: typecheck.TString()}}
	if digest == "" {
		digest = cfg.DigestView(fields, nil, nil)
	}
	return &runtime.Library{Schema: &runtime.LibrarySchema{
		HasConfiguration:    true,
		ConfigurationFields: fields,
		ConfigurationDigest: digest,
		Configuration:       map[string]typecheck.Type{"region": typecheck.TString()},
	}}
}

func emptyConfigResourceLibrary() *runtime.Library {
	return &runtime.Library{Schema: &runtime.LibrarySchema{
		HasConfiguration:    true,
		ConfigurationFields: []typecheck.ObjectField{},
		ConfigurationDigest: cfg.DigestView(nil, nil, nil),
		ConfigurationEmpty:  true,
		Configuration:       map[string]typecheck.Type{},
		Resources: map[string]*runtime.TypeSchema{
			"bucket": {Inputs: map[string]typecheck.Type{}},
		},
	}}
}

// TestCheckTypesSkipsUnknownTypedInput proves an input whose type the
// schema could not describe is not required, since its optionality is
// unknowable.
func TestCheckTypesSkipsUnknownTypedInput(t *testing.T) {
	lib := &runtime.Library{
		Schema: &runtime.LibrarySchema{
			Resources: map[string]*runtime.TypeSchema{
				"thing": {
					Inputs: map[string]typecheck.Type{
						"name":   typecheck.TString(),
						"opaque": typecheck.TUnknown(),
					},
				},
			},
		},
	}
	errs := checkSyntaxReferences(t, typeFixture(t, "skips-unknown-typed-input"),
		map[string]*runtime.Library{"ext": lib})

	require.Empty(t, errs.Messages())
}

// TestCheckTypesSkipsSchemalessLibrary proves a library without a
// schema blocks nothing, matching how the rest of the checker treats
// missing schemas.
func TestCheckTypesSkipsSchemalessLibrary(t *testing.T) {
	errs := checkSyntaxReferences(t, typeFixture(t, "skips-schemaless-library"),
		map[string]*runtime.Library{"ext": {}})

	require.Empty(t, errs.Messages())
}

func TestCheckTypesSkipsOpaqueLibraryConfig(t *testing.T) {
	errs := checkSyntaxReferences(t, typeFixture(t, "opaque-library-config"),
		map[string]*runtime.Library{"ext": {}})

	require.Empty(t, errs.Messages())
}

func TestCheckTypesSkipsUnreadableLibraryConfig(t *testing.T) {
	errs := checkSyntaxReferences(t, typeFixture(t, "opaque-library-config"),
		map[string]*runtime.Library{"ext": {Schema: &runtime.LibrarySchema{
			HasConfiguration: true,
		}}})

	require.Empty(t, errs.Messages())
}

func TestCheckTypesRequiresOneLibraryConfigSchema(t *testing.T) {
	errs := checkSyntaxReferences(t, typeFixture(t, "one-library-config-schema"),
		map[string]*runtime.Library{
			"primary": libraryConfigSchemaLibrary("one"),
			"backup":  libraryConfigSchemaLibrary("two"),
		})

	require.Equal(t,
		[]string{`library-config "github.com/acme/aws": aliases disagree on config schema`},
		errs.Messages())
}

func TestCheckTypesAllowsEmptyConfigWithoutBinding(t *testing.T) {
	errs := checkSyntaxReferences(t, typeFixture(t, "empty-config-without-binding"),
		map[string]*runtime.Library{"aws": emptyConfigResourceLibrary()})

	require.Empty(t, errs.Messages())
}

func TestCheckTypesUsesCompositeSyntaxBody(t *testing.T) {
	composite := parseSyntaxCompositeFixture(t, typeFixture(t, "composite-syntax-body"))
	fixture := parseSyntaxFactoryFixture(t, typeFixture(t, "composite-syntax-root"))
	body := composite.body
	checker := NewSyntax(fixture.body, map[string]*runtime.Library{
		"outer": {
			ResourceComposites: map[string]*runtime.CompositeType{
				"greeting": {
					Name:       "greeting",
					SyntaxBody: &body,
				},
			},
		},
		"local": localFileLibrary(),
	})

	got := checker.References(nil).Messages()
	require.ElementsMatch(t, []string{
		`missing required input "path" on outer.greeting`,
		"type mismatch: expected string, got integer",
		"+: operands must both be numbers or both be strings, got string and integer",
	}, got)
}

func TestCheckTypesSkipsWhenInputsSchemaAbsent(t *testing.T) {
	errs := checkSyntaxReferences(t, typeFixture(t, "skips-when-inputs-schema-absent"),
		map[string]*runtime.Library{
			"local": {Schema: &runtime.LibrarySchema{
				Resources: map[string]*runtime.TypeSchema{
					"file": {Outputs: map[string]typecheck.Type{"path": typecheck.TString()}},
				},
			}},
		})
	require.Empty(t, errs.Messages())
}

func TestNewSyntaxUsesRootInputsForTypeChecks(t *testing.T) {
	fixture := parseSyntaxFactoryFixture(t, typeFixture(t, "root-inputs"))

	errs := NewSyntax(fixture.body,
		map[string]*runtime.Library{"local": localFileLibrary()}).References(nil)

	require.Equal(t,
		[]string{"type mismatch: expected string, got integer"},
		errs.Messages())
}

func TestNewSyntaxUsesRootLocalsForTypeChecks(t *testing.T) {
	fixture := parseSyntaxFactoryFixture(t, typeFixture(t, "root-locals"))

	errs := NewSyntax(fixture.body,
		map[string]*runtime.Library{"local": localFileLibrary()}).References(nil)

	require.Equal(t,
		[]string{"type mismatch: expected string, got integer"},
		errs.Messages())
}

func TestNewSyntaxUsesRootConstraints(t *testing.T) {
	fixture := parseSyntaxFactoryFixture(t, typeFixture(t, "root-constraints"))

	errs := NewSyntax(fixture.body, nil).References(nil)

	require.Equal(t,
		[]string{"a constraint may read inputs only, not local.ok"},
		errs.Messages())
}

// TestCheckTypesMergeInfersPreciseObject proves @core.merge of object
// literals reaches a typed field as the precise merged object through
// the full compile pipeline, not as an unknown that checks nothing.
func TestCheckTypesMergeInfersPreciseObject(t *testing.T) {
	errs := checkSyntaxReferences(t, typeFixture(t, "merge-precise-object"),
		map[string]*runtime.Library{"local": localFileLibrary()})
	require.Equal(t,
		[]string{"type mismatch: expected string, got object({ a: integer  b: string })"},
		errs.Messages())
}

// TestCheckTypesMergeOfMapChecksNothing proves a merge holding an
// argument whose keys the checker cannot know infers Unknown, so the
// call checks nothing instead of guessing.
func TestCheckTypesMergeOfMapChecksNothing(t *testing.T) {
	errs := checkSyntaxReferences(t, typeFixture(t, "merge-map-unknown"),
		map[string]*runtime.Library{"local": localFileLibrary()})
	require.Empty(t, errs.Messages())
}
