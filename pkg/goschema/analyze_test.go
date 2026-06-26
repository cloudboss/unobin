package goschema

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAnalyzeReturnsSchemaAndSourceIndex(t *testing.T) {
	analysis, err := Analyze("testdata/definition")
	require.NoError(t, err)
	require.NotNil(t, analysis)
	require.Empty(t, analysis.Warnings)
	require.NotNil(t, analysis.Schema)
	require.NotNil(t, analysis.Index)

	require.Contains(t, analysis.Schema.Resources, "server")
	require.Contains(t, analysis.Schema.DataSources, "lookup")
	require.Contains(t, analysis.Schema.Actions, "deploy")
	require.Contains(t, analysis.Schema.Functions, "slug")
	require.True(t, analysis.Schema.HasConfiguration)

	requireLocationUnder(t, analysis.Index.LibraryFunc, "testdata/definition")
	requireLocationUnder(t, analysis.Index.Registrations["resource"]["server"],
		"testdata/definition")
	requireLocationUnder(t, analysis.Index.InputTypes["resource"]["server"],
		"testdata/definition")
	requireLocationUnder(t, analysis.Index.OutputFields["resource"]["server"]["endpoint.url"],
		"testdata/definition")
	requireLocationUnder(t, analysis.Index.ConfigFields["retry.count"], "testdata/definition")
	requireLocationUnder(t, analysis.Index.Functions["slug"], "testdata/definition")
}

func TestAnalyzePreservesWarnings(t *testing.T) {
	analysis, err := Analyze("testdata/nested")
	require.NoError(t, err)
	require.NotNil(t, analysis.Schema)
	require.Contains(t, analysis.Schema.Resources, "db")
	require.Equal(t, []string{nestedSelfWarning}, analysis.Warnings)
}

func TestReadAndReadWithIndexUseAnalyze(t *testing.T) {
	analysis, err := Analyze("testdata/definition")
	require.NoError(t, err)

	schema, warnings, err := Read("testdata/definition")
	require.NoError(t, err)
	require.Equal(t, analysis.Schema, schema)
	require.Equal(t, analysis.Warnings, warnings)

	indexedSchema, index, indexedWarnings, err := ReadWithIndex("testdata/definition")
	require.NoError(t, err)
	require.Equal(t, analysis.Schema, indexedSchema)
	require.Equal(t, analysis.Index, index)
	require.Equal(t, analysis.Warnings, indexedWarnings)
}

func requireLocationUnder(t *testing.T, loc GoLocation, root string) {
	t.Helper()
	require.NotEmpty(t, loc.Path)
	rel, err := filepath.Rel(root, loc.Path)
	require.NoError(t, err)
	require.NotContains(t, rel, "..")
	require.Greater(t, loc.Line, 0)
	require.Greater(t, loc.Column, 0)
	require.GreaterOrEqual(t, loc.Offset, 0)
}
