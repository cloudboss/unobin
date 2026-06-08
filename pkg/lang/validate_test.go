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

// topLevelDriver checks a fixture's top-level keys as the given file kind.
func topLevelDriver(kind FileKind) ubtest.Driver {
	return func(name string, src []byte) (string, []string) {
		f, err := ParseSource("", src)
		if err != nil {
			return "", []string{err.Error()}
		}
		f.Kind = kind
		return "", ValidateTopLevelKeys(f).Messages()
	}
}

func TestValidateTopLevelKeysFactoryFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/toplevel/factory", topLevelDriver(FileFactory))
}

func TestValidateTopLevelKeysConfigFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/toplevel/config", topLevelDriver(FileConfig))
}

func TestValidateTopLevelKeysUnknownFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/toplevel/unknown", topLevelDriver(FileUnknown))
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

// fileDriver runs ValidateFile over a whole fixture parsed as the given kind,
// reporting positioned diagnostics so the goldens pin file:line:col.
func fileDriver(kind FileKind) ubtest.Driver {
	return func(name string, src []byte) (string, []string) {
		f, err := ParseSource("main.ub", src)
		if err != nil {
			return "", []string{err.Error()}
		}
		f.Kind = kind
		return "", ValidateFile(f).Strings()
	}
}

func TestValidateFileFactoryFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/file/factory", fileDriver(FileFactory))
}

func TestValidateFileExportedTypeFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/file/exported-type", fileDriver(FileExportedType))
}

func TestValidateFileUnknownFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/file/unknown", fileDriver(FileUnknown))
}

func TestValidateCallsFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/calls", func(name string, src []byte) (string, []string) {
		f, err := ParseSource("main.ub", src)
		if err != nil {
			return "", []string{err.Error()}
		}
		return "", ValidateCalls(f).Strings()
	})
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
