package syntax

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/parse"
	"github.com/cloudboss/unobin/pkg/ubtest"
)

func parseFile(t *testing.T, path, src string, kind parse.FileKind) *parse.File {
	t.Helper()
	f, err := lang.ParseSource(path, []byte(src))
	require.NoError(t, err)
	f.Kind = kind
	return f
}

func requireSpan(t *testing.T, span parse.Span) {
	t.Helper()
	require.False(t, span.Start.IsZero())
}

func TestLowerFactoryFile(t *testing.T) {
	f := parseFile(t, "factory.ub", `
factory: {
description: 'Example.'

imports: { std: 'github.com/cloudboss/unobin-library-std' }

inputs: {
  message: { type: string }
}

locals: {
  path: '/tmp/hello.txt'
}

constraints: [
  { when: var.message require: var.message != '' }
]

configurations: {
  std {
    region: 'us-east-1'
  }
}

resources: {
  hello: std.fs-file {
    path: local.path
    content: var.message
  }
}

data: {
  existing: std.file {
    path: local.path
  }
}

actions: {
  run: std.exec {
    command: 'echo hello'
  }
}

outputs: {
  path: { value: resource.hello.path }
}
}
`, parse.FileFactory)

	got, errs := LowerFile(f)
	require.Equal(t, 0, errs.Len(), errs.Error())
	requireSpan(t, got.S)
	require.Equal(t, FileFactory, got.Kind)
	require.NotNil(t, got.Factory)
	requireSpan(t, got.Factory.S)
	requireSpan(t, got.Factory.Body.S)
	assert.Equal(t, "Example.", got.Factory.Body.Description.Value)

	require.Len(t, got.Factory.Body.Imports, 1)
	requireSpan(t, got.Factory.Body.Imports[0].S)
	assert.Equal(t, "std", got.Factory.Body.Imports[0].Alias.Name)
	assert.Equal(t, "github.com/cloudboss/unobin-library-std", got.Factory.Body.Imports[0].Ref.Value)

	require.Len(t, got.Factory.Body.Inputs, 1)
	requireSpan(t, got.Factory.Body.Inputs[0].S)
	assert.Equal(t, "message", got.Factory.Body.Inputs[0].Name.Name)
	require.IsType(t, &parse.TypeAtomic{}, got.Factory.Body.Inputs[0].Type)
	assert.Equal(t, "string", got.Factory.Body.Inputs[0].Type.(*parse.TypeAtomic).Name)

	require.Len(t, got.Factory.Body.Locals, 1)
	requireSpan(t, got.Factory.Body.Locals[0].S)
	assert.Equal(t, "path", got.Factory.Body.Locals[0].Name.Name)

	require.Len(t, got.Factory.Body.Constraints, 1)
	requireSpan(t, got.Factory.Body.Constraints[0].S)
	require.Len(t, got.Factory.Body.Configurations, 1)
	requireSpan(t, got.Factory.Body.Configurations[0].S)
	require.Nil(t, got.Factory.Body.Configurations[0].Name)
	assert.Equal(t, "std", got.Factory.Body.Configurations[0].Selector.Name)

	require.Len(t, got.Factory.Body.Resources, 1)
	resource := got.Factory.Body.Resources[0]
	requireSpan(t, resource.S)
	requireSpan(t, resource.Selector.S)
	assert.Equal(t, NodeResource, resource.Kind)
	assert.Equal(t, "hello", resource.Name.Name)
	assert.Equal(t, "std", resource.Selector.Alias.Name)
	assert.Equal(t, "fs-file", resource.Selector.Export.Name)

	require.Len(t, got.Factory.Body.Data, 1)
	requireSpan(t, got.Factory.Body.Data[0].S)
	assert.Equal(t, NodeData, got.Factory.Body.Data[0].Kind)
	assert.Equal(t, "existing", got.Factory.Body.Data[0].Name.Name)

	require.Len(t, got.Factory.Body.Actions, 1)
	requireSpan(t, got.Factory.Body.Actions[0].S)
	assert.Equal(t, NodeAction, got.Factory.Body.Actions[0].Kind)
	assert.Equal(t, "run", got.Factory.Body.Actions[0].Name.Name)

	require.Len(t, got.Factory.Body.Outputs, 1)
	requireSpan(t, got.Factory.Body.Outputs[0].S)
	assert.Equal(t, "path", got.Factory.Body.Outputs[0].Name.Name)
}

func TestLowerInputTypeFieldsUseParsedTypes(t *testing.T) {
	f := parseFile(t, "factory.ub", `
factory: {
  inputs: {
    cfg: { type: object({ port: { type: integer, default: 8080 } }) }
  }
}
`, parse.FileFactory)

	got, errs := LowerFile(f)
	require.Equal(t, 0, errs.Len(), errs.Error())
	require.Len(t, got.Factory.Body.Inputs, 1)

	input := got.Factory.Body.Inputs[0]
	typeField := input.Body.Fields[0]
	require.Equal(t, "type", typeField.Key.Name)
	require.Same(t, input.Type, typeField.Value)

	obj, ok := input.Type.(*parse.TypeObject)
	require.True(t, ok, "got %T", input.Type)
	require.Len(t, obj.Fields, 1)
	nested := obj.Fields[0]
	require.NotNil(t, nested.Decl)
	nestedTypeField := nested.Decl.Fields[0]
	require.Equal(t, "type", nestedTypeField.Key.Name)
	require.IsType(t, &parse.TypeAtomic{}, nestedTypeField.Value)
}

func TestLowerStackFile(t *testing.T) {
	f := parseFile(t, "dev.ub", `
locals: {
  bucket: 'example'
}

factory: {
  pin: {
    library-path: './factory'
  }
  inputs: {
    message: 'hello'
  }
  configurations: {
    std.default: {
      region: local.bucket
    }
  }
}

state: {
  @backend: local
  path: '.unobin/state'
}

encryption: {
  @key-source: noop
}

parallelism: 4
`, parse.FileConfig)

	got, errs := LowerFile(f)
	require.Equal(t, 0, errs.Len(), errs.Error())
	require.Equal(t, FileStack, got.Kind)
	require.NotNil(t, got.Stack)
	requireSpan(t, got.Stack.S)

	require.Len(t, got.Stack.Locals, 1)
	requireSpan(t, got.Stack.Locals[0].S)
	assert.Equal(t, "bucket", got.Stack.Locals[0].Name.Name)
	require.NotNil(t, got.Stack.Factory)
	requireSpan(t, got.Stack.Factory.S)
	require.NotNil(t, got.Stack.Factory.Pin)
	require.NotNil(t, got.Stack.Factory.Inputs)
	require.Len(t, got.Stack.Factory.Configurations, 1)
	requireSpan(t, got.Stack.Factory.Configurations[0].S)
	assert.Equal(t, "default", got.Stack.Factory.Configurations[0].Name.Name)
	assert.Equal(t, "std", got.Stack.Factory.Configurations[0].Selector.Name)

	require.NotNil(t, got.Stack.State)
	requireSpan(t, got.Stack.State.S)
	assert.Equal(t, "local", got.Stack.State.Selector.Name)
	require.Len(t, got.Stack.State.Body.Fields, 1)
	assert.Equal(t, "path", got.Stack.State.Body.Fields[0].Key.Name)

	require.NotNil(t, got.Stack.Encryption)
	requireSpan(t, got.Stack.Encryption.S)
	assert.Equal(t, "noop", got.Stack.Encryption.Selector.Name)
	require.Empty(t, got.Stack.Encryption.Body.Fields)
	require.NotNil(t, got.Stack.Parallelism)
}

func TestLowerSourceDeclaredFactoryFile(t *testing.T) {
	f := parseFile(t, "factory.ub", `
factory: {
  description: 'Example.'
  resources: {
    hello: std.fs-file {
      path: '/tmp/hello.txt'
    }
  }
}
`, parse.FileUnknown)

	got, errs := LowerFile(f)
	require.Equal(t, 0, errs.Len(), errs.Error())
	require.Equal(t, FileFactory, got.Kind)
	require.NotNil(t, got.Factory)
	assert.Equal(t, "Example.", got.Factory.Body.Description.Value)
	require.Len(t, got.Factory.Body.Resources, 1)
	assert.Equal(t, "hello", got.Factory.Body.Resources[0].Name.Name)
	assert.Equal(t, "std", got.Factory.Body.Resources[0].Selector.Alias.Name)
	assert.Equal(t, "fs-file", got.Factory.Body.Resources[0].Selector.Export.Name)
}

func TestLowerSourceDeclaredStackFile(t *testing.T) {
	f := parseFile(t, "dev.ub", `
stack: {
  factory: {
    inputs: {
      message: 'hello'
    }
  }

  state: local {
    path: '.unobin/state'
  }

  encryption: noop {}
}
`, parse.FileUnknown)

	got, errs := LowerFile(f)
	require.Equal(t, 0, errs.Len(), errs.Error())
	require.Equal(t, FileStack, got.Kind)
	require.NotNil(t, got.Stack)
	require.NotNil(t, got.Stack.Factory)
	require.NotNil(t, got.Stack.Factory.Inputs)
	require.NotNil(t, got.Stack.State)
	assert.Equal(t, "local", got.Stack.State.Selector.Name)
	require.NotNil(t, got.Stack.Encryption)
	assert.Equal(t, "noop", got.Stack.Encryption.Selector.Name)
}

func TestLowerSourceDeclaredManifestFile(t *testing.T) {
	f := parseFile(t, "manifest.ub", `
manifest: {
  unobin-version: '0.2.0'
  requires: {
    'github.com/cloudboss/example': 'v1.2.3'
  }
}
`, parse.FileUnknown)

	got, errs := LowerFile(f)
	require.Equal(t, 0, errs.Len(), errs.Error())
	require.Equal(t, FileManifest, got.Kind)
	require.NotNil(t, got.Manifest)
	require.NotNil(t, got.Manifest.UnobinVersion)
	assert.Equal(t, "0.2.0", got.Manifest.UnobinVersion.Value)
	require.Len(t, got.Manifest.Requires, 1)
	assert.Equal(t, "github.com/cloudboss/example", got.Manifest.Requires[0].ID.Value)
}

func TestLowerSourceDeclaredLockFile(t *testing.T) {
	f := parseFile(t, "lock.ub", `
lock: {
  version: 1
  toolchain: {
    unobin-version: 'v0.4.2'
  }
  deps: {
    'github.com/cloudboss/unobin-library-std': {
      kind: go
      version: 'v0.1.0'
      commit: 'abc123'
    }
    'github.com/acme/ub-lib//network': {
      kind: ub
      version: 'v0.4.2'
      commit: 'def456'
      hash: 'sha256:789abc'
    }
  }
}
`, parse.FileUnknown)

	got, errs := LowerFile(f)
	require.Equal(t, 0, errs.Len(), errs.Error())
	require.Equal(t, FileLock, got.Kind)
	require.NotNil(t, got.Lock)
	requireSpan(t, got.Lock.S)
	require.NotNil(t, got.Lock.Version)
	assert.Equal(t, int64(1), got.Lock.Version.ParsedInt)
	require.NotNil(t, got.Lock.Toolchain)
	assert.Equal(t, "v0.4.2", got.Lock.Toolchain.UnobinVersion.Value)

	require.Len(t, got.Lock.Deps, 2)
	assert.Equal(t, "github.com/cloudboss/unobin-library-std", got.Lock.Deps[0].ID.Value)
	assert.Equal(t, "go", got.Lock.Deps[0].Kind.Name)
	assert.Equal(t, "v0.1.0", got.Lock.Deps[0].Version.Value)
	assert.Equal(t, "abc123", got.Lock.Deps[0].Commit.Value)
	require.Nil(t, got.Lock.Deps[0].Hash)
	assert.Equal(t, "github.com/acme/ub-lib//network", got.Lock.Deps[1].ID.Value)
	assert.Equal(t, "ub", got.Lock.Deps[1].Kind.Name)
	assert.Equal(t, "sha256:789abc", got.Lock.Deps[1].Hash.Value)
}

func TestLowerSourceDeclaredLibraryFile(t *testing.T) {
	f := parseFile(t, "library.ub", `
greeting: resource {
  outputs: {
    message: { value: 'hello' }
  }
}

lookup: data {
  outputs: {}
}
`, parse.FileUnknown)

	got, errs := LowerFile(f)
	require.Equal(t, 0, errs.Len(), errs.Error())
	require.Equal(t, FileLibrary, got.Kind)
	require.NotNil(t, got.Library)
	require.Len(t, got.Library.Exports, 2)
	assert.Equal(t, NodeResource, got.Library.Exports[0].Kind)
	assert.Equal(t, "greeting", got.Library.Exports[0].Name.Name)
	assert.Equal(t, NodeData, got.Library.Exports[1].Kind)
	assert.Equal(t, "lookup", got.Library.Exports[1].Name.Name)
}

func TestLowerExportedTypeFileRequiresDeclarations(t *testing.T) {
	f := parseFile(t, "library.ub", `
inputs: {
  message: { type: string }
}

outputs: {
  message: { value: var.message }
}
`, parse.FileExportedType)

	_, errs := LowerFile(f)
	require.Equal(t, 1, errs.Len(), errs.Error())
	assert.Contains(t, errs.Error(), "library file must contain composite declarations")
}

func TestRuntimeFactoryBodyObjectKeepsConfigurationDeclarations(t *testing.T) {
	f := parseFile(t, "factory.ub", `
factory: {
  configurations: {
    greet {}
    formal: greet {
      prefix: configuration.formal.prefix
    }
  }

  actions: {
    say: greet.say {
      @configuration: configuration.formal
      message: configuration.formal.prefix
    }
    wrapped: greeter.greeting {
      @configurations: { greet: configuration.formal }
      message: 'wrapped'
    }
  }
}
`, parse.FileUnknown)

	got, errs := LowerFile(f)
	require.Equal(t, 0, errs.Len(), errs.Error())
	body := RuntimeFactoryBodyObject(got.Factory.Body)
	out := &parse.File{S: body.S, Kind: parse.FileFactory, Body: body}
	formatted, err := lang.Format(out)
	require.NoError(t, err)
	want := `configurations: {
  greet {}
  formal: greet {
    prefix: configuration.greet.formal.prefix
  }
}

actions: {
  say: greet.say {
    @configuration: greet.formal
    message:        configuration.greet.formal.prefix
  }
  wrapped: greeter.greeting {
    @configurations: { greet: greet.formal }
    message:         'wrapped'
  }
}
`
	assert.Equal(t, want, string(formatted))
}

func TestLowerSelectorBodyFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/valid/selector-body", func(name string, src []byte) (string, []string) {
		kind, path := selectorBodyFixtureKind(name)
		f, err := lang.ParseSource(path, src)
		if err != nil {
			return "", []string{err.Error()}
		}
		f.Kind = kind
		got, errs := LowerFile(f)
		if errs.Len() > 0 {
			return "", errs.Strings()
		}
		return lowerSelectorBodySummary(got), nil
	})
}

func TestLowerInvalidSelectorBodyFixtures(t *testing.T) {
	ubtest.Run(t, "testdata/ub/invalid/selector-body", func(
		name string, src []byte,
	) (string, []string) {
		f, err := lang.ParseSource(invalidSelectorBodyFixturePath(name), src)
		if err != nil {
			return "", []string{err.Error()}
		}
		_, errs := LowerFile(f)
		return "", errs.Messages()
	})
}

func selectorBodyFixtureKind(name string) (parse.FileKind, string) {
	switch name {
	case "factory":
		return parse.FileFactory, "factory.ub"
	case "stack":
		return parse.FileConfig, "dev.ub"
	case "library":
		return parse.FileExportedType, "library.ub"
	default:
		return parse.FileUnknown, name + ".ub"
	}
}

func invalidSelectorBodyFixturePath(name string) string {
	switch {
	case strings.HasPrefix(name, "factory-"):
		return "factory.ub"
	case strings.HasPrefix(name, "stack-"):
		return "dev.ub"
	case strings.HasPrefix(name, "library-"):
		return "library.ub"
	default:
		return name + ".ub"
	}
}

func lowerSelectorBodySummary(f *File) string {
	var b strings.Builder
	switch f.Kind {
	case FileFactory:
		writeConfigurationDecls(&b, f.Factory.Body.Configurations, "configuration")
		writeNodeDecls(&b, f.Factory.Body.Resources)
		writeNodeDecls(&b, f.Factory.Body.Data)
		writeNodeDecls(&b, f.Factory.Body.Actions)
	case FileStack:
		if f.Stack.State != nil {
			fmt.Fprintf(&b, "state -> %s fields=%d\n",
				f.Stack.State.Selector.Name, objectFieldCount(f.Stack.State.Body))
		}
		if f.Stack.Encryption != nil {
			fmt.Fprintf(&b, "encryption -> %s fields=%d\n",
				f.Stack.Encryption.Selector.Name, objectFieldCount(f.Stack.Encryption.Body))
		}
		if f.Stack.Factory != nil {
			writeConfigurationValues(&b, f.Stack.Factory.Configurations,
				"factory configuration")
		}
	case FileLibrary:
		for _, export := range f.Library.Exports {
			fmt.Fprintf(&b, "export %s %s outputs=%d\n",
				export.Kind, export.Name.Name, len(export.Body.Outputs))
		}
	}
	return b.String()
}

func writeConfigurationDecls(
	b *strings.Builder,
	configurations []ConfigurationDecl,
	prefix string,
) {
	for _, cfg := range configurations {
		fmt.Fprintf(b, "%s %s -> %s fields=%d\n",
			prefix, optionalName(cfg.Name), cfg.Selector.Name, objectFieldCount(cfg.Body))
	}
}

func writeConfigurationValues(
	b *strings.Builder,
	configurations []ConfigurationValue,
	prefix string,
) {
	for _, cfg := range configurations {
		fmt.Fprintf(b, "%s %s -> %s fields=%d\n",
			prefix, optionalName(cfg.Name), cfg.Selector.Name, objectFieldCount(cfg.Body))
	}
}

func writeNodeDecls(b *strings.Builder, nodes []NodeDecl) {
	for _, node := range nodes {
		fmt.Fprintf(b, "%s %s -> %s.%s fields=%d\n",
			node.Kind, node.Name.Name, node.Selector.Alias.Name,
			node.Selector.Export.Name, objectFieldCount(node.Body))
	}
}

func optionalName(name *Ident) string {
	if name == nil {
		return "<default>"
	}
	return name.Name
}

func objectFieldCount(obj *parse.ObjectLit) int {
	if obj == nil {
		return 0
	}
	return len(obj.Fields)
}

func TestLowerReportsSchemaErrors(t *testing.T) {
	f := parseFile(t, "factory.ub", `
factory: {
  inputs: {
    bad: { type: list(unknown) }
  }

  resources: {
    std.file: {}
  }
}
`, parse.FileFactory)

	_, errs := LowerFile(f)
	require.NotEqual(t, 0, errs.Len())
	assert.Contains(t, errs.Error(), "unknown atomic type")
	assert.Contains(t, errs.Error(), "resource must be written as name: alias.export { ... }")
}

func TestLowerRejectsUnwrappedFactoryFile(t *testing.T) {
	f := parseFile(t, "factory.ub", `
inputs: {}
resources: {}
`, parse.FileFactory)

	_, errs := LowerFile(f)
	require.NotEqual(t, 0, errs.Len())
	assert.Contains(t, errs.Error(), "factory.ub must declare factory")
}

func TestLowerReportsUserFacingFileRoleError(t *testing.T) {
	f := parseFile(t, "unknown.ub", "description: 'minimal'\n", parse.FileUnknown)

	_, errs := LowerFile(f)
	require.Equal(t, 1, errs.Len())
	got := errs.Error()
	assert.Contains(t, got, "cannot determine UB file role")
	assert.NotContains(t, got, "lower")
}

func TestLowerReportsMixedSourceDeclaredFileRoles(t *testing.T) {
	f := parseFile(t, "mixed.ub", `
factory: {}
stack: {}
`, parse.FileUnknown)

	_, errs := LowerFile(f)
	require.NotEqual(t, 0, errs.Len())
	assert.Contains(t, errs.Error(), "file must not declare both factory and stack")
}

func TestLowerReportsReservedFilenameMismatch(t *testing.T) {
	cases := []struct {
		name string
		path string
		src  string
		want string
	}{
		{
			name: "factory file without factory declaration",
			path: "factory.ub",
			src:  "greeting: resource {}\n",
			want: "factory.ub must declare factory",
		},
		{
			name: "manifest file with factory declaration",
			path: "manifest.ub",
			src:  "factory: {}\n",
			want: "manifest.ub must declare manifest",
		},
		{
			name: "lock file with manifest declaration",
			path: "lock.ub",
			src:  "manifest: {}\n",
			want: "lock.ub must declare lock",
		},
		{
			name: "factory declaration outside factory file",
			path: "app.ub",
			src:  "factory: {}\n",
			want: "factory declaration must be in factory.ub",
		},
		{
			name: "manifest declaration outside manifest file",
			path: "app.ub",
			src:  "manifest: {}\n",
			want: "manifest declaration must be in manifest.ub",
		},
		{
			name: "lock declaration outside lock file",
			path: "app.ub",
			src:  "lock: {}\n",
			want: "lock declaration must be in lock.ub",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := parseFile(t, c.path, c.src, parse.FileUnknown)

			_, errs := LowerFile(f)
			require.NotEqual(t, 0, errs.Len())
			assert.Contains(t, errs.Error(), c.want)
		})
	}
}

func TestLowerReportsLockSchemaErrors(t *testing.T) {
	f := parseFile(t, "lock.ub", `
lock: {
  version: '1'
  deps: {
    'github.com/cloudboss/example': {
      kind: ub
      version: 'v0.1.0'
      commit: 'abc123'
    }
    'github.com/cloudboss/example-go': {
      kind: go
      version: 'v0.1.0'
      commit: 'def456'
      hash: 'sha256:nope'
    }
    'github.com/cloudboss/example-bad': {
      kind: other
      commit: 'bad789'
    }
  }
}
`, parse.FileUnknown)

	_, errs := LowerFile(f)
	require.NotEqual(t, 0, errs.Len())
	got := errs.Error()
	assert.Contains(t, got, "lock version must be an integer")
	assert.Contains(t, got, "lock: missing toolchain")
	assert.Contains(t, got, "lock dependency github.com/cloudboss/example: ub kind requires hash")
	assert.Contains(t, got, "lock dependency github.com/cloudboss/example-go: go kind forbids hash")
	assert.Contains(t, got, "lock dependency github.com/cloudboss/example-bad: unknown kind")
	assert.Contains(t, got, "lock dependency github.com/cloudboss/example-bad: missing version")
}

func TestLowerReportsLockToolchainSchemaErrors(t *testing.T) {
	f := parseFile(t, "lock.ub", `
lock: {
  version: 1
  toolchain: {}
  deps: {}
}
`, parse.FileUnknown)

	_, errs := LowerFile(f)
	require.NotEqual(t, 0, errs.Len())
	assert.Contains(t, errs.Error(), "lock toolchain: missing unobin-version")
}
