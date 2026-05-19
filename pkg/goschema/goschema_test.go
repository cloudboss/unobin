package goschema

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadSamePackage(t *testing.T) {
	schema, err := Read("testdata/samepkg")
	require.NoError(t, err)
	require.NotNil(t, schema)

	require.Contains(t, schema.Actions, "do")
	do := schema.Actions["do"]
	require.Equal(t, map[string]string{
		"result":   "string",
		"duration": "time.Duration",
		"tags":     "[]string",
	}, do.Outputs)

	require.Contains(t, schema.Actions, "do2")
	require.Equal(t, do.Outputs, schema.Actions["do2"].Outputs,
		"the alias should resolve to the same field set")
}

func TestReadSubpackage(t *testing.T) {
	schema, err := Read("testdata/subpkg")
	require.NoError(t, err)

	require.Contains(t, schema.Resources, "thing")
	thing := schema.Resources["thing"]
	require.Equal(t, map[string]string{
		"id":         "string",
		"cidr-block": "string",
		"replicas":   "*int64",
	}, thing.Outputs)

	require.Contains(t, schema.DataSources, "ami")
	ami := schema.DataSources["ami"]
	require.Equal(t, map[string]string{
		"architecture": "string",
	}, ami.Outputs)
}

func TestReadErrorsWhenNoModuleFunc(t *testing.T) {
	dir := t.TempDir()
	src := []byte("package empty\n\nfunc Other() int { return 0 }\n")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "empty.go"), src, 0o644))

	_, err := Read(dir)
	require.Error(t, err)
}
