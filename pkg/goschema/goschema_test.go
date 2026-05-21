package goschema

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudboss/unobin/pkg/typecheck"
	"github.com/stretchr/testify/require"
)

func TestReadSamePackage(t *testing.T) {
	schema, warnings, err := Read("testdata/samepkg")
	require.NoError(t, err)
	require.Empty(t, warnings)
	require.NotNil(t, schema)

	require.Contains(t, schema.Actions, "do")
	do := schema.Actions["do"]
	require.Equal(t, map[string]typecheck.Type{
		"what": typecheck.TString(),
	}, do.Inputs)
	require.Equal(t, map[string]typecheck.Type{
		"result":   typecheck.TString(),
		"duration": typecheck.TInteger(),
		"tags":     typecheck.TList(typecheck.TString()),
	}, do.Outputs)

	require.Contains(t, schema.Actions, "do2")
	require.Equal(t, do.Outputs, schema.Actions["do2"].Outputs,
		"the alias should resolve to the same field set")
}

func TestReadSubpackage(t *testing.T) {
	schema, warnings, err := Read("testdata/subpkg")
	require.NoError(t, err)
	require.Empty(t, warnings)

	require.Contains(t, schema.Resources, "thing")
	thing := schema.Resources["thing"]
	require.Equal(t, map[string]typecheck.Type{
		"name": typecheck.TString(),
	}, thing.Inputs)
	require.Equal(t, map[string]typecheck.Type{
		"id":         typecheck.TString(),
		"cidr-block": typecheck.TString(),
		"replicas":   typecheck.TOptional(typecheck.TInteger()),
	}, thing.Outputs)

	require.Contains(t, schema.DataSources, "ami")
	ami := schema.DataSources["ami"]
	require.Equal(t, map[string]typecheck.Type{
		"image-id": typecheck.TString(),
	}, ami.Inputs)
	require.Equal(t, map[string]typecheck.Type{
		"architecture": typecheck.TString(),
	}, ami.Outputs)
}

func TestReadDerivesKebabFromFieldNameWhenTagAbsent(t *testing.T) {
	schema, warnings, err := Read("testdata/untagged")
	require.NoError(t, err)
	require.Empty(t, warnings)

	require.Contains(t, schema.Resources, "thing")
	thing := schema.Resources["thing"]
	require.Equal(t, map[string]typecheck.Type{
		"id":           typecheck.TString(),
		"cidr-block":   typecheck.TString(),
		"https-proxy":  typecheck.TString(),
		"explicit-tag": typecheck.TString(),
	}, thing.Outputs)
}

func TestReadWarnsWhenOutputTypeMissing(t *testing.T) {
	dir := t.TempDir()
	src := []byte(`package mod

import "github.com/cloudboss/unobin/pkg/runtime"

func Module() *runtime.Module {
	return &runtime.Module{
		Name: "mod",
		Resources: map[string]runtime.ResourceRegistration{
			"thing": runtime.MakeResource[Thing, *ThingOutput](),
		},
	}
}

type Thing struct{}
`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "module.go"), src, 0o644))

	schema, warnings, err := Read(dir)
	require.NoError(t, err)
	require.NotNil(t, schema)
	require.Contains(t, schema.Resources, "thing")
	require.Nil(t, schema.Resources["thing"].Outputs)

	require.Len(t, warnings, 1)
	require.Contains(t, warnings[0], `"thing"`)
	require.Contains(t, warnings[0], "ThingOutput")
}

func TestReadExpandsNestedStructTypes(t *testing.T) {
	schema, warnings, err := Read("testdata/nested")
	require.NoError(t, err)
	require.Empty(t, warnings)

	require.Contains(t, schema.Resources, "db")
	db := schema.Resources["db"]

	endpoint := typecheck.TObject([]typecheck.ObjectField{
		{Name: "host", Type: typecheck.TString()},
		{Name: "port", Type: typecheck.TInteger()},
	})

	require.Len(t, db.Outputs, 4)
	require.True(t, db.Outputs["id"].Equal(typecheck.TString()),
		"got %s", db.Outputs["id"])
	require.True(t, db.Outputs["endpoint"].Equal(endpoint),
		"got %s", db.Outputs["endpoint"])
	require.True(t,
		db.Outputs["replicas"].Equal(typecheck.TList(endpoint)),
		"got %s", db.Outputs["replicas"])
	// Self is *DBOutput while DBOutput is being walked, so the
	// cycle guard returns Unknown for the recursive reference.
	require.True(t,
		db.Outputs["self"].Equal(typecheck.TOptional(typecheck.TUnknown())),
		"recursive self should hit the cycle guard, got %s",
		db.Outputs["self"])
}

func TestReadErrorsWhenNoModuleFunc(t *testing.T) {
	dir := t.TempDir()
	src := []byte("package empty\n\nfunc Other() int { return 0 }\n")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "empty.go"), src, 0o644))

	_, _, err := Read(dir)
	require.Error(t, err)
}
