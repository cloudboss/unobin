package lang

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func parseWithKind(t *testing.T, src string, kind FileKind) *File {
	t.Helper()
	f, err := ParseSource("", []byte(src))
	require.NoError(t, err)
	f.Kind = kind
	return f
}

func TestValidateTopLevelKeysStack(t *testing.T) {
	src := `
description: 'test'
inputs:      {}
constraints: []
imports:     {}
data:        {}
resources:   {}
actions:     {}
outputs:     {}
`
	errs := ValidateTopLevelKeys(parseWithKind(t, src, FileStack))
	require.Equal(t, 0, errs.Len(), "expected no errors, got: %v", errs.Errors())
}

func TestValidateTopLevelKeysModule(t *testing.T) {
	src := `
description: 'a module'
exports:     { cluster: 'cluster.ub' }
`
	errs := ValidateTopLevelKeys(parseWithKind(t, src, FileModule))
	require.Equal(t, 0, errs.Len())
}

func TestValidateTopLevelKeysConfig(t *testing.T) {
	src := `
stack:          { source: 'github.com/x/y' }
deployment-id:  'prod'
parallelism:    10
state:          { backend: local }
inputs:         { region: 'us-east-1' }
configurations: { aws: { default: {} } }
`
	errs := ValidateTopLevelKeys(parseWithKind(t, src, FileConfig))
	require.Equal(t, 0, errs.Len())
}

func TestValidateRejectsForeignKeys(t *testing.T) {
	cases := []struct {
		name   string
		kind   FileKind
		src    string
		badKey string
	}{
		{
			name:   "stack-with-exports",
			kind:   FileStack,
			src:    "exports: { x: 'y.ub' }\n",
			badKey: "exports",
		},
		{
			name:   "module-with-inputs",
			kind:   FileModule,
			src:    "inputs: {}\n",
			badKey: "inputs",
		},
		{
			name:   "config-with-resources",
			kind:   FileConfig,
			src:    "resources: {}\n",
			badKey: "resources",
		},
		{
			name:   "stack-with-state",
			kind:   FileStack,
			src:    "state: { backend: local }\n",
			badKey: "state",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			errs := ValidateTopLevelKeys(parseWithKind(t, c.src, c.kind))
			require.Equal(t, 1, errs.Len())
			require.Contains(t, errs.Errors()[0].Msg, c.badKey)
		})
	}
}

func TestValidateRejectsMetaKey(t *testing.T) {
	src := "@module: 'aws'\n"
	errs := ValidateTopLevelKeys(parseWithKind(t, src, FileStack))
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Errors()[0].Msg, "@-prefixed")
}

func TestValidateRejectsStringKey(t *testing.T) {
	src := "'description': 'x'\n"
	errs := ValidateTopLevelKeys(parseWithKind(t, src, FileStack))
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Errors()[0].Msg, "top level key must be an identifier")
}

func TestValidateRejectsDuplicateKey(t *testing.T) {
	src := `
description: 'first'
description: 'second'
`
	errs := ValidateTopLevelKeys(parseWithKind(t, src, FileStack))
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Errors()[0].Msg, "duplicate")
}

func TestValidateUnknownKindRefuses(t *testing.T) {
	src := "description: 'x'\n"
	errs := ValidateTopLevelKeys(parseWithKind(t, src, FileUnknown))
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Errors()[0].Msg, "unknown")
}

func TestValidateCollectsMultiple(t *testing.T) {
	src := `
exports:    { x: 'y.ub' }
state:      { backend: local }
@bad:       1
'quoted':   2
`
	errs := ValidateTopLevelKeys(parseWithKind(t, src, FileStack))
	require.Equal(t, 4, errs.Len(), "expected 4 errors, got: %s",
		strings.Join(errsToStrings(errs), "; "))
}

func errsToStrings(l *ErrorList) []string {
	es := l.Errors()
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Error()
	}
	return out
}

func parseInputsBlock(t *testing.T, src string) *ObjectLit {
	t.Helper()
	f, err := ParseSource("", []byte(src))
	require.NoError(t, err)
	require.NotEmpty(t, f.Body.Fields)
	require.Equal(t, "inputs", f.Body.Fields[0].Key.Name)
	o, ok := f.Body.Fields[0].Value.(*ObjectLit)
	require.True(t, ok, "expected `inputs:` to be an object literal")
	return o
}

func TestValidateInputDeclarationsHappy(t *testing.T) {
	src := `
inputs: {
  region: {
    type:        string
    description: 'AWS region'
    pattern:     '^[a-z]+'
  }
  size: {
    type:    optional(integer, 3)
    minimum: 1
    maximum: 100
  }
  subnets: {
    type:      list(string)
    min-items: 1
  }
  tags: {
    type:        optional(map(string), {})
    description: 'Resource tags'
    @sensitive:  true
  }
}
`
	errs := ValidateInputDeclarations(parseInputsBlock(t, src))
	require.Equal(t, 0, errs.Len(), "got: %v", errsToStrings(errs))
}

func TestValidateInputMissingType(t *testing.T) {
	src := `
inputs: {
  bad: { description: 'no type' }
}
`
	errs := ValidateInputDeclarations(parseInputsBlock(t, src))
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Errors()[0].Msg, "missing required `type:`")
}

func TestValidateInputBadType(t *testing.T) {
	src := `
inputs: {
  bad: { type: list(weird-thing) }
}
`
	errs := ValidateInputDeclarations(parseInputsBlock(t, src))
	require.Equal(t, 1, errs.Len())
	require.Equal(t, ErrType, errs.Errors()[0].Kind)
}

func TestValidateInputUnknownModifier(t *testing.T) {
	src := `
inputs: {
  region: {
    type:    string
    bogus:   'x'
  }
}
`
	errs := ValidateInputDeclarations(parseInputsBlock(t, src))
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Errors()[0].Msg, "unknown modifier")
}

func TestValidateInputDeclNotObject(t *testing.T) {
	src := `
inputs: {
  region: 'us-east-1'
}
`
	errs := ValidateInputDeclarations(parseInputsBlock(t, src))
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Errors()[0].Msg, "object declaration")
}

func TestValidateInputDuplicateName(t *testing.T) {
	src := `
inputs: {
  region: { type: string }
  region: { type: integer }
}
`
	errs := ValidateInputDeclarations(parseInputsBlock(t, src))
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Errors()[0].Msg, "duplicate input")
}

func TestValidateInputDuplicateModifier(t *testing.T) {
	src := `
inputs: {
  region: {
    type:    string
    pattern: '^a'
    pattern: '^b'
  }
}
`
	errs := ValidateInputDeclarations(parseInputsBlock(t, src))
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Errors()[0].Msg, "duplicate")
}

func TestValidateInputBadMetaKey(t *testing.T) {
	src := `
inputs: {
  region: {
    type:    string
    @module: 'aws'
  }
}
`
	errs := ValidateInputDeclarations(parseInputsBlock(t, src))
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Errors()[0].Msg, "@module")
}

func TestValidateInputCollectsMultiple(t *testing.T) {
	src := `
inputs: {
  one: { description: 'no type' }
  two: { type: weird-atomic, bogus: 1 }
  three: 'not an object'
}
`
	errs := ValidateInputDeclarations(parseInputsBlock(t, src))
	require.GreaterOrEqual(t, errs.Len(), 3,
		"got: %v", errsToStrings(errs))
}

func parseConstraintsBlock(t *testing.T, src string) *ArrayLit {
	t.Helper()
	f, err := ParseSource("", []byte(src))
	require.NoError(t, err)
	require.NotEmpty(t, f.Body.Fields)
	require.Equal(t, "constraints", f.Body.Fields[0].Key.Name)
	a, ok := f.Body.Fields[0].Value.(*ArrayLit)
	require.True(t, ok, "expected `constraints:` to be an array literal")
	return a
}

func TestValidateConstraintsHappy(t *testing.T) {
	src := `
constraints: [
  { kind: exactly-one-of,    fields: [encryption-key, encryption-key-arn] },
  { kind: required-together, fields: [vpc-id, subnet-ids] },
  { kind: mutually-exclusive, fields: [use-spot, reserved-capacity] },
  {
    kind:    predicate
    when:    'var.region == \'us-gov-east-1\''
    require: 'var.fips-mode == true'
    message: 'GovCloud regions require FIPS mode enabled'
  },
]
`
	errs := ValidateConstraints(parseConstraintsBlock(t, src))
	require.Equal(t, 0, errs.Len(), "got: %v", errsToStrings(errs))
}

func TestValidateConstraintEntryNotObject(t *testing.T) {
	src := `
constraints: ['bogus']
`
	errs := ValidateConstraints(parseConstraintsBlock(t, src))
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Errors()[0].Msg, "must be an object")
}

func TestValidateConstraintMissingKind(t *testing.T) {
	src := `
constraints: [
  { fields: [a, b] },
]
`
	errs := ValidateConstraints(parseConstraintsBlock(t, src))
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Errors()[0].Msg, "missing required `kind:`")
}

func TestValidateConstraintUnknownKind(t *testing.T) {
	src := `
constraints: [
  { kind: weird-thing, fields: [a] },
]
`
	errs := ValidateConstraints(parseConstraintsBlock(t, src))
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Errors()[0].Msg, "unknown constraint kind")
}

func TestValidateConstraintFieldsRequired(t *testing.T) {
	src := `
constraints: [
  { kind: required-together },
]
`
	errs := ValidateConstraints(parseConstraintsBlock(t, src))
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Errors()[0].Msg, "requires a `fields:` list")
}

func TestValidateConstraintFieldsEmpty(t *testing.T) {
	src := `
constraints: [
  { kind: required-together, fields: [] },
]
`
	errs := ValidateConstraints(parseConstraintsBlock(t, src))
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Errors()[0].Msg, "must not be empty")
}

func TestValidateConstraintFieldsNotIdent(t *testing.T) {
	src := `
constraints: [
  { kind: required-together, fields: ['quoted-name', 42, valid-name] },
]
`
	errs := ValidateConstraints(parseConstraintsBlock(t, src))
	require.Equal(t, 2, errs.Len(), "got: %v", errsToStrings(errs))
}

func TestValidateConstraintUnknownKeyForFieldsKind(t *testing.T) {
	src := `
constraints: [
  { kind: required-together, fields: [a, b], message: 'x' },
]
`
	errs := ValidateConstraints(parseConstraintsBlock(t, src))
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Errors()[0].Msg, "unknown key")
}

func TestValidateConstraintPredicateMissingWhen(t *testing.T) {
	src := `
constraints: [
  { kind: predicate, require: 'true' },
]
`
	errs := ValidateConstraints(parseConstraintsBlock(t, src))
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Errors()[0].Msg, "`when:`")
}

func TestValidateConstraintPredicateMissingRequire(t *testing.T) {
	src := `
constraints: [
  { kind: predicate, when: 'true' },
]
`
	errs := ValidateConstraints(parseConstraintsBlock(t, src))
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Errors()[0].Msg, "`require:`")
}

func TestValidateConstraintDuplicateKey(t *testing.T) {
	src := `
constraints: [
  { kind: required-together, fields: [a], fields: [b] },
]
`
	errs := ValidateConstraints(parseConstraintsBlock(t, src))
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Errors()[0].Msg, "duplicate")
}
