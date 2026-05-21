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
parallelism:    10
state:          { backend: local }
inputs:         { region: 'us-east-1' }
configurations: { aws: { default: {} } }
`
	errs := ValidateTopLevelKeys(parseWithKind(t, src, FileConfig))
	require.Equal(t, 0, errs.Len())
}

func TestValidateTopLevelKeysConfigRejectsDeploymentID(t *testing.T) {
	// The deployment id comes from the config filename basename, so
	// `deployment-id:` is not a permitted top-level key in a config.
	src := `
deployment-id: 'prod'
`
	errs := ValidateTopLevelKeys(parseWithKind(t, src, FileConfig))
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), `"deployment-id"`)
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

func TestValidateRejectsCallToUnimportedModule(t *testing.T) {
	src := `
imports: { core: 'github.com/x/core@v0.1.0' }
outputs: {
  shout: { value: lib.upper(var.name) }
}
`
	f, err := ParseSource("stack.ub", []byte(src))
	require.NoError(t, err)
	errs := ValidateFile(f)
	require.Equal(t, 1, errs.Len(), "got: %v", errsToStrings(errs))
	msg := errs.Errors()[0].Error()
	require.Contains(t, msg, `"lib"`)
	require.Contains(t, msg, "not imported")
}

func TestValidateAcceptsCallToImportedModule(t *testing.T) {
	src := `
imports: { lib: 'github.com/x/lib@v0.1.0' }
outputs: {
  shout: { value: lib.upper(var.name) }
}
`
	f, err := ParseSource("stack.ub", []byte(src))
	require.NoError(t, err)
	errs := ValidateFile(f)
	require.Equal(t, 0, errs.Len(), "got: %v", errsToStrings(errs))
}

func TestValidateChecksCallsInNestedExpressions(t *testing.T) {
	src := `
imports: { core: 'github.com/x/core@v0.1.0' }
resources: {
  core: {
    thing: {
      one: { name: lib.upper('hi') }
    }
  }
}
`
	f, err := ParseSource("stack.ub", []byte(src))
	require.NoError(t, err)
	errs := ValidateFile(f)
	require.Equal(t, 1, errs.Len(), "got: %v", errsToStrings(errs))
	require.Contains(t, errs.Errors()[0].Error(), `"lib"`)
}

func errsToStrings(l *ErrorList) []string {
	es := l.Errors()
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Error()
	}
	return out
}

func TestValidateStateConfigAcceptsBareBackend(t *testing.T) {
	src := `
state: { @backend: local, path: '.unobin/state' }
`
	f := parseWithKind(t, src, FileConfig)
	errs := ValidateFile(f)
	require.Equal(t, 0, errs.Len(), "got: %v", errsToStrings(errs))
}

func TestValidateStateConfigAcceptsAliasedBackend(t *testing.T) {
	src := `
state: {
  @backend: aws.s3
  bucket:   'tf-state'
  region:   'us-east-1'
  encryption: { @key-source: aws.kms, key-id: 'alias/state' }
}
`
	f := parseWithKind(t, src, FileConfig)
	errs := ValidateFile(f)
	require.Equal(t, 0, errs.Len(), "got: %v", errsToStrings(errs))
}

func TestValidateStateConfigRejects(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "missing-backend",
			src:  "state: { path: '.unobin/state' }\n",
			want: "state block: missing required @backend",
		},
		{
			name: "duplicate-backend",
			src:  "state: { @backend: local, @backend: local }\n",
			want: "state block: duplicate @backend",
		},
		{
			name: "unknown-meta-key",
			src:  "state: { @backend: local, @lock-timeout: '30s' }\n",
			want: `state block: unknown meta-key "@lock-timeout"`,
		},
		{
			name: "backend-string-value",
			src:  "state: { @backend: 'local' }\n",
			want: "state block: @backend: expected `name` or `alias.name`",
		},
		{
			name: "backend-too-many-segments",
			src:  "state: { @backend: a.b.c }\n",
			want: "state block: @backend: expected `name` or `alias.name`",
		},
		{
			name: "quoted-body-key",
			src:  "state: { @backend: local, 'path': '.unobin/state' }\n",
			want: "state block key must be a bare identifier",
		},
		{
			name: "duplicate-body-key",
			src:  "state: { @backend: local, path: 'a', path: 'b' }\n",
			want: `state block: duplicate key "path"`,
		},
		{
			name: "encryption-not-an-object",
			src:  "state: { @backend: local, encryption: 'oops' }\n",
			want: "encryption must be an object",
		},
		{
			name: "encryption-missing-key-source",
			src:  "state: { @backend: local, encryption: { env-var: 'X' } }\n",
			want: "encryption block: missing required @key-source",
		},
		{
			name: "encryption-duplicate-key-source",
			src:  "state: { @backend: local, encryption: { @key-source: env-key, @key-source: env-key } }\n",
			want: "encryption block: duplicate @key-source",
		},
		{
			name: "encryption-unknown-meta-key",
			src:  "state: { @backend: local, encryption: { @key-source: env-key, @bogus: 1 } }\n",
			want: `encryption block: unknown meta-key "@bogus"`,
		},
		{
			name: "encryption-bad-key-source-value",
			src:  "state: { @backend: local, encryption: { @key-source: 'env-key' } }\n",
			want: "encryption block: @key-source: expected `name` or `alias.name`",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := parseWithKind(t, c.src, FileConfig)
			errs := ValidateFile(f)
			require.GreaterOrEqual(t, errs.Len(), 1, "expected an error")
			joined := strings.Join(errsToStrings(errs), "; ")
			require.Contains(t, joined, c.want)
		})
	}
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

func parseObjectBlock(t *testing.T, src, key string) *ObjectLit {
	t.Helper()
	f, err := ParseSource("", []byte(src))
	require.NoError(t, err)
	require.NotEmpty(t, f.Body.Fields)
	require.Equal(t, key, f.Body.Fields[0].Key.Name)
	o, ok := f.Body.Fields[0].Value.(*ObjectLit)
	require.True(t, ok, "expected `%s:` to be an object literal", key)
	return o
}

func TestValidateImportsHappy(t *testing.T) {
	src := `
imports: {
  aws:   'github.com/cloudboss/unobin-modules/aws@v0.5.0'
  net:   'github.com/me/modules/network@v1.2.3'
  utils: 'github.com/me/utils@v0.3.0'
  local: './local-modules/foo'
}
`
	errs := ValidateImports(parseObjectBlock(t, src, "imports"))
	require.Equal(t, 0, errs.Len(), "got: %v", errsToStrings(errs))
}

func TestValidateImportsNotString(t *testing.T) {
	src := `
imports: {
  aws: { url: 'github.com/x/y' }
}
`
	errs := ValidateImports(parseObjectBlock(t, src, "imports"))
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Errors()[0].Msg, "quoted-string")
}

func TestValidateImportsDuplicate(t *testing.T) {
	src := `
imports: {
  aws: 'github.com/a/x'
  aws: 'github.com/a/y'
}
`
	errs := ValidateImports(parseObjectBlock(t, src, "imports"))
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Errors()[0].Msg, "duplicate import")
}

func TestValidateImportsRejectsMetaAndStringKeys(t *testing.T) {
	src := `
imports: {
  @bad:   'x'
  'aws':  'github.com/a/y'
}
`
	errs := ValidateImports(parseObjectBlock(t, src, "imports"))
	require.Equal(t, 2, errs.Len())
}

func TestValidateExportsHappy(t *testing.T) {
	src := `
exports: {
  cluster: 'cluster.ub'
  proxy:   'proxy.ub'
}
`
	errs := ValidateExports(parseObjectBlock(t, src, "exports"))
	require.Equal(t, 0, errs.Len())
}

func TestValidateExportsNotString(t *testing.T) {
	src := `
exports: {
  cluster: 42
}
`
	errs := ValidateExports(parseObjectBlock(t, src, "exports"))
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Errors()[0].Msg, "quoted-string")
}

func TestValidateOutputsHappy(t *testing.T) {
	src := `
outputs: {
  cluster-id:  { value: resource.net.cluster.web.id }
  cluster-arn: { value: resource.net.cluster.web.arn }
  region:      { value: var.region }
  static:      { value: 'literal' }
}
`
	errs := ValidateOutputs(parseObjectBlock(t, src, "outputs"))
	require.Equal(t, 0, errs.Len(), "got: %v", errsToStrings(errs))
}

func TestValidateOutputsRejectsBadKeys(t *testing.T) {
	src := `
outputs: {
  ok:        { value: var.x }
  ok:        { value: var.y }
  @bad:      { value: var.z }
  'quoted':  { value: var.q }
}
`
	errs := ValidateOutputs(parseObjectBlock(t, src, "outputs"))
	require.Equal(t, 3, errs.Len(), "got: %v", errsToStrings(errs))
}

func TestValidateOutputsRejectsBareForm(t *testing.T) {
	src := `
outputs: {
  bare: var.x
}
`
	errs := ValidateOutputs(parseObjectBlock(t, src, "outputs"))
	require.Equal(t, 1, errs.Len(), "got: %v", errsToStrings(errs))
	require.Contains(t, errs.Errors()[0].Msg, "wrapper object")
}

func TestValidateOutputsRejectsWrapperMissingValue(t *testing.T) {
	src := `
outputs: {
  bad: { extra: 1 }
}
`
	errs := ValidateOutputs(parseObjectBlock(t, src, "outputs"))
	joined := strings.Join(errsToStrings(errs), "; ")
	require.Contains(t, joined, "unknown wrapper key")
	require.Contains(t, joined, "missing required `value:`")
}

func TestValidateConstraintReferencesHappy(t *testing.T) {
	src := `
inputs: {
  vpc-id:     { type: string }
  subnet-ids: { type: list(string) }
}
constraints: [
  { kind: required-together, fields: [vpc-id, subnet-ids] },
]
`
	f, err := ParseSource("", []byte(src))
	require.NoError(t, err)
	inputs := f.Body.Fields[0].Value.(*ObjectLit)
	constraints := f.Body.Fields[1].Value.(*ArrayLit)

	errs := ValidateConstraintReferences(constraints, inputs)
	require.Equal(t, 0, errs.Len())
}

func TestValidateConstraintReferencesUnknown(t *testing.T) {
	src := `
inputs: {
  vpc-id: { type: string }
}
constraints: [
  { kind: required-together, fields: [vpc-id, missing-name] },
]
`
	f, err := ParseSource("", []byte(src))
	require.NoError(t, err)
	inputs := f.Body.Fields[0].Value.(*ObjectLit)
	constraints := f.Body.Fields[1].Value.(*ArrayLit)

	errs := ValidateConstraintReferences(constraints, inputs)
	require.Equal(t, 1, errs.Len())
	require.Equal(t, ErrResolve, errs.Errors()[0].Kind)
	require.Contains(t, errs.Errors()[0].Msg, "missing-name")
}

func TestValidateFileStack(t *testing.T) {
	src := `
description: 'a stack'
inputs: {
  region: { type: string }
}
constraints: [
  { kind: required-together, fields: [region] },
]
imports: {
  aws: 'github.com/x/y@v1.0.0'
}
outputs: {
  out: { value: var.region }
}
`
	f, err := ParseSource("stack.ub", []byte(src))
	require.NoError(t, err)
	require.Equal(t, FileStack, f.Kind)

	errs := ValidateFile(f)
	require.Equal(t, 0, errs.Len(), "got: %v", errsToStrings(errs))
}

func TestValidateFileStackCollectsCrossErrors(t *testing.T) {
	src := `
inputs: {
  region: { type: string }
  bad:    { description: 'no type' }
}
constraints: [
  { kind: required-together, fields: [region, missing] },
]
imports: {
  aws: 42
}
exports: {
  x: 'y.ub'
}
`
	f, err := ParseSource("stack.ub", []byte(src))
	require.NoError(t, err)

	errs := ValidateFile(f)
	require.GreaterOrEqual(t, errs.Len(), 4, "got: %v", errsToStrings(errs))
}

func TestValidateFileModule(t *testing.T) {
	src := `
description: 'a module'
exports: {
  cluster: 'cluster.ub'
  proxy:   'proxy.ub'
}
`
	f, err := ParseSource("module.ub", []byte(src))
	require.NoError(t, err)
	require.Equal(t, FileModule, f.Kind)

	errs := ValidateFile(f)
	require.Equal(t, 0, errs.Len(), "got: %v", errsToStrings(errs))
}

func TestValidateFileUnknownKind(t *testing.T) {
	src := `description: 'x'`
	f, err := ParseSource("", []byte(src))
	require.NoError(t, err)
	require.Equal(t, FileUnknown, f.Kind)

	errs := ValidateFile(f)
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Errors()[0].Msg, "unknown")
}

func TestValidateResourcesHappy(t *testing.T) {
	src := `
resources: {
  aws: {
    vpc: {
      main: {
        cidr-block: '10.0.0.0/16'
        tags: { Name: 'prod' }
      }
    }
    security-group: {
      web: {
        @depends-on: [resource.aws.vpc.main]
        vpc-id:      resource.aws.vpc.main.id
      }
    }
  }
  net: {
    cluster: {
      web: {
        size: 3
      }
    }
  }
}
`
	errs := ValidateResources(parseObjectBlock(t, src, "resources"))
	require.Equal(t, 0, errs.Len(), "got: %v", errsToStrings(errs))
}

func TestValidateResourcesRejectsBadShape(t *testing.T) {
	src := `
resources: {
  aws: {
    vpc: {
      main: 'not-an-object'
    }
  }
}
`
	errs := ValidateResources(parseObjectBlock(t, src, "resources"))
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Errors()[0].Msg, "body must be an object")
}

func TestValidateResourcesRejectsMetaAtNamespace(t *testing.T) {
	src := `
resources: {
  @bad: { vpc: { main: {} } }
}
`
	errs := ValidateResources(parseObjectBlock(t, src, "resources"))
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Errors()[0].Msg, "@bad")
}

func TestValidateResourcesDuplicateName(t *testing.T) {
	src := `
resources: {
  aws: {
    vpc: {
      main: { cidr: '10.0.0.0/16' }
      main: { cidr: '10.1.0.0/16' }
    }
  }
}
`
	errs := ValidateResources(parseObjectBlock(t, src, "resources"))
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Errors()[0].Msg, "duplicate")
}

func TestValidateResourcesNamespaceNotObject(t *testing.T) {
	src := `
resources: {
  aws: 'oops'
}
`
	errs := ValidateResources(parseObjectBlock(t, src, "resources"))
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Errors()[0].Msg, "must be an object of type names")
}

func TestValidateDataSourcesHappy(t *testing.T) {
	src := `
data: {
  aws: {
    ami: {
      ubuntu: {
        most-recent: true
        owners:      ['099720109477']
      }
    }
  }
}
`
	errs := ValidateDataSources(parseObjectBlock(t, src, "data"))
	require.Equal(t, 0, errs.Len())
}

func TestValidateActionsHappy(t *testing.T) {
	src := `
actions: {
  core: {
    command: {
      smoke-test: {
        @trigger: 'always'
        execute:  'curl -fsS https://example/health'
        @timeout: '30s'
      }
    }
  }
}
`
	errs := ValidateActions(parseObjectBlock(t, src, "actions"))
	require.Equal(t, 0, errs.Len())
}

func TestValidateFileWithResourcesAndActions(t *testing.T) {
	src := `
description: 'a stack'
inputs: {
  size: { type: optional(integer, 3) }
}
resources: {
  aws: {
    vpc: {
      main: { cidr-block: '10.0.0.0/16' }
    }
  }
}
actions: {
  core: {
    command: {
      smoke: { @trigger: 'always', execute: 'echo' }
    }
  }
}
`
	f, err := ParseSource("stack.ub", []byte(src))
	require.NoError(t, err)

	errs := ValidateFile(f)
	require.Equal(t, 0, errs.Len(), "got: %v", errsToStrings(errs))
}
