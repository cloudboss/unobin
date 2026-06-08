package lang

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/ubtest"
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
locals:      {}
constraints: []
imports:     {}
data:        {}
resources:   {}
actions:     {}
outputs:     {}
`
	errs := ValidateTopLevelKeys(parseWithKind(t, src, FileFactory))
	require.Equal(t, 0, errs.Len(), "expected no errors, got: %v", errs.Errors())
}

func TestValidateTopLevelKeysConfig(t *testing.T) {
	src := `
factory:        { source: 'github.com/x/y' }
parallelism:    10
state:          { backend: local }
inputs:         { region: 'us-east-1' }
configurations: { aws: { default: {} } }
`
	errs := ValidateTopLevelKeys(parseWithKind(t, src, FileConfig))
	require.Equal(t, 0, errs.Len())
}

func TestValidateTopLevelKeysConfigRejectsStackName(t *testing.T) {
	// The stack name comes from the config filename basename, so
	// `stack:` is not a permitted top-level key in a config.
	src := `
stack: 'prod'
`
	errs := ValidateTopLevelKeys(parseWithKind(t, src, FileConfig))
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Err().Error(), `"stack"`)
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
			kind:   FileFactory,
			src:    "exports: { x: 'y.ub' }\n",
			badKey: "exports",
		},
		{
			name:   "config-with-resources",
			kind:   FileConfig,
			src:    "resources: {}\n",
			badKey: "resources",
		},
		{
			name:   "stack-with-state",
			kind:   FileFactory,
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
	src := "@library: 'aws'\n"
	errs := ValidateTopLevelKeys(parseWithKind(t, src, FileFactory))
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Errors()[0].Msg, "@-prefixed")
}

func TestValidateRejectsStringKey(t *testing.T) {
	src := "'description': 'x'\n"
	errs := ValidateTopLevelKeys(parseWithKind(t, src, FileFactory))
	require.Equal(t, 1, errs.Len())
	require.Contains(t, errs.Errors()[0].Msg, "top level key must be an identifier")
}

func TestValidateRejectsDuplicateKey(t *testing.T) {
	src := `
description: 'first'
description: 'second'
`
	errs := ValidateTopLevelKeys(parseWithKind(t, src, FileFactory))
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
	errs := ValidateTopLevelKeys(parseWithKind(t, src, FileFactory))
	require.Equal(t, 4, errs.Len(), "expected 4 errors, got: %s",
		strings.Join(errs.Strings(), "; "))
}

func TestValidateManifest(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		wantErr string // a substring of the expected error; "" expects none
	}{
		{
			name: "requires with multiple deps",
			src: `
requires: {
  'github.com/cloudboss/unobin//pkg/libraries/core': 'v0.1.0'
  'github.com/me/net//vpc':                          'v2.0.0'
}
`,
		},
		{
			name: "empty requires",
			src:  "requires: {}\n",
		},
		{
			name:    "version is not allowed",
			src:     "version: 'v1.0.0'\n",
			wantErr: "is not a valid top level key for a manifest file",
		},
		{
			name:    "unknown top-level key",
			src:     "imports: {}\n",
			wantErr: "is not a valid top level key for a manifest file",
		},
		{
			name:    "requires key is a bare identifier",
			src:     "requires: { core: 'v0.1.0' }\n",
			wantErr: "dependency id must be a quoted string",
		},
		{
			name:    "requires value is not a string",
			src:     "requires: { 'github.com/x/y': 1 }\n",
			wantErr: "version must be a quoted string",
		},
		{
			name: "duplicate dependency",
			src: `
requires: {
  'github.com/x/y': 'v1.0.0'
  'github.com/x/y': 'v2.0.0'
}
`,
			wantErr: "duplicate dependency",
		},
		{
			name: "replace maps a url to a local path",
			src: `
requires: { 'github.com/x/y': 'v1.0.0' }
replace:  { 'github.com/cloudboss/unobin-library-aws': '../../../..' }
`,
		},
		{
			name:    "replace key is a bare identifier",
			src:     "replace: { aws: '../aws' }\n",
			wantErr: "replace: dependency id must be a quoted string",
		},
		{
			name:    "replace value is not a string",
			src:     "replace: { 'github.com/x/y': 1 }\n",
			wantErr: "replace: dependency \"github.com/x/y\": local path must be a quoted string",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, err := ParseSource("unobin.manifest", []byte(c.src))
			require.NoError(t, err)
			require.Equal(t, FileManifest, f.Kind)
			errs := ValidateFile(f)
			if c.wantErr == "" {
				require.Equal(t, 0, errs.Len(), "unexpected errors: %v", errs.Errors())
				return
			}
			require.Positive(t, errs.Len())
			require.Contains(t, errs.Err().Error(), c.wantErr)
		})
	}
}

func TestValidateReservesSetType(t *testing.T) {
	src := `inputs: { a: { type: set(string) } }`
	f, err := ParseSource("main.ub", []byte(src))
	require.NoError(t, err)
	errs := ValidateFile(f)
	require.Equal(t,
		[]string{"main.ub:1:22: type: set is not available yet; use list, or a map for fan-out"},
		errs.Strings())
}

func TestValidateAcceptsOpenObjectType(t *testing.T) {
	src := `inputs: { p: { type: optional(open(object({ a: optional(list(string)) })), {}) } }`
	f, err := ParseSource("main.ub", []byte(src))
	require.NoError(t, err)
	errs := ValidateFile(f)
	require.Equal(t, 0, errs.Len(), "unexpected errors: %v", errs.Strings())
}

func TestValidateRejectsAnyType(t *testing.T) {
	src := `inputs: { a: { type: any } }`
	f, err := ParseSource("main.ub", []byte(src))
	require.NoError(t, err)
	errs := ValidateFile(f)
	require.Equal(t,
		[]string{"main.ub:1:22: type: any is not a type; " +
			"use opaque for a value passed along unread, or declare the value's type"},
		errs.Strings())
}

func TestValidateRejectsCallToUnimportedModule(t *testing.T) {
	src := `
imports: { core: 'github.com/x/core' }
outputs: {
  shout: { value: lib.upper(var.name) }
}
`
	f, err := ParseSource("main.ub", []byte(src))
	require.NoError(t, err)
	errs := ValidateFile(f)
	require.Equal(t, 1, errs.Len(), "got: %v", errs.Strings())
	msg := errs.Errors()[0].Error()
	require.Contains(t, msg, `"lib"`)
	require.Contains(t, msg, "not imported")
}

func TestValidateAcceptsCallToImportedModule(t *testing.T) {
	src := `
imports: { lib: 'github.com/x/lib' }
outputs: {
  shout: { value: lib.upper(var.name) }
}
`
	f, err := ParseSource("main.ub", []byte(src))
	require.NoError(t, err)
	errs := ValidateFile(f)
	require.Equal(t, 0, errs.Len(), "got: %v", errs.Strings())
}

func TestValidateChecksCallsInNestedExpressions(t *testing.T) {
	src := `
imports: { core: 'github.com/x/core' }
resources: {
  core.thing.one: { name: lib.upper('hi') }
}
`
	f, err := ParseSource("main.ub", []byte(src))
	require.NoError(t, err)
	errs := ValidateFile(f)
	require.Equal(t, 1, errs.Len(), "got: %v", errs.Strings())
	require.Contains(t, errs.Errors()[0].Error(), `"lib"`)
}

func TestValidateRejectsBareCall(t *testing.T) {
	src := `
outputs: {
  shout: { value: format('%s', var.name) }
}
`
	f, err := ParseSource("main.ub", []byte(src))
	require.NoError(t, err)
	errs := ValidateFile(f)
	require.Equal(t, 1, errs.Len(), "got: %v", errs.Strings())
	msg := errs.Errors()[0].Error()
	require.Contains(t, msg, "must be qualified")
	require.Contains(t, msg, "format")
}

// TestValidateAdmitsCoreNamespaceCall proves a @core call needs no
// import: the namespace is part of the language.
func TestValidateAdmitsCoreNamespaceCall(t *testing.T) {
	src := `
outputs: {
  shout: { value: @core.b64-encode('hi') }
}
`
	f, err := ParseSource("main.ub", []byte(src))
	require.NoError(t, err)
	errs := ValidateFile(f)
	require.Equal(t, 0, errs.Len(), "got: %v", errs.Strings())
}

// TestValidateRejectsUnknownNamespaceCall proves @core is the only
// language namespace a call may use.
func TestValidateRejectsUnknownNamespaceCall(t *testing.T) {
	src := `
outputs: {
  shout: { value: @std.format('%s', 'hi') }
}
`
	f, err := ParseSource("main.ub", []byte(src))
	require.NoError(t, err)
	errs := ValidateFile(f)
	require.Equal(t, 1, errs.Len(), "got: %v", errs.Strings())
	msg := errs.Errors()[0].Error()
	require.Contains(t, msg, "@std")
	require.Contains(t, msg, "@core")
}

// TestValidateRejectsAtPrefixedImportAlias proves the @ namespace stays
// the language's: an import cannot claim a name there.
func TestValidateRejectsAtPrefixedImportAlias(t *testing.T) {
	src := `
imports: {
  @core: 'github.com/x/y'
}
`
	f, err := ParseSource("main.ub", []byte(src))
	require.NoError(t, err)
	errs := ValidateFile(f)
	require.Equal(t, 1, errs.Len(), "got: %v", errs.Strings())
	require.Contains(t, errs.Errors()[0].Error(),
		`@-prefixed key "@core" is not a valid import name`)
}

func TestValidateCallsTypePositions(t *testing.T) {
	const ok = "" // no error expected
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"atomic type", `inputs: { a: { type: string } }`, ok},
		{"list", `inputs: { a: { type: list(string) } }`, ok},
		{"set skips call checking", `inputs: { a: { type: set(string) } }`, ok},
		{"map", `inputs: { a: { type: map(integer) } }`, ok},
		{"nested list", `inputs: { a: { type: list(list(string)) } }`, ok},
		{"optional no default", `inputs: { a: { type: optional(integer) } }`, ok},
		{"optional literal default", `inputs: { a: { type: optional(string, 'x') } }`, ok},
		{"optional map default", `inputs: { a: { type: optional(map(string), {}) } }`, ok},
		{"object fields", `inputs: { a: { type: object({ p: { type: integer }, q: string }) } }`, ok},
		{"tuple elements", `inputs: { a: { type: tuple([string, integer]) } }`, ok},
		{
			"qualified call in optional default",
			"imports: { core: 'github.com/x/core' }\n" +
				"inputs: { a: { type: optional(string, core.format('hi')) } }",
			ok,
		},
		{
			"bare call in optional default",
			`inputs: { a: { type: optional(integer, pick()) } }`,
			"must be qualified",
		},
		{
			"bare call in object field default",
			`inputs: { a: { type: object({ p: { type: integer, default: pick() } }) } }`,
			"must be qualified",
		},
		{
			"type attribute in a resource body",
			`resources: { core.thing.it: { type: pick() } }`,
			"must be qualified",
		},
		{
			"type attribute in a data body",
			`data: { core.lookup.it: { type: pick() } }`,
			"must be qualified",
		},
		{"constructor name in value position", `outputs: { o: { value: list(1) } }`, "must be qualified"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, err := ParseSource("main.ub", []byte(c.src))
			require.NoError(t, err)
			got := ValidateCalls(f).Strings()
			if c.want == "" {
				require.Empty(t, got)
				return
			}
			require.Len(t, got, 1)
			require.Contains(t, got[0], c.want)
		})
	}
}

func TestValidateConfigInputs(t *testing.T) {
	const ok = "" // no error expected
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"string literal", `inputs: { region: 'us-east-1' }`, ok},
		{"number and bool", `inputs: { size: 5, spot: true }`, ok},
		{"list and map of literals", `inputs: { ports: [80, 443], tags: { env: 'prod' } }`, ok},
		{"operators over literals", `inputs: { n: 1 + 2 * 3 }`, ok},
		{"conditional over literals", `inputs: { r: if true then 3 else 1 }`, ok},
		{"list comprehension over a literal", `inputs: { xs: [for n in [1, 2, 3]: n] }`, ok},
		{"map comprehension over a literal", `inputs: { m: { for n in [1, 2]: n => n } }`, ok},
		{"comprehension with two bindings", `inputs: { xs: [for i, n in [10, 20]: n] }`, ok},
		{"interpolation with a static slot", `inputs: { s: $'n={{1}}' }`, ok},
		{"bare call", `inputs: { x: pick() }`, "is a function call"},
		{"qualified call", `inputs: { x: core.format('hi') }`, "is a function call"},
		{"var reference", `inputs: { x: var.other }`, "is a reference"},
		{"resource reference", `inputs: { x: resource.a.b.c }`, "is a reference"},
		{"bare ident reference", `inputs: { x: somename }`, "is a reference"},
		{"call nested in a list", `inputs: { x: [1, pick()] }`, "is a function call"},
		{"call nested in a map", `inputs: { x: { a: pick() } }`, "is a function call"},
		{"reference in comprehension source", `inputs: { x: [for n in var.xs: n] }`, "is a reference"},
		{"reference in comprehension body", `inputs: { x: [for n in [1, 2]: var.y] }`, "is a reference"},
		{"reference in interpolation slot", `inputs: { s: $'v={{var.x}}' }`, "is a reference"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, err := ParseSource("config.ub", []byte(c.src))
			require.NoError(t, err)
			f.Kind = FileConfig
			got := ValidateFile(f).Strings()
			if c.want == "" {
				require.Empty(t, got)
				return
			}
			require.Len(t, got, 1)
			require.Contains(t, got[0], c.want)
		})
	}
}

func TestValidateConfigurations(t *testing.T) {
	const ok = "" // no error expected
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"literal values", `configurations: { aws: { default: { region: 'us-east-1' } } }`, ok},
		{
			"multiple configs and fields",
			`configurations: { aws: { default: { region: 'us-east-1' }, formal: { region: 'eu-west-1', profile: 'prod' } } }`,
			ok,
		},
		{
			"nested literal list and map",
			`configurations: { aws: { default: { zones: ['a', 'b'], tags: { env: 'prod' } } } }`,
			ok,
		},
		{
			"static expression",
			`configurations: { aws: { default: { region: if true then 'a' else 'b' } } }`,
			ok,
		},
		{
			"qualified call",
			`configurations: { aws: { default: { region: core.format('x') } } }`,
			"is a function call",
		},
		{"bare call", `configurations: { aws: { default: { region: pick() } } }`, "is a function call"},
		{
			"var reference",
			`configurations: { aws: { default: { region: var.region } } }`,
			"is a reference",
		},
		{
			"resource reference",
			`configurations: { aws: { default: { region: resource.a.b.c } } }`,
			"is a reference",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, err := ParseSource("config.ub", []byte(c.src))
			require.NoError(t, err)
			f.Kind = FileConfig
			got := ValidateFile(f).Strings()
			if c.want == "" {
				require.Empty(t, got)
				return
			}
			require.Len(t, got, 1)
			require.Contains(t, got[0], c.want)
		})
	}
}

func TestValidateStateConfigAcceptsBareBackend(t *testing.T) {
	src := `
state: { @backend: local, path: '.unobin/state' }
`
	f := parseWithKind(t, src, FileConfig)
	errs := ValidateFile(f)
	require.Equal(t, 0, errs.Len(), "got: %v", errs.Strings())
}

func TestValidateStateConfigRejectsDottedBackend(t *testing.T) {
	src := `
state: {
  @backend: aws.s3
  encryption: { @key-source: aws.kms }
}
`
	f := parseWithKind(t, src, FileConfig)
	errs := ValidateFile(f)
	require.NotZero(t, errs.Len())
	require.Contains(t, strings.Join(errs.Strings(), "; "), "not a qualified reference")
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
			want: "state block: @backend: expected a bare name like local",
		},
		{
			name: "backend-too-many-segments",
			src:  "state: { @backend: a.b.c }\n",
			want: "state block: @backend: use a bare name like local, not a qualified reference",
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
			want: "encryption block: @key-source: expected a bare name like local",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := parseWithKind(t, c.src, FileConfig)
			errs := ValidateFile(f)
			require.GreaterOrEqual(t, errs.Len(), 1, "expected an error")
			joined := strings.Join(errs.Strings(), "; ")
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

func constraintsBlock(f *File) (*ArrayLit, bool) {
	if len(f.Body.Fields) == 0 || f.Body.Fields[0].Key.Name != "constraints" {
		return nil, false
	}
	a, ok := f.Body.Fields[0].Value.(*ArrayLit)
	return a, ok
}

func parseConstraintsBlock(t *testing.T, src string) *ArrayLit {
	t.Helper()
	f, err := ParseSource("", []byte(src))
	require.NoError(t, err)
	block, ok := constraintsBlock(f)
	require.True(t, ok, "expected `constraints:` to be an array literal")
	return block
}

func TestValidateConstraintsFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/constraints", func(name string, src []byte) (string, []string) {
		f, err := ParseSource("", src)
		if err != nil {
			return "", []string{err.Error()}
		}
		block, ok := constraintsBlock(f)
		if !ok {
			return "", []string{"fixture must begin with a constraints: array"}
		}
		return "", ValidateConstraints(block).Messages()
	})
}

func namedObjectBlock(f *File, key string) (*ObjectLit, bool) {
	if len(f.Body.Fields) == 0 || f.Body.Fields[0].Key.Name != key {
		return nil, false
	}
	o, ok := f.Body.Fields[0].Value.(*ObjectLit)
	return o, ok
}

func parseObjectBlock(t *testing.T, src, key string) *ObjectLit {
	t.Helper()
	f, err := ParseSource("", []byte(src))
	require.NoError(t, err)
	o, ok := namedObjectBlock(f, key)
	require.True(t, ok, "expected `%s:` to be an object literal", key)
	return o
}

// objectBlockDriver runs the named top-level object block through validate,
// reporting its diagnostics as the fixture's expected errors.
func objectBlockDriver(key string, validate func(*ObjectLit) *ErrorList) ubtest.Driver {
	return func(name string, src []byte) (string, []string) {
		f, err := ParseSource("", src)
		if err != nil {
			return "", []string{err.Error()}
		}
		o, ok := namedObjectBlock(f, key)
		if !ok {
			return "", []string{"fixture must begin with a " + key + ": object"}
		}
		return "", validate(o).Messages()
	}
}

func TestValidateImportsFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/imports", objectBlockDriver("imports", ValidateImports))
}

func TestValidateOutputsFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/outputs", objectBlockDriver("outputs", ValidateOutputs))
}

func TestValidateLocalsFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/locals", objectBlockDriver("locals", ValidateLocals))
}

func TestValidateResourcesFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/resources", objectBlockDriver("resources", ValidateResources))
}

func TestValidateDataSourcesFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/data", objectBlockDriver("data", ValidateDataSources))
}

func TestValidateActionsFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/actions", objectBlockDriver("actions", ValidateActions))
}

func TestValidateInputsFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/inputs", objectBlockDriver("inputs", ValidateInputDeclarations))
}

func TestValidateConstraintReferencesHappy(t *testing.T) {
	src := `
inputs: {
  vpc-id:     { type: string }
  subnet-ids: { type: list(string) }
}
constraints: [
  { kind: required-together, fields: [var.vpc-id, var.subnet-ids] },
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
  { kind: required-together, fields: [var.vpc-id, var.missing-name] },
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

func TestValidateConstraintReferencesNested(t *testing.T) {
	src := `
inputs: {
  code: { type: optional(object({ inline: optional(string) })) }
}
constraints: [
  { kind: at-least-one-of, fields: [var.code.inline, var.bogus.inline] },
]
`
	f, err := ParseSource("", []byte(src))
	require.NoError(t, err)
	inputs := f.Body.Fields[0].Value.(*ObjectLit)
	constraints := f.Body.Fields[1].Value.(*ArrayLit)

	errs := ValidateConstraintReferences(constraints, inputs)
	require.Equal(t, 1, errs.Len(), "got: %v", errs.Strings())
	require.Equal(t, ErrResolve, errs.Errors()[0].Kind)
	require.Contains(t, errs.Errors()[0].Msg, "bogus")
}

func TestValidateConstraintReferencesSplatAndIndexRoots(t *testing.T) {
	src := `
inputs: {
  replicas:  { type: list(object({ host: optional(string) })) }
  listeners: { type: list(object({ cert: optional(string) })) }
}
constraints: [
  {
    kind: required-together
    fields: [var.replicas[*].host, var.listeners[0].cert, var.volumes[*].id]
  },
]
`
	f, err := ParseSource("", []byte(src))
	require.NoError(t, err)
	inputs := f.Body.Fields[0].Value.(*ObjectLit)
	constraints := f.Body.Fields[1].Value.(*ArrayLit)

	errs := ValidateConstraintReferences(constraints, inputs)
	require.Equal(t, 1, errs.Len(), "got: %v", errs.Strings())
	require.Equal(t, ErrResolve, errs.Errors()[0].Kind)
	require.Contains(t, errs.Errors()[0].Msg, `input "volumes" not declared`)
}

func TestValidateFileStack(t *testing.T) {
	src := `
description: 'a stack'
inputs: {
  region: { type: string }
}
constraints: [
  { kind: required-together, fields: [var.region] },
]
imports: {
  aws: 'github.com/x/y'
}
outputs: {
  out: { value: var.region }
}
`
	f, err := ParseSource("main.ub", []byte(src))
	require.NoError(t, err)
	require.Equal(t, FileFactory, f.Kind)

	errs := ValidateFile(f)
	require.Equal(t, 0, errs.Len(), "got: %v", errs.Strings())
}

func TestValidateFileStackCollectsCrossErrors(t *testing.T) {
	src := `
inputs: {
  region: { type: string }
  bad:    { description: 'no type' }
}
constraints: [
  { kind: required-together, fields: [var.region, var.missing] },
]
imports: {
  aws: 42
}
exports: {
  x: 'y.ub'
}
`
	f, err := ParseSource("main.ub", []byte(src))
	require.NoError(t, err)

	errs := ValidateFile(f)
	require.GreaterOrEqual(t, errs.Len(), 4, "got: %v", errs.Strings())
}

func TestValidateFileExportedType(t *testing.T) {
	src := `
description: 'a composite'
inputs:  { name: { type: string } }
outputs: { name: { value: var.name } }
`
	f := parseWithKind(t, src, FileExportedType)
	errs := ValidateFile(f)
	require.Equal(t, 0, errs.Len(), "got: %v", errs.Strings())
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

func TestValidateBodyMetaKeys(t *testing.T) {
	tests := []struct {
		name  string
		block string // resources, data, or actions
		body  string
		want  []string
	}{
		{name: "resource plain inputs", block: "resources", body: "path: '/x'"},
		{name: "resource for-each", block: "resources", body: "@for-each: ['a']"},
		{name: "resource configuration", block: "resources", body: "@configuration: aws.east"},
		{name: "resource configurations", block: "resources",
			body: "@configurations: { aws: aws.east }"},
		{name: "resource depends-on", block: "resources", body: "@depends-on: ['x']"},
		{name: "resource lock", block: "resources", body: "@lock: 'x'"},
		{name: "resource rejects trigger", block: "resources", body: "@trigger: 'always'",
			want: []string{`resource aws.vpc.this: meta key "@trigger" is not allowed`}},
		{name: "resource rejects unknown", block: "resources", body: "@bogus: 1",
			want: []string{`resource aws.vpc.this: meta key "@bogus" is not allowed`}},
		{name: "resource reports every bad key", block: "resources",
			body: "@bogus: 1, @nope: 2",
			want: []string{
				`resource aws.vpc.this: meta key "@bogus" is not allowed`,
				`resource aws.vpc.this: meta key "@nope" is not allowed`,
			}},
		{name: "data for-each", block: "data", body: "@for-each: ['a']"},
		{name: "data configurations", block: "data", body: "@configurations: { aws: aws.east }"},
		{name: "data lock", block: "data", body: "@lock: 'x'"},
		{name: "data rejects trigger", block: "data", body: "@trigger: 'always'",
			want: []string{`data source aws.ami.this: meta key "@trigger" is not allowed`}},
		{name: "action lock", block: "actions", body: "@lock: 'x'"},
		{name: "action trigger", block: "actions", body: "@trigger: 'always'"},
		{name: "action common keys", block: "actions",
			body: "@for-each: ['a'], @configurations: { aws: aws.east }, @depends-on: ['x']"},
		{name: "action timeout", block: "actions", body: "@timeout: '30s'"},
		{name: "resource timeout", block: "resources", body: "@timeout: '5m'"},
		{name: "data timeout", block: "data", body: "@timeout: '1h30m'"},
		{name: "timeout rejects non-string", block: "resources", body: "@timeout: 30",
			want: []string{`resource aws.vpc.this: @timeout must be a duration string like '30s'`}},
		{name: "timeout rejects bad duration", block: "actions", body: "@timeout: 'banana'",
			want: []string{`action core.command.run: @timeout "banana" is not a valid duration`}},
		{name: "action rejects unknown", block: "actions", body: "@bogus: 1",
			want: []string{`action core.command.run: meta key "@bogus" is not allowed`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var errs *ErrorList
			switch tt.block {
			case "resources":
				src := "resources: { aws.vpc.this: { " + tt.body + " } }\n"
				errs = ValidateResources(parseObjectBlock(t, src, "resources"))
			case "data":
				src := "data: { aws.ami.this: { " + tt.body + " } }\n"
				errs = ValidateDataSources(parseObjectBlock(t, src, "data"))
			case "actions":
				src := "actions: { core.command.run: { " + tt.body + " } }\n"
				errs = ValidateActions(parseObjectBlock(t, src, "actions"))
			}
			var got []string
			for _, e := range errs.Errors() {
				got = append(got, e.Msg)
			}
			if tt.want == nil {
				require.Empty(t, got)
				return
			}
			require.Equal(t, tt.want, got)
		})
	}
}

func TestValidateFileWithResourcesAndActions(t *testing.T) {
	src := `
description: 'a stack'
inputs: {
  size: { type: optional(integer, 3) }
}
resources: {
  aws.vpc.main: { cidr-block: '10.0.0.0/16' }
}
actions: {
  core.command.smoke: { @trigger: 'always', execute: 'echo' }
}
`
	f, err := ParseSource("main.ub", []byte(src))
	require.NoError(t, err)

	errs := ValidateFile(f)
	require.Equal(t, 0, errs.Len(), "got: %v", errs.Strings())
}
