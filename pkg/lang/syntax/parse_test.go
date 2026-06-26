package syntax

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang"
)

func readParseFixture(t *testing.T, path string) []byte {
	t.Helper()
	src, err := os.ReadFile(path)
	require.NoError(t, err)
	return src
}

func TestParseSourceLowersFile(t *testing.T) {
	got, err := ParseSource("factory.ub",
		readParseFixture(t, "testdata/ub/parse-source/valid/factory.ub"))
	require.NoError(t, err)
	require.Equal(t, FileFactory, got.Kind)
	require.NotNil(t, got.Factory)
	require.NotNil(t, got.Factory.Body.Description)
	assert.Equal(t, "Example.", got.Factory.Body.Description.Value)
}

func TestParseSourceReturnsLoweringDiagnostics(t *testing.T) {
	got, err := ParseSource("factory.ub",
		readParseFixture(t, "testdata/ub/parse-source/invalid/stack.ub"))

	require.Error(t, err)
	require.NotNil(t, got)
	assert.Contains(t, err.Error(), "factory.ub must declare factory")
}

func TestParseSourceReturnsParseError(t *testing.T) {
	got, err := ParseSource("factory.ub",
		readParseFixture(t, "testdata/ub/parse-source/invalid/open-factory.ub"))

	require.Error(t, err)
	require.Nil(t, got)
}

func TestLowerParsedSourceMatchesParseSource(t *testing.T) {
	src := readParseFixture(t, "testdata/ub/parse-source/valid/lower-parsed-source.ub")
	raw, err := lang.ParseSource("factory.ub", src)
	require.NoError(t, err)

	got, err := LowerParsedSource("factory.ub", src, raw)
	require.NoError(t, err)
	want, err := ParseSource("factory.ub", src)
	require.NoError(t, err)

	require.Equal(t, want.Kind, got.Kind)
	require.NotNil(t, got.Factory)
	require.NotNil(t, want.Factory)
	assert.Equal(t, inputSummaries(t, want), inputSummaries(t, got))
	assert.Equal(t, importSummaries(want), importSummaries(got))
	assert.Equal(t, nodeSummaries(want), nodeSummaries(got))
}

func TestLowerParsedSourceKeepsTypeDiagnostics(t *testing.T) {
	src := readParseFixture(t, "testdata/ub/lower/invalid/parse-source-type-parser-error.ub")
	raw, err := lang.ParseSource("factory.ub", src)
	require.NoError(t, err)

	_, gotErr := LowerParsedSource("factory.ub", src, raw)
	_, wantErr := ParseSource("factory.ub", src)

	require.Error(t, gotErr)
	require.Error(t, wantErr)
	assert.Equal(t, wantErr.Error(), gotErr.Error())
}

func inputSummaries(t *testing.T, f *File) []string {
	t.Helper()
	inputs := f.Factory.Body.Inputs
	out := make([]string, 0, len(inputs))
	for _, input := range inputs {
		out = append(out, input.Name.Name+"="+formatTypeExpr(t, input.Type))
	}
	return out
}

func formatTypeExpr(t *testing.T, typ lang.TypeExpr) string {
	t.Helper()
	if typ == nil {
		return ""
	}
	out, err := lang.Format(&lang.File{Body: &lang.ObjectLit{Fields: []*lang.Field{{
		Key:   lang.FieldKey{Kind: lang.FieldIdent, Name: "type"},
		Value: typ,
	}}}})
	require.NoError(t, err)
	return strings.TrimSpace(strings.TrimPrefix(string(out), "type: "))
}

func importSummaries(f *File) []string {
	imports := f.Factory.Body.Imports
	out := make([]string, 0, len(imports))
	for _, imp := range imports {
		out = append(out, imp.Alias.Name+"="+imp.Ref.Value)
	}
	return out
}

func nodeSummaries(f *File) []string {
	body := f.Factory.Body
	out := make([]string, 0, len(body.Resources)+len(body.Data)+len(body.Actions))
	out = appendNodeSummaries(out, body.Resources)
	out = appendNodeSummaries(out, body.Data)
	out = appendNodeSummaries(out, body.Actions)
	return out
}

func appendNodeSummaries(out []string, nodes []NodeDecl) []string {
	for _, node := range nodes {
		out = append(out,
			string(node.Kind)+":"+node.Name.Name+"="+
				node.Selector.Alias.Name+"."+node.Selector.Export.Name)
	}
	return out
}
