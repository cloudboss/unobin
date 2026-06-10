package check

import (
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/sdk/cfg"
	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/stretchr/testify/require"
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

// TestCheckTypesRequiresMissingInput proves a body that leaves out a
// required input fails at compile: content has no default and is not
// optional, while mode and create-directory are excused by their
// declared defaults.
func TestCheckTypesRequiresMissingInput(t *testing.T) {
	errs := checkReferences(parseStack(t, `
resources: { local.file.one: { path: 'p' } }
`), map[string]*runtime.Library{"local": localFileLibrary()})

	require.Equal(t,
		[]string{`missing required input "content" on local.file`},
		errs.Messages())
}

// TestCheckTypesReportsEveryMissingInput proves the check reports each
// missing required input by name, in sorted order.
func TestCheckTypesReportsEveryMissingInput(t *testing.T) {
	errs := checkReferences(parseStack(t, `
resources: { local.file.one: { create-directory: true } }
`), map[string]*runtime.Library{"local": localFileLibrary()})

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
	errs := checkReferences(parseStack(t, `
resources: { ext.thing.one: { name: 'a' } }
`), map[string]*runtime.Library{"ext": lib})

	require.Empty(t, errs.Messages())
}

// TestCheckTypesSkipsSchemalessLibrary proves a library without a
// schema blocks nothing, matching how the rest of the checker treats
// missing schemas.
func TestCheckTypesSkipsSchemalessLibrary(t *testing.T) {
	errs := checkReferences(parseStack(t, `
resources: { ext.thing.one: { name: 'a' } }
`), map[string]*runtime.Library{"ext": {}})

	require.Empty(t, errs.Messages())
}

// TestCheckTypesRequiresCompositeInput proves a composite call site
// must provide the composite's required inputs; a declared optional
// input may stay absent.
func TestCheckTypesRequiresCompositeInput(t *testing.T) {
	composite := parseStack(t, `
inputs:    { name: { type: string }, note: { type: optional(string) } }
resources: { local.file.one: { path: var.name, content: 'x' } }
`)
	libs := map[string]*runtime.Library{
		"bundle": {
			ResourceComposites: map[string]*runtime.CompositeType{
				"pair": {
					Name:      "pair",
					Body:      composite,
					Libraries: map[string]*runtime.Library{"local": localFileLibrary()},
				},
			},
		},
	}

	errs := checkReferences(parseStack(t, `
resources: { bundle.pair.demo: {} }
`), libs)
	require.Equal(t,
		[]string{`missing required input "name" on bundle.pair`},
		errs.Messages())

	clean := checkReferences(parseStack(t, `
resources: { bundle.pair.demo: { name: 'n' } }
`), libs)
	require.Empty(t, clean.Messages())
}

func TestCheckTypesCompositeOutputTypes(t *testing.T) {
	composite := parseStack(t, `
inputs:    { name: { type: string } }
resources: { local.file.one: { path: var.name, content: 'x' } }
outputs: {
  path:  { value: resource.local.file.one.path }
  size:  { value: resource.local.file.one.size }
  info:  { value: { host: var.name } }
  names: { value: [var.name] }
}
`)
	libs := func() map[string]*runtime.Library {
		return map[string]*runtime.Library{
			"local": localFileLibrary(),
			"bundle": {ResourceComposites: map[string]*runtime.CompositeType{"pair": {
				Name:      "pair",
				Body:      composite,
				Libraries: map[string]*runtime.Library{"local": localFileLibrary()},
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
  bundle.pair.demo: { name: 'n' }
  local.file.logs:  { path: 'p', content: resource.bundle.pair.demo.info.bogus }
}
`,
			want: []string{`unknown field "bogus" on object({ host: string })`},
		},
		{
			name: "composite output type mismatches a field",
			src: `
resources: {
  bundle.pair.demo: { name: 'n' }
  local.file.logs:  { path: resource.bundle.pair.demo.names, content: 'c' }
}
`,
			want: []string{"type mismatch: expected string, got list(string)"},
		},
		{
			name: "composite output in an operator",
			src: `
resources: {
  bundle.pair.demo: { name: 'n' }
  local.file.logs:  { path: 'p', content: 'c', mode: resource.bundle.pair.demo.size + 'x' }
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
  bundle.pair.demo: { name: 'n' }
  local.file.logs: {
    path:    resource.bundle.pair.demo.path
    content: resource.bundle.pair.demo.info.host
    mode:    resource.bundle.pair.demo.size
  }
}
`,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			errs := checkReferences(parseStack(t, tt.src), libs())
			require.Equal(t, tt.want, errs.Messages())
		})
	}
}

func TestCheckTypesAcceptsMatchingBody(t *testing.T) {
	errs := checkReferences(parseStack(t, `
inputs:    { path: { type: string } }
resources: { local.file.one: { path: var.path, content: 'hi' } }
`), map[string]*runtime.Library{"local": localFileLibrary()})

	require.Empty(t, checkRefMessages(t, errs))
}

func TestCheckTypesRejectsLiteralIntoStringField(t *testing.T) {
	errs := checkReferences(parseStack(t, `
resources: { local.file.one: { path: 5, content: 'hi' } }
`), map[string]*runtime.Library{"local": localFileLibrary()})

	got := errs.Messages()
	require.Len(t, got, 1)
	require.Contains(t, got[0], "expected string, got integer")
}

func TestCheckTypesRejectsVarWithWrongType(t *testing.T) {
	errs := checkReferences(parseStack(t, `
inputs:    { mode: { type: integer } }
resources: { local.file.one: { path: var.mode, content: 'hi' } }
`), map[string]*runtime.Library{"local": localFileLibrary()})

	got := errs.Messages()
	require.Len(t, got, 1)
	require.Contains(t, got[0], "expected string, got integer")
}

func TestCheckTypesAcceptsLocalMatchingField(t *testing.T) {
	errs := checkReferences(parseStack(t, `
locals:    { p: 'somewhere' }
resources: { local.file.one: { path: local.p, content: 'hi' } }
`), map[string]*runtime.Library{"local": localFileLibrary()})

	require.Empty(t, errs.Messages())
}

func TestCheckTypesRejectsLocalWithWrongType(t *testing.T) {
	errs := checkReferences(parseStack(t, `
locals:    { m: 5 }
resources: { local.file.one: { path: local.m, content: 'hi' } }
`), map[string]*runtime.Library{"local": localFileLibrary()})

	got := errs.Messages()
	require.Len(t, got, 1)
	require.Contains(t, got[0], "expected string, got integer")
}

func TestCheckTypesRejectsChainedLocalWithWrongType(t *testing.T) {
	errs := checkReferences(parseStack(t, `
locals:    { raw: 5, derived: local.raw }
resources: { local.file.one: { path: local.derived, content: 'hi' } }
`), map[string]*runtime.Library{"local": localFileLibrary()})

	got := errs.Messages()
	require.Len(t, got, 1)
	require.Contains(t, got[0], "expected string, got integer")
}

func TestCheckTypesRejectsResourceFieldWithWrongType(t *testing.T) {
	errs := checkReferences(parseStack(t, `
resources: {
  local.file.one: { path: 'one', content: 'hi' }
  local.file.two: { path: resource.local.file.one.size, content: 'hi' }
}
`), map[string]*runtime.Library{"local": localFileLibrary()})

	got := errs.Messages()
	require.Len(t, got, 1)
	require.Contains(t, got[0], "expected string, got integer")
}

func TestCheckTypesAcceptsInputFieldReference(t *testing.T) {
	errs := checkReferences(parseStack(t, `
resources: {
  local.file.one: { path: 'one', content: 'hi' }
  local.file.two: { path: resource.local.file.one.content, content: 'hi' }
}
`), map[string]*runtime.Library{"local": localFileLibrary()})

	require.Empty(t, errs.Messages(),
		"content is an input-only field and is readable like an output")
}

func TestCheckTypesRejectsInputFieldReferenceWithWrongType(t *testing.T) {
	errs := checkReferences(parseStack(t, `
resources: {
  local.file.one: { path: 'one', content: 'hi' }
  local.file.two: { path: resource.local.file.one.mode, content: 'hi' }
}
`), map[string]*runtime.Library{"local": localFileLibrary()})

	got := errs.Messages()
	require.Len(t, got, 1)
	require.Contains(t, got[0], "expected string, got integer",
		"an input field keeps its declared type through the reference")
}

func TestCheckTypesAcceptsOptionalIntoRequired(t *testing.T) {
	errs := checkReferences(parseStack(t, `
inputs:    { p: { type: optional(string, 'x') } }
resources: { local.file.one: { path: var.p, content: 'hi' } }
`), map[string]*runtime.Library{"local": localFileLibrary()})

	require.Empty(t, errs.Messages())
}

func TestCheckTypesRejectsListWithWrongElementType(t *testing.T) {
	errs := checkReferences(parseStack(t, `
actions: { core.command.x: { argv: ['echo', 5] } }
`), map[string]*runtime.Library{
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
	errs := checkReferences(parseStack(t, `
actions: { core.command.x: { argv: ['echo', 'hi'] } }
`), map[string]*runtime.Library{
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
	errs := checkReferences(parseStack(t, `
inputs: { region: { type: string } }
constraints: [
  {
    kind: predicate
    when: var.region
    require: var.region == 'us-east-1'
  }
]
`), nil)

	got := errs.Messages()
	require.Len(t, got, 1)
	require.Contains(t, got[0], "expected boolean, got string")
}

func TestCheckTypesRejectsForEachValueIntoWrongSlot(t *testing.T) {
	errs := checkReferences(parseStack(t, `
inputs:    { counts: { type: map(integer) } }
resources: { local.file.many: { @for-each: var.counts, path: @each.value, content: 'hi' } }
`), map[string]*runtime.Library{"local": localFileLibrary()})

	got := errs.Messages()
	require.Len(t, got, 1)
	require.Contains(t, got[0], "expected string, got integer")
}

func TestCheckTypesRejectsUnknownBodyField(t *testing.T) {
	errs := checkReferences(parseStack(t, `
resources: { local.file.one: { paht: 'x', content: 'hi' } }
`), map[string]*runtime.Library{"local": localFileLibrary()})

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
	errs := checkReferences(parseStack(t, `
resources: {
  aws.rds.main:   { name: 'one' }
  local.file.one: { path: resource.aws.rds.main.endpoint.bogus, content: 'hi' }
}
`), map[string]*runtime.Library{
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
	errs := checkReferences(parseStack(t, `
inputs:    { cfg: { type: object({ host: string, port: integer }) } }
resources: { local.file.one: { path: var.cfg.bogus, content: 'hi' } }
`), map[string]*runtime.Library{"local": localFileLibrary()})

	got := errs.Messages()
	require.Len(t, got, 1)
	require.Contains(t, got[0], `unknown field "bogus" on object(`)
}

func TestCheckTypesSkipsWhenInputsSchemaAbsent(t *testing.T) {
	errs := checkReferences(parseStack(t, `
resources: { local.file.one: { path: 5, content: 'hi' } }
`), map[string]*runtime.Library{
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
	errs := checkReferences(parseStack(t, `
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
`), nil)
	require.Equal(t, []string(nil), errs.Messages())

	control := checkReferences(parseStack(t, `
inputs: {
  note: { type: optional(string) }
}
constraints: [
  { kind: predicate, when: true, require: $'{{var.note}}' != '' },
]
`), nil)
	require.Equal(t, []string{
		"interpolation slot may be null; supply a fallback, like " +
			"{{ x ?? '-' }} (got optional(string))",
	}, control.Messages())
}

func TestCheckTypesReportsLocalsBodyErrors(t *testing.T) {
	errs := checkReferences(parseStack(t, `
locals: {
  bad: 'a' - 'b'
}
`), nil)
	want := []string{
		"-: operand must be a number, got string",
		"-: operand must be a number, got string",
	}
	require.Equal(t, want, errs.Messages())
}

func TestCheckTypesReportsLocalsDeepFieldError(t *testing.T) {
	errs := checkReferences(parseStack(t, `
inputs: { cfg: { type: object({ host: string }) } }
locals: {
  h: var.cfg.bogus
}
`), nil)
	want := []string{`unknown field "bogus" on object({ host: string })`}
	require.Equal(t, want, errs.Messages())
}

func TestCheckTypesLocalsErrorsReportOnce(t *testing.T) {
	errs := checkReferences(parseStack(t, `
locals:    { bad: 'a' - 'b' }
resources: { local.file.one: { path: local.bad, content: local.bad } }
`), nil)
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

// configuredLibrary returns a library whose compile-time schema
// declares a configuration with one required and one optional field.
func configuredLibrary() *runtime.Library {
	return &runtime.Library{
		Schema: &runtime.LibrarySchema{
			HasConfiguration: true,
			Configuration: map[string]typecheck.Type{
				"region":  typecheck.TString(),
				"profile": typecheck.TOptional(typecheck.TString()),
			},
		},
	}
}

func TestCheckTypesConfigurationUnknownAlias(t *testing.T) {
	errs := checkReferences(parseStack(t, `
configurations: { ghost: { default: { region: 'r' } } }
`), map[string]*runtime.Library{})
	require.Equal(t,
		[]string{`configurations.ghost: library "ghost" is not imported`},
		errs.Messages())
}

func TestCheckTypesConfigurationOnUnconfiguredLibrary(t *testing.T) {
	errs := checkReferences(parseStack(t, `
configurations: { local: { default: { region: 'r' } } }
`), map[string]*runtime.Library{"local": localFileLibrary()})
	require.Equal(t,
		[]string{`configurations.local: library declares no configuration`},
		errs.Messages())
}

func TestCheckTypesConfigurationUnknownField(t *testing.T) {
	errs := checkReferences(parseStack(t, `
configurations: { aws: { default: { region: 'r', regin: 'oops' } } }
`), map[string]*runtime.Library{"aws": configuredLibrary()})
	require.Equal(t,
		[]string{`configurations.aws.default: unknown field "regin"`},
		errs.Messages())
}

func TestCheckTypesConfigurationFieldTypeMismatch(t *testing.T) {
	errs := checkReferences(parseStack(t, `
configurations: { aws: { default: { region: 5 } } }
`), map[string]*runtime.Library{"aws": configuredLibrary()})
	require.Equal(t,
		[]string{`type mismatch: expected string, got integer`},
		errs.Messages())
}

func TestCheckTypesConfigurationMissingRequiredField(t *testing.T) {
	errs := checkReferences(parseStack(t, `
configurations: { aws: { default: { profile: 'p' } } }
`), map[string]*runtime.Library{"aws": configuredLibrary()})
	require.Equal(t,
		[]string{`configurations.aws.default: missing required field "region"`},
		errs.Messages())
}

func TestCheckTypesConfigurationValidPasses(t *testing.T) {
	errs := checkReferences(parseStack(t, `
inputs: { region: { type: string } }
configurations: { aws: { default: { region: var.region } } }
`), map[string]*runtime.Library{"aws": configuredLibrary()})
	require.Empty(t, errs.Messages())
}

func TestCheckTypesConfigurationDeclaredOnlyAtRuntime(t *testing.T) {
	lib := &runtime.Library{
		Configuration: &cfg.ConfigurationType{New: func() any { return nil }},
		Schema:        &runtime.LibrarySchema{},
	}
	errs := checkReferences(parseStack(t, `
configurations: { aws: { default: { anything: 'goes' } } }
`), map[string]*runtime.Library{"aws": lib})
	require.Empty(t, errs.Messages())
}
