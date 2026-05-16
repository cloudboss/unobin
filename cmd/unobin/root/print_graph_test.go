package root

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeStack(t *testing.T, dir, body string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	stackPath := filepath.Join(dir, "stack.ub")
	require.NoError(t, os.WriteFile(stackPath, []byte(body), 0o644))
	return stackPath
}

func TestPrintGraphPlain(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "graph-plain")
	stackPath := writeStack(t, dir, `
inputs: { msg: { type: string } }
imports: {
  core: 'github.com/cloudboss/unobin//pkg/modules/core@v0.1.0'
}
actions: {
  core: {
    command: {
      first:  { argv: ['echo', var.msg] }
      second: { argv: ['echo', action.core.command.first.stdout] }
    }
  }
}
`)

	out, err := runCommand(t, "print-graph", "-p", stackPath)
	require.NoError(t, err)
	want := `action.core.command.first
  -> var.msg

action.core.command.second
  -> action.core.command.first
`
	require.Equal(t, want, out)
}

func TestPrintGraphDOT(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "graph-dot")
	stackPath := writeStack(t, dir, `
inputs: { msg: { type: string } }
imports: {
  core: 'github.com/cloudboss/unobin//pkg/modules/core@v0.1.0'
}
actions: {
  core: {
    command: {
      first:  { argv: ['echo', var.msg] }
      second: { argv: ['echo', action.core.command.first.stdout] }
    }
  }
}
`)

	out, err := runCommand(t, "print-graph", "-p", stackPath, "--format", "dot")
	require.NoError(t, err)
	want := `digraph "graph-dot" {
  "action.core.command.first";
  "action.core.command.second";
  "action.core.command.second" -> "action.core.command.first";
}
`
	require.Equal(t, want, out)
}

func TestPrintGraphRequiresPath(t *testing.T) {
	_, err := runCommand(t, "print-graph")
	require.Error(t, err)
	require.Contains(t, err.Error(), `"path"`)
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
  bad: resource.local.file.missing.path
}
`)
	_, err := runCommand(t, "print-graph", "-p", stackPath)
	require.Error(t, err)
	require.Contains(t, err.Error(), `unknown resource "resource.local.file.missing"`)
}

func TestPrintGraphExpandsLocalUBModuleComposite(t *testing.T) {
	root := t.TempDir()

	greeterDir := filepath.Join(root, "greeter")
	require.NoError(t, os.MkdirAll(greeterDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(greeterDir, "module.ub"), []byte(`
description: 'Local greeter'

exports: {
  greeting: 'greeting.ub'
}
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(greeterDir, "greeting.ub"), []byte(`
description: 'Greeting composite'

inputs: {
  message: { type: string }
}

imports: {
  local: 'github.com/cloudboss/unobin//pkg/modules/local@v0.1.0'
}

resources: {
  local: {
    file: {
      this: {
        path:    '/tmp/greeting'
        content: var.message
      }
    }
  }
}

outputs: {
  path: resource.local.file.this.path
}
`), 0o644))

	stackDir := filepath.Join(root, "stack")
	stackPath := writeStack(t, stackDir, `
inputs: { who: { type: string } }
imports: {
  greeter: '../greeter'
}
resources: {
  greeter: {
    greeting: {
      hello: {
        message: var.who
      }
    }
  }
}
`)

	out, err := runCommand(t, "print-graph", "-p", stackPath)
	require.NoError(t, err)
	want := `resource.greeter.greeting.hello
  -> resource.greeter.greeting.hello/local.file.this

resource.greeter.greeting.hello/local.file.this
  -> var.who
`
	require.Equal(t, want, out)
}
