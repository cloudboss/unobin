package resolve

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/stretchr/testify/require"
)

func writeUB(t *testing.T, path, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
}

func parseFile(t *testing.T, path string) *lang.File {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	f, err := lang.ParseSource(path, b)
	require.NoError(t, err)
	return f
}

// setupBasicTree writes a stack importing one local UB module that
// itself imports nothing. Returns the stack file's absolute path and
// the dir to use as the resolver root.
func setupBasicTree(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()

	stackPath := filepath.Join(root, "stack.ub")
	writeUB(t, stackPath, `
description: 'top'
imports: {
  net: './modules/net'
}
`)
	writeUB(t, filepath.Join(root, "modules", "net", "module.ub"), `
description: 'net'
exports: {
  cluster: 'cluster.ub'
}
`)
	writeUB(t, filepath.Join(root, "modules", "net", "cluster.ub"), `
description: 'cluster type'
`)
	return stackPath, root
}

func TestResolveAllSimpleLocal(t *testing.T) {
	stackPath, root := setupBasicTree(t)
	f := parseFile(t, stackPath)

	graph, errs := ResolveAll(stackPath, f, NewLocalResolver(root))
	require.Empty(t, errs)

	// Edges:
	//   stackPath -> stackPath/net   (stack imports the module)
	//   stackPath/net -> stackPath/net:cluster  (module exports cluster)
	require.Empty(t, graph.DetectCycles())

	netID := stackPath + "/net"
	clusterID := netID + ":cluster"
	require.Contains(t, graph.edges[stackPath], netID)
	require.Contains(t, graph.edges[netID], clusterID)
}

func TestResolveAllDetectsCycle(t *testing.T) {
	root := t.TempDir()
	stackPath := filepath.Join(root, "stack.ub")
	writeUB(t, stackPath, `
imports: {
  a: './modules/a'
}
`)
	writeUB(t, filepath.Join(root, "modules", "a", "module.ub"), `
exports: { x: 'x.ub' }
`)
	writeUB(t, filepath.Join(root, "modules", "a", "x.ub"), `
imports: {
  b: '../b'
}
`)
	writeUB(t, filepath.Join(root, "modules", "b", "module.ub"), `
exports: { y: 'y.ub' }
`)
	writeUB(t, filepath.Join(root, "modules", "b", "y.ub"), `
imports: {
  loop: '../a'
}
`)
	f := parseFile(t, stackPath)
	graph, errs := ResolveAll(stackPath, f, NewLocalResolver(root))
	// b/y.ub tries to import ../a which escapes b's source root - it
	// should error rather than create a real cycle. The cycle is only
	// possible if the language allowed climbing out of a UB module's
	// fs.FS, which it doesn't.
	require.NotEmpty(t, errs)
	foundEscape := false
	for _, e := range errs {
		if strings.Contains(e.Error(), "must stay inside the module") {
			foundEscape = true
		}
	}
	require.True(t, foundEscape, "got: %v", errs)
	_ = graph
}

func TestResolveAllRecordsRemoteAsLeaf(t *testing.T) {
	root := t.TempDir()
	stackPath := filepath.Join(root, "stack.ub")
	writeUB(t, stackPath, `
imports: {
  aws: 'github.com/x/y@v1.0.0'
}
`)
	f := parseFile(t, stackPath)
	graph, errs := ResolveAll(stackPath, f, stubResolver{err: errors.New("stub")})
	require.NotEmpty(t, errs)
	require.NotContains(t, graph.edges[stackPath], "github.com/x/y@v1.0.0")
}

func TestResolveAllFlagsVersionConflict(t *testing.T) {
	root := t.TempDir()
	stackPath := filepath.Join(root, "stack.ub")
	writeUB(t, stackPath, `
imports: {
  a: 'github.com/x/y//a@v1.0.0'
  b: 'github.com/x/y//b@v1.1.0'
}
`)
	f := parseFile(t, stackPath)
	_, errs := ResolveAll(stackPath, f, stubResolver{err: errors.New("stub")})
	conflict := false
	for _, e := range errs {
		if strings.Contains(e.Error(), "same repo") {
			conflict = true
		}
	}
	require.True(t, conflict, "got: %v", errs)
}

func TestResolveAllNoImports(t *testing.T) {
	root := t.TempDir()
	stackPath := filepath.Join(root, "stack.ub")
	writeUB(t, stackPath, `description: 'just text'`)
	f := parseFile(t, stackPath)

	graph, errs := ResolveAll(stackPath, f, NewLocalResolver(root))
	require.Empty(t, errs)
	require.Contains(t, graph.edges, stackPath)
	require.Empty(t, graph.edges[stackPath])
}
