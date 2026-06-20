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

func configuredResourceLibrary() *runtime.Library {
	lib := libraryConfigSchemaLibrary("")
	lib.Schema.Resources = map[string]*runtime.TypeSchema{
		"bucket": {Inputs: map[string]typecheck.Type{}},
	}
	return lib
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

// TestCheckTypesRequiresMissingInput proves a body that leaves out a
// required input fails at compile: content has no default and is not
// optional, while mode and create-directory are excused by their
// declared defaults.
func TestCheckTypesRequiresMissingInput(t *testing.T) {
	errs := checkSyntaxReferences(t, `
resources: { one: local.file { path: 'p' } }
`, map[string]*runtime.Library{"local": localFileLibrary()})

	require.Equal(t,
		[]string{`missing required input "content" on local.file`},
		errs.Messages())
}

// TestCheckTypesReportsEveryMissingInput proves the check reports each
// missing required input by name, in sorted order.
func TestCheckTypesReportsEveryMissingInput(t *testing.T) {
	errs := checkSyntaxReferences(t, `
resources: { one: local.file { create-directory: true } }
`, map[string]*runtime.Library{"local": localFileLibrary()})

	require.Equal(t, []string{
		`missing required input "content" on local.file`,
		`missing required input "path" on local.file`,
	}, errs.Messages())
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

// TestCheckTypesRequiresCompositeInput proves a composite call site
// must provide the composite's required inputs; a declared optional
// input may stay absent.
func TestCheckTypesRequiresCompositeInput(t *testing.T) {
	composite := parseSyntaxCompositeFixture(t, `
pair: resource {
  inputs:    { name: { type: string }, note: { type: optional(string) } }
  resources: { one: local.file { path: var.name, content: 'x' } }
}
`)
	body := composite.body
	libs := map[string]*runtime.Library{
		"bundle": {
			ResourceComposites: map[string]*runtime.CompositeType{
				"pair": {
					Name:       "pair",
					SyntaxBody: &body,
					Libraries:  map[string]*runtime.Library{"local": localFileLibrary()},
				},
			},
		},
	}

	errs := checkSyntaxReferences(t, `
resources: { demo: bundle.pair {} }
`, libs)
	require.Equal(t,
		[]string{`missing required input "name" on bundle.pair`},
		errs.Messages())

	clean := checkSyntaxReferences(t, `
resources: { demo: bundle.pair { name: 'n' } }
`, libs)
	require.Empty(t, clean.Messages())
}

func TestCheckTypesUsesLibraryConfigInputFields(t *testing.T) {
	errs := checkSyntaxReferences(t, `
imports: {
  aws: 'github.com/acme/aws'
  local: 'github.com/local/file'
}
inputs: {
  aws-config: { type: library-config('github.com/acme/aws') }
}
resources: {
  one: local.file { path: var.aws-config.region content: 'x' }
}
`, map[string]*runtime.Library{
		"aws":   libraryConfigSchemaLibrary(""),
		"local": localFileLibrary(),
	})

	require.Empty(t, errs.Messages())
}

func TestCheckTypesRequiresImportedLibraryConfigPath(t *testing.T) {
	errs := checkSyntaxReferences(t, `
imports: { aws: 'github.com/acme/other' }
inputs: {
  aws-config: { type: library-config('github.com/acme/aws') }
}
locals: { region: var.aws-config.region }
`, map[string]*runtime.Library{"aws": libraryConfigSchemaLibrary("")})

	require.Equal(t,
		[]string{`library-config path "github.com/acme/aws" is not imported in this body`},
		errs.Messages())
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

func TestCheckTypesChecksLibraryConfigBindings(t *testing.T) {
	errs := checkSyntaxReferences(t, `
imports: { aws: 'github.com/acme/aws' }
inputs: { aws-config: { type: library-config('github.com/acme/aws') } }
library-configs: { aws: var.aws-config }
locals: { region: var.aws-config.region }
`, map[string]*runtime.Library{"aws": libraryConfigSchemaLibrary("")})

	require.Empty(t, errs.Messages())
}

func TestCheckTypesRequiresLibraryConfigBindingForLeaf(t *testing.T) {
	errs := checkSyntaxReferences(t, `
imports: { aws: 'github.com/acme/aws' }
inputs: { aws-config: { type: library-config('github.com/acme/aws') } }
resources: { bucket: aws.bucket {} }
`, map[string]*runtime.Library{"aws": configuredResourceLibrary()})

	require.Equal(t,
		[]string{`library "aws" requires library-configs.aws`},
		errs.Messages())
}

func TestCheckTypesAcceptsLibraryConfigBindingForLeaf(t *testing.T) {
	errs := checkSyntaxReferences(t, `
imports: { aws: 'github.com/acme/aws' }
inputs: { aws-config: { type: library-config('github.com/acme/aws') } }
library-configs: { aws: var.aws-config }
resources: { bucket: aws.bucket {} }
`, map[string]*runtime.Library{"aws": configuredResourceLibrary()})

	require.Empty(t, errs.Messages())
}

func TestCheckTypesAllowsEmptyConfigWithoutBinding(t *testing.T) {
	errs := checkSyntaxReferences(t, `
imports: { aws: 'github.com/acme/aws' }
resources: { bucket: aws.bucket {} }
`, map[string]*runtime.Library{"aws": emptyConfigResourceLibrary()})

	require.Empty(t, errs.Messages())
}

func TestCheckTypesChecksLibraryConfigBindingFields(t *testing.T) {
	errs := checkSyntaxReferences(t, `
imports: { aws: 'github.com/acme/aws' }
library-configs: { aws: { region: 1 } }
`, map[string]*runtime.Library{"aws": libraryConfigSchemaLibrary("")})

	require.Equal(t,
		[]string{`type mismatch: expected string, got integer`},
		errs.Messages())
}

func TestCheckTypesRejectsUnknownLibraryConfigBindingAlias(t *testing.T) {
	errs := checkSyntaxReferences(t, `
library-configs: { aws: {} }
`, nil)

	require.Equal(t,
		[]string{`library-configs.aws has no resolved imports`},
		errs.Messages())
}

func TestCheckTypesUsesCompositeLibraryConfigInput(t *testing.T) {
	composite := parseSyntaxCompositeFixture(t, `
app: resource {
  imports: { aws: 'github.com/acme/aws' }
  inputs: { aws-config: { type: library-config('github.com/acme/aws') } }
  outputs: { region: { value: var.aws-config.region } }
}
`)
	body := composite.body
	libs := map[string]*runtime.Library{
		"bundle": {ResourceComposites: map[string]*runtime.CompositeType{"app": {
			Name:       "app",
			SyntaxBody: &body,
			Libraries:  map[string]*runtime.Library{"aws": libraryConfigSchemaLibrary("")},
		}}},
	}

	errs := checkSyntaxReferences(t, `
resources: { demo: bundle.app { aws-config: { region: 1 } } }
`, libs)
	require.Equal(t,
		[]string{`type mismatch: expected string, got integer`},
		errs.Messages())
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

func TestCheckTypesCompositeOutputTypes(t *testing.T) {
	composite := parseSyntaxCompositeFixture(t, `
pair: resource {
  inputs:    { name: { type: string } }
  resources: { one: local.file { path: var.name, content: 'x' } }
  outputs: {
    path:  { value: resource.one.path }
    size:  { value: resource.one.size }
    info:  { value: { host: var.name } }
    names: { value: [var.name] }
  }
}
`)
	body := composite.body
	libs := func() map[string]*runtime.Library {
		return map[string]*runtime.Library{
			"local": localFileLibrary(),
			"bundle": {ResourceComposites: map[string]*runtime.CompositeType{"pair": {
				Name:       "pair",
				SyntaxBody: &body,
				Libraries:  map[string]*runtime.Library{"local": localFileLibrary()},
			}}},
		}
	}
	cases := []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "deep field beyond an object output",
			src: `
resources: {
  demo: bundle.pair { name: 'n' }
  logs: local.file { path: 'p', content: resource.demo.info.bogus }
}
`,
			want: []string{`unknown field "bogus" on object({ host: string })`},
		},
		{
			name: "composite output type mismatches a field",
			src: `
resources: {
  demo: bundle.pair { name: 'n' }
  logs: local.file { path: resource.demo.names, content: 'c' }
}
`,
			want: []string{"type mismatch: expected string, got list(string)"},
		},
		{
			name: "composite output in an operator",
			src: `
resources: {
  demo: bundle.pair { name: 'n' }
  logs: local.file { path: 'p', content: 'c', mode: resource.demo.size + 'x' }
}
`,
			want: []string{
				"+: operands must both be numbers or both be strings, got integer and string",
			},
		},
		{
			name: "matching composite outputs pass",
			src: `
resources: {
  demo: bundle.pair { name: 'n' }
  logs: local.file {
    path:    resource.demo.path
    content: resource.demo.info.host
    mode:    resource.demo.size
  }
}
`,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			errs := checkSyntaxReferences(t, tt.src, libs())
			require.Equal(t, tt.want, errs.Messages())
		})
	}
}

func TestCheckTypesAcceptsMatchingBody(t *testing.T) {
	errs := checkSyntaxReferences(t, `
inputs:    { path: { type: string } }
resources: { one: local.file { path: var.path, content: 'hi' } }
`, map[string]*runtime.Library{"local": localFileLibrary()})

	require.Empty(t, checkRefMessages(t, errs))
}

func TestCheckTypesRejectsLiteralIntoStringField(t *testing.T) {
	errs := checkSyntaxReferences(t, `
resources: { one: local.file { path: 5, content: 'hi' } }
`, map[string]*runtime.Library{"local": localFileLibrary()})

	got := errs.Messages()
	require.Len(t, got, 1)
	require.Contains(t, got[0], "expected string, got integer")
}

func TestCheckTypesRejectsVarWithWrongType(t *testing.T) {
	errs := checkSyntaxReferences(t, `
inputs:    { mode: { type: integer } }
resources: { one: local.file { path: var.mode, content: 'hi' } }
`, map[string]*runtime.Library{"local": localFileLibrary()})

	got := errs.Messages()
	require.Len(t, got, 1)
	require.Contains(t, got[0], "expected string, got integer")
}

func TestCheckTypesAcceptsLocalMatchingField(t *testing.T) {
	errs := checkSyntaxReferences(t, `
locals:    { p: 'somewhere' }
resources: { one: local.file { path: local.p, content: 'hi' } }
`, map[string]*runtime.Library{"local": localFileLibrary()})

	require.Empty(t, errs.Messages())
}

func TestCheckTypesRejectsLocalWithWrongType(t *testing.T) {
	errs := checkSyntaxReferences(t, `
locals:    { m: 5 }
resources: { one: local.file { path: local.m, content: 'hi' } }
`, map[string]*runtime.Library{"local": localFileLibrary()})

	got := errs.Messages()
	require.Len(t, got, 1)
	require.Contains(t, got[0], "expected string, got integer")
}

func TestCheckTypesRejectsChainedLocalWithWrongType(t *testing.T) {
	errs := checkSyntaxReferences(t, `
locals:    { raw: 5, derived: local.raw }
resources: { one: local.file { path: local.derived, content: 'hi' } }
`, map[string]*runtime.Library{"local": localFileLibrary()})

	got := errs.Messages()
	require.Len(t, got, 1)
	require.Contains(t, got[0], "expected string, got integer")
}

func TestCheckTypesRejectsResourceFieldWithWrongType(t *testing.T) {
	errs := checkSyntaxReferences(t, `
resources: {
  one: local.file { path: 'one', content: 'hi' }
  two: local.file { path: resource.one.size, content: 'hi' }
}
`, map[string]*runtime.Library{"local": localFileLibrary()})

	got := errs.Messages()
	require.Len(t, got, 1)
	require.Contains(t, got[0], "expected string, got integer")
}

func TestCheckTypesAcceptsInputFieldReference(t *testing.T) {
	errs := checkSyntaxReferences(t, `
resources: {
  one: local.file { path: 'one', content: 'hi' }
  two: local.file { path: resource.one.content, content: 'hi' }
}
`, map[string]*runtime.Library{"local": localFileLibrary()})

	require.Empty(t, errs.Messages(),
		"content is an input-only field and is readable like an output")
}

func TestCheckTypesRejectsInputFieldReferenceWithWrongType(t *testing.T) {
	errs := checkSyntaxReferences(t, `
resources: {
  one: local.file { path: 'one', content: 'hi' }
  two: local.file { path: resource.one.mode, content: 'hi' }
}
`, map[string]*runtime.Library{"local": localFileLibrary()})

	got := errs.Messages()
	require.Len(t, got, 1)
	require.Contains(t, got[0], "expected string, got integer",
		"an input field keeps its declared type through the reference")
}

func TestCheckTypesAcceptsDefaultedRequiredInput(t *testing.T) {
	errs := checkSyntaxReferences(t, `
inputs:    { p: { type: string, default: 'x' } }
resources: { one: local.file { path: var.p, content: 'hi' } }
`, map[string]*runtime.Library{"local": localFileLibrary()})

	require.Empty(t, errs.Messages())
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

func TestCheckTypesRejectsConstraintWithNonBooleanPredicate(t *testing.T) {
	errs := checkSyntaxReferences(t, `
inputs: { region: { type: string } }
constraints: [
  {
    kind: predicate
    when: var.region
    require: var.region == 'us-east-1'
  }
]
`, nil)

	got := errs.Messages()
	require.Len(t, got, 1)
	require.Contains(t, got[0], "expected boolean, got string")
}

func TestCheckTypesRejectsForEachValueIntoWrongSlot(t *testing.T) {
	errs := checkSyntaxReferences(t, `
inputs:    { counts: { type: map(integer) } }
resources: { many: local.file { @for-each: var.counts, path: @each.value, content: 'hi' } }
`, map[string]*runtime.Library{"local": localFileLibrary()})

	got := errs.Messages()
	require.Len(t, got, 1)
	require.Contains(t, got[0], "expected string, got integer")
}

func TestCheckTypesRejectsUnknownBodyField(t *testing.T) {
	errs := checkSyntaxReferences(t, `
resources: { one: local.file { paht: 'x', content: 'hi' } }
`, map[string]*runtime.Library{"local": localFileLibrary()})

	got := errs.Messages()
	require.Len(t, got, 2)
	require.Contains(t, got[0], `missing required input "path" on local.file`)
	require.Contains(t, got[1], `unknown field "paht" on local.file`)
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

func TestCheckTypesReportsLocalsBodyErrors(t *testing.T) {
	errs := checkSyntaxReferences(t, `
locals: {
  bad: 'a' - 'b'
}
`, nil)
	want := []string{
		"-: operand must be a number, got string",
		"-: operand must be a number, got string",
	}
	require.Equal(t, want, errs.Messages())
}

func TestCheckTypesReportsLocalsDeepFieldError(t *testing.T) {
	errs := checkSyntaxReferences(t, `
inputs: { cfg: { type: object({ host: string }) } }
locals: {
  h: var.cfg.bogus
}
`, nil)
	want := []string{`unknown field "bogus" on object({ host: string })`}
	require.Equal(t, want, errs.Messages())
}

func TestCheckTypesLocalsErrorsReportOnce(t *testing.T) {
	errs := checkSyntaxReferences(t, `
locals:    { bad: 'a' - 'b' }
resources: { one: local.file { path: local.bad, content: local.bad } }
`, nil)
	want := []string{
		"-: operand must be a number, got string",
		"-: operand must be a number, got string",
	}
	require.Equal(t, want, errs.Messages())
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
