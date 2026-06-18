package root

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/stretchr/testify/require"
)

func writeStack(t *testing.T, dir, body string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "factory.ub")
	require.NoError(t, os.WriteFile(stackPath, factorySource(body), 0o644))
	return stackPath
}

func TestPrintGraphPlain(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "graph-plain")
	stackPath := writeStack(t, dir, `
inputs:  { msg: { type: string } }
imports: { core: 'github.com/cloudboss/unobin//pkg/libraries/core' }
actions: {
  first:  core.command { argv: ['echo', var.msg] }
  second: core.command { argv: ['echo', action.first.stdout] }
}
`)
	writeCompileLock(t, dir, map[string]string{
		"github.com/cloudboss/unobin//pkg/libraries/core": "v0.1.0",
	})

	out, err := runCommand(t, "print-graph", "-p", stackPath)
	require.NoError(t, err)
	want := `action.first
  -> var.msg

action.second
  -> action.first
`
	require.Equal(t, want, out)
}

func TestPrintGraphSourceDeclaredFactory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "graph-source-declared")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "factory.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte(`
factory: {
  inputs:  { msg: { type: string } }
  imports: { core: 'github.com/cloudboss/unobin//pkg/libraries/core' }
  actions: {
    first:  core.command { argv: ['echo', var.msg] }
    second: core.command { argv: ['echo', action.first.stdout] }
  }
}
`), 0o644))
	writeCompileLock(t, dir, map[string]string{
		"github.com/cloudboss/unobin//pkg/libraries/core": "v0.1.0",
	})

	out, err := runCommand(t, "print-graph", "-p", stackPath)
	require.NoError(t, err)
	want := `action.first
  -> var.msg

action.second
  -> action.first
`
	require.Equal(t, want, out)
}

func TestPrintGraphDefaultPathUsesFactoryUB(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "graph-source-declared")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	t.Chdir(dir)
	require.NoError(t, os.WriteFile("factory.ub", []byte(`
factory: {
  imports: { core: 'github.com/cloudboss/unobin//pkg/libraries/core' }
  actions: { hi: core.command { argv: ['echo', 'hi'] } }
}
`), 0o644))
	writeCompileLock(t, dir, map[string]string{
		"github.com/cloudboss/unobin//pkg/libraries/core": "v0.1.0",
	})

	out, err := runCommand(t, "print-graph")
	require.NoError(t, err)
	require.Equal(t, "action.hi\n", out)
}

func TestPrintGraphDirectoryUsesFactoryUB(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "graph-source-declared")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "factory.ub"), []byte(`
factory: {
  imports: { core: 'github.com/cloudboss/unobin//pkg/libraries/core' }
  actions: { hi: core.command { argv: ['echo', 'hi'] } }
}
`), 0o644))
	writeCompileLock(t, dir, map[string]string{
		"github.com/cloudboss/unobin//pkg/libraries/core": "v0.1.0",
	})

	out, err := runCommand(t, "print-graph", "-p", dir)
	require.NoError(t, err)
	require.Equal(t, "action.hi\n", out)
}

func TestPrintGraphUsesAncestorProjectFiles(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "stacks", "demo")
	require.NoError(t, os.MkdirAll(child, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, deps.ManifestFileName),
		manifestSource("requires: { 'github.com/x/core': 'v1.0.0' }\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(child, "factory.ub"), []byte(`
factory: {
  imports: { core: 'github.com/x/core' }
  actions: { hi: core.command { argv: ['echo', 'hi'] } }
}
`), 0o644))
	writeCompileLock(t, root, map[string]string{"github.com/x/core": "v1.0.0"})

	out, err := runCommand(t, "print-graph", "-p", child)
	require.NoError(t, err)
	require.Equal(t, "action.hi\n", out)
}

func TestPrintGraphResolvesLocalImportsFromFactoryDir(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "stacks", "demo")
	require.NoError(t, os.MkdirAll(filepath.Join(child, "lib"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, deps.ManifestFileName),
		manifestSource("requires: {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(child, "factory.ub"), []byte(`
factory: {
  imports: { local: './lib' }
  data: { message: local.message {} }
  outputs: { text: { value: data.message.text } }
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(child, "lib", "library.ub"), []byte(`
message: data {
  outputs: { text: { value: 'hi' } }
}
`), 0o644))

	out, err := runCommand(t, "print-graph", "-p", child)
	require.NoError(t, err)
	require.Equal(t, "data.message\n\noutput.text\n  -> data.message\n", out)
}

func TestPrintGraphUsesManifestReplace(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "demo-lib")
	require.NoError(t, os.MkdirAll(repo, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repo, deps.ManifestFileName),
		[]byte("manifest: { requires: {} }\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "noop.ub"), []byte(`
noop: action {
  description: 'No-op action composite.'
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, deps.ManifestFileName),
		manifestSource("requires: {}\nreplace: { 'github.com/x/demo': './demo-lib' }\n"),
		0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "factory.ub"), []byte(`
factory: {
  imports: { demo: 'github.com/x/demo' }
  actions: { hi: demo.noop {} }
}
`), 0o644))

	out, err := runCommand(t, "print-graph", "-p", root)
	require.NoError(t, err)
	require.Equal(t, "action.hi\n", out)
}

func TestPrintGraphAllowsSelfImport(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "graph-self")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "factory.ub"), []byte(`
factory: {
  imports: { self: '.' }
  data: { message: self.message {} }
  outputs: { text: { value: data.message.text } }
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "library.ub"), []byte(`
message: data {
  outputs: { text: { value: 'hi' } }
}
`), 0o644))

	out, err := runCommand(t, "print-graph", "-p", dir)
	require.NoError(t, err)
	require.Equal(t, "data.message\n\noutput.text\n  -> data.message\n", out)
}

func TestPrintGraphDOT(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "graph-dot")
	stackPath := writeStack(t, dir, `
inputs:  { msg: { type: string } }
imports: { core: 'github.com/cloudboss/unobin//pkg/libraries/core' }
actions: {
  first:  core.command { argv: ['echo', var.msg] }
  second: core.command { argv: ['echo', action.first.stdout] }
}
`)
	writeCompileLock(t, dir, map[string]string{
		"github.com/cloudboss/unobin//pkg/libraries/core": "v0.1.0",
	})

	out, err := runCommand(t, "print-graph", "-p", stackPath, "--format", "dot")
	require.NoError(t, err)
	want := `digraph "graph-dot" {
  "action.first";
  "action.second";
  "action.second" -> "action.first";
}
`
	require.Equal(t, want, out)
}

func TestPrintGraphRejectsUnknownFormat(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "graph-bad-format")
	stackPath := writeStack(t, dir, `
description: 'x'
`)
	_, err := runCommand(t, "print-graph", "-p", stackPath, "--format", "yaml")
	require.Error(t, err)
	require.Contains(t, err.Error(), "--format")
}

func TestPrintGraphInvalidReferenceFails(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "graph-invalid-ref")
	stackPath := writeStack(t, dir, `
outputs: {
  bad: { value: resource.missing.path }
}
`)
	_, err := runCommand(t, "print-graph", "-p", stackPath)
	require.Error(t, err)
	require.Contains(t, err.Error(), `unknown resource "resource.missing"`)
}

func TestPrintGraphExpandsLocalUBLibraryComposite(t *testing.T) {
	root := t.TempDir()

	greeterDir := filepath.Join(root, "greeter")
	require.NoError(t, os.MkdirAll(greeterDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(greeterDir, "library.ub"), []byte(`
greeting: resource {
  description: 'Greeting composite'
  inputs: { message: { type: string } }
  imports: { local: 'github.com/cloudboss/unobin//pkg/libraries/local' }
  resources: { this: local.file { path: '/tmp/greeting', content: var.message } }
  outputs: { path: { value: resource.this.path } }
}
`), 0o644))

	stackDir := filepath.Join(root, "stack")
	stackPath := writeStack(t, stackDir, `
inputs:  { who: { type: string } }
imports: { greeter: '../greeter' }
resources: { hello: greeter.greeting { message: var.who } }
`)
	writeCompileLock(t, stackDir, map[string]string{
		"github.com/cloudboss/unobin//pkg/libraries/local": "v0.1.0",
	})

	out, err := runCommand(t, "print-graph", "-p", stackPath)
	require.NoError(t, err)
	want := `resource.hello
  -> resource.hello/resource.this

resource.hello/resource.this
  -> var.who
`
	require.Equal(t, want, out)
}
