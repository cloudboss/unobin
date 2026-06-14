package syntax

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/parse"
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
	f := parseFile(t, "main.ub", `
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
  std.default: {
    region: 'us-east-1'
  }
}

resources: {
  std.fs-file.hello: {
    path: local.path
    content: var.message
  }
}

data: {
  std.file.existing: {
    path: local.path
  }
}

actions: {
  std.exec.run: {
    command: 'echo hello'
  }
}

outputs: {
  path: { value: resource.std.fs-file.hello.path }
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
	assert.Equal(t, "default", got.Factory.Body.Configurations[0].Name.Name)
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

func TestLowerManifestFile(t *testing.T) {
	f := parseFile(t, "unobin.manifest", `
unobin-version: '0.2.0'
requires: {
  'github.com/cloudboss/example': 'v1.2.3'
}
replace: {
  'github.com/cloudboss/example': '../example'
}
`, parse.FileManifest)

	got, errs := LowerFile(f)
	require.Equal(t, 0, errs.Len(), errs.Error())
	require.Equal(t, FileManifest, got.Kind)
	require.NotNil(t, got.Manifest)
	requireSpan(t, got.Manifest.S)
	require.NotNil(t, got.Manifest.UnobinVersion)
	assert.Equal(t, "0.2.0", got.Manifest.UnobinVersion.Value)

	require.Len(t, got.Manifest.Requires, 1)
	requireSpan(t, got.Manifest.Requires[0].S)
	assert.Equal(t, "github.com/cloudboss/example", got.Manifest.Requires[0].ID.Value)
	assert.Equal(t, "v1.2.3", got.Manifest.Requires[0].Version.Value)

	require.Len(t, got.Manifest.Replace, 1)
	requireSpan(t, got.Manifest.Replace[0].S)
	assert.Equal(t, "github.com/cloudboss/example", got.Manifest.Replace[0].ID.Value)
	assert.Equal(t, "../example", got.Manifest.Replace[0].Path.Value)
}

func TestLowerExportedTypeFile(t *testing.T) {
	f := parseFile(t, "resource-greeting.ub", `
inputs: {
  message: { type: string }
}

outputs: {
  message: { value: var.message }
}
`, parse.FileExportedType)

	got, errs := LowerFile(f)
	require.Equal(t, 0, errs.Len(), errs.Error())
	require.Equal(t, FileLibrary, got.Kind)
	require.NotNil(t, got.Library)
	requireSpan(t, got.Library.S)
	require.Len(t, got.Library.Exports, 1)
	requireSpan(t, got.Library.Exports[0].S)
	requireSpan(t, got.Library.Exports[0].Body.S)
	assert.Equal(t, "greeting", got.Library.Exports[0].Name.Name)
	assert.Equal(t, NodeResource, got.Library.Exports[0].Kind)
	require.Len(t, got.Library.Exports[0].Body.Inputs, 1)
	require.Len(t, got.Library.Exports[0].Body.Outputs, 1)
}

func TestLowerReportsSchemaErrors(t *testing.T) {
	f := parseFile(t, "main.ub", `
inputs: {
  bad: { type: list(unknown) }
}

resources: {
  std.file: {}
}
`, parse.FileFactory)

	_, errs := LowerFile(f)
	require.NotEqual(t, 0, errs.Len())
	assert.Contains(t, errs.Error(), "unknown atomic type")
	assert.Contains(t, errs.Error(), "resource key std.file must have three segments")
}

func TestLowerReportsUserFacingFileRoleError(t *testing.T) {
	f := parseFile(t, "unknown.ub", "description: 'minimal'\n", parse.FileUnknown)

	_, errs := LowerFile(f)
	require.Equal(t, 1, errs.Len())
	got := errs.Error()
	assert.Contains(t, got, "cannot determine UB file role")
	assert.NotContains(t, got, "lower")
}
