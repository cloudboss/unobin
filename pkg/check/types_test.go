package check

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

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
						{Field: "var.mode", Value: "420"},
						{Field: "var.create-directory", Optional: true},
					},
				},
			},
		},
	}
}

func libraryConfigSchemaLibrary(digest string) *runtime.Library {
	fields := []typecheck.ObjectField{{Name: "region", Type: typecheck.TString()}}
	if digest == "" {
		digest = cfg.DigestView(fields, nil)
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
		ConfigurationDigest: cfg.DigestView(nil, nil),
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
	errs := checkSyntaxReferences(t, `
resources: { one: ext.thing { name: 'a' } }
`, map[string]*runtime.Library{"ext": lib})

	require.Empty(t, errs.Messages())
}

// TestCheckTypesSkipsSchemalessLibrary proves a library without a
// schema blocks nothing, matching how the rest of the checker treats
// missing schemas.
func TestCheckTypesSkipsSchemalessLibrary(t *testing.T) {
	errs := checkSyntaxReferences(t, `
resources: { one: ext.thing { name: 'a' } }
`, map[string]*runtime.Library{"ext": {}})

	require.Empty(t, errs.Messages())
}

func TestCheckTypesRequiresOneLibraryConfigSchema(t *testing.T) {
	errs := checkSyntaxReferences(t, `
imports: {
  primary: 'github.com/acme/aws'
  backup: 'github.com/acme/aws'
}
inputs: {
  aws-config: { type: library-config('github.com/acme/aws') }
}
locals: { region: var.aws-config.region }
`, map[string]*runtime.Library{
		"primary": libraryConfigSchemaLibrary("one"),
		"backup":  libraryConfigSchemaLibrary("two"),
	})

	require.Equal(t,
		[]string{`library-config "github.com/acme/aws": aliases disagree on config schema`},
		errs.Messages())
}

func TestCheckTypesAllowsEmptyConfigWithoutBinding(t *testing.T) {
	errs := checkSyntaxReferences(t, `
imports: { aws: 'github.com/acme/aws' }
resources: { bucket: aws.bucket {} }
`, map[string]*runtime.Library{"aws": emptyConfigResourceLibrary()})

	require.Empty(t, errs.Messages())
}

func TestCheckTypesUsesCompositeSyntaxBody(t *testing.T) {
	composite := parseSyntaxCompositeFixture(t, `
greeting: resource {
  inputs: { path: { type: string } }
  outputs: {
    size:   { value: 42 }
    broken: { value: var.path + 1 }
  }
}
`)
	fixture := parseSyntaxFactoryFixture(t, `
factory: {
  resources: {
    app: outer.greeting {}
    log: local.file { path: resource.app.size, content: 'x' }
  }
}
`)
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

func TestCheckTypesRejectsListWithWrongElementType(t *testing.T) {
	errs := checkSyntaxReferences(t, `
actions: { x: core.command { argv: ['echo', 5] } }
`, map[string]*runtime.Library{
		"core": {Schema: &runtime.LibrarySchema{
			Actions: map[string]*runtime.TypeSchema{
				"command": {
					Inputs: map[string]typecheck.Type{
						"argv": typecheck.TList(typecheck.TString()),
					},
				},
			},
		}},
	})

	got := errs.Messages()
	require.Len(t, got, 1)
	require.Contains(t, got[0], "expected string, got integer")
}

func TestCheckTypesAcceptsListLiteralMatchingTarget(t *testing.T) {
	errs := checkSyntaxReferences(t, `
actions: { x: core.command { argv: ['echo', 'hi'] } }
`, map[string]*runtime.Library{
		"core": {Schema: &runtime.LibrarySchema{
			Actions: map[string]*runtime.TypeSchema{
				"command": {
					Inputs: map[string]typecheck.Type{
						"argv": typecheck.TList(typecheck.TString()),
					},
				},
			},
		}},
	})
	require.Empty(t, errs.Messages())
}

func TestCheckTypesRejectsUnknownFieldOnNestedResourceOutput(t *testing.T) {
	endpoint := typecheck.TObject([]typecheck.ObjectField{
		{Name: "host", Type: typecheck.TString()},
		{Name: "port", Type: typecheck.TInteger()},
	})
	errs := checkSyntaxReferences(t, `
resources: {
  main: aws.rds { name: 'one' }
  one: local.file { path: resource.main.endpoint.bogus, content: 'hi' }
}
`, map[string]*runtime.Library{
		"local": localFileLibrary(),
		"aws": {Schema: &runtime.LibrarySchema{
			Resources: map[string]*runtime.TypeSchema{
				"rds": {
					Inputs: map[string]typecheck.Type{
						"name": typecheck.TString(),
					},
					Outputs: map[string]typecheck.Type{
						"endpoint": endpoint,
					},
				},
			},
		}},
	})

	got := errs.Messages()
	require.Len(t, got, 1)
	require.Contains(t, got[0], `unknown field "bogus" on object(`)
}

func TestCheckTypesRejectsUnknownNestedObjectField(t *testing.T) {
	errs := checkSyntaxReferences(t, `
inputs:    { cfg: { type: object({ host: string, port: integer }) } }
resources: { one: local.file { path: var.cfg.bogus, content: 'hi' } }
`, map[string]*runtime.Library{"local": localFileLibrary()})

	got := errs.Messages()
	require.Len(t, got, 1)
	require.Contains(t, got[0], `unknown field "bogus" on object(`)
}

func TestCheckTypesSkipsWhenInputsSchemaAbsent(t *testing.T) {
	errs := checkSyntaxReferences(t, `
resources: { one: local.file { path: 5, content: 'hi' } }
`, map[string]*runtime.Library{
		"local": {Schema: &runtime.LibrarySchema{
			Resources: map[string]*runtime.TypeSchema{
				"file": {Outputs: map[string]typecheck.Type{"path": typecheck.TString()}},
			},
		}},
	})
	require.Empty(t, errs.Messages())
}

// checkErrorMessages returns the messages of every diagnostic
// regardless of kind. Used by the type-check tests because their
// errors come back as ErrType while reference checks produce
// ErrResolve.
func TestCheckTypesConstraintWhenNarrowsRequire(t *testing.T) {
	errs := checkSyntaxReferences(t, `
inputs: {
  note: { type: optional(string) }
}
constraints: [
  {
    kind: predicate
    require: $'{{var.note}}' != ''
    when: var.note != null
  },
]
`, nil)
	require.Equal(t, []string(nil), errs.Messages())

	control := checkSyntaxReferences(t, `
inputs: {
  note: { type: optional(string) }
}
constraints: [
  { kind: predicate, when: true, require: $'{{var.note}}' != '' },
]
`, nil)
	require.Equal(t, []string{
		"interpolation slot may be null; supply a fallback, like " +
			"{{ x ?? '-' }} (got optional(string))",
	}, control.Messages())
}

func checkErrorMessages(t *testing.T, errs *lang.ErrorList) []string {
	t.Helper()
	require.NotNil(t, errs)
	var out []string
	for _, err := range errs.Errors() {
		out = append(out, err.Msg)
	}
	return out
}

func TestNewSyntaxUsesRootInputsForTypeChecks(t *testing.T) {
	fixture := parseSyntaxFactoryFixture(t, `
factory: {
  inputs: { path: { type: integer } }
  resources: {
    file: local.file {
      path: var.path
      content: 'x'
    }
  }
}
`)

	errs := NewSyntax(fixture.body,
		map[string]*runtime.Library{"local": localFileLibrary()}).References(nil)

	require.Equal(t,
		[]string{"type mismatch: expected string, got integer"},
		errs.Messages())
}

func TestNewSyntaxUsesRootLocalsForTypeChecks(t *testing.T) {
	fixture := parseSyntaxFactoryFixture(t, `
factory: {
  locals: { path: 5 }
  resources: {
    file: local.file {
      path: local.path
      content: 'x'
    }
  }
}
`)

	errs := NewSyntax(fixture.body,
		map[string]*runtime.Library{"local": localFileLibrary()}).References(nil)

	require.Equal(t,
		[]string{"type mismatch: expected string, got integer"},
		errs.Messages())
}

func TestNewSyntaxUsesRootConstraints(t *testing.T) {
	fixture := parseSyntaxFactoryFixture(t, `
factory: {
  locals: { ok: true }
  constraints: [
    { require: local.ok }
  ]
}
`)

	errs := NewSyntax(fixture.body, nil).References(nil)

	require.Equal(t,
		[]string{"a constraint may read inputs only, not local.ok"},
		errs.Messages())
}

// TestCheckTypesMergeInfersPreciseObject proves @core.merge of object
// literals reaches a typed field as the precise merged object through
// the full compile pipeline, not as an unknown that checks nothing.
func TestCheckTypesMergeInfersPreciseObject(t *testing.T) {
	errs := checkSyntaxReferences(t, `
resources: { one: local.file { path: @core.merge({ a: 1 }, { b: 'x' }), content: 'c' } }
`, map[string]*runtime.Library{"local": localFileLibrary()})
	require.Equal(t,
		[]string{"type mismatch: expected string, got object({ a: integer  b: string })"},
		errs.Messages())
}

// TestCheckTypesMergeOfMapChecksNothing proves a merge holding an
// argument whose keys the checker cannot know infers Unknown, so the
// call checks nothing instead of guessing.
func TestCheckTypesMergeOfMapChecksNothing(t *testing.T) {
	errs := checkSyntaxReferences(t, `
inputs: { tags: { type: map(string) } }
resources: { one: local.file { path: @core.merge(var.tags, { a: 'x' }), content: 'c' } }
`, map[string]*runtime.Library{"local": localFileLibrary()})
	require.Empty(t, errs.Messages())
}
