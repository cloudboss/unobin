package codegen

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/ubtest"
)

// parsesAsGoExpr returns the Go expression parsed from src, or fails
// the test with the source for diagnosis.
func parsesAsGoExpr(t *testing.T, src string) {
	t.Helper()
	_, err := parser.ParseExpr(src)
	require.NoError(t, err, "encoder output should parse as a Go expression:\n%s", src)
}

// encodeBody parses .ub source through `lang.ParseSource`, runs
// `EncodeNode` on the result, and returns the encoded string.
func encodeBody(t *testing.T, src string) string {
	t.Helper()
	f, err := lang.ParseSource("test.ub", []byte(src))
	require.NoError(t, err)
	got, err := EncodeNode(f)
	require.NoError(t, err)
	return got
}

func encodeFixture(t *testing.T, name string) string {
	t.Helper()
	return encodeBody(t, ubtest.ReadValidFixture(t, "testdata/ub/encode", name))
}

func TestEncodeStringLit(t *testing.T) {
	got, err := EncodeNode(&lang.StringLit{Value: "hello"})
	require.NoError(t, err)
	require.Equal(t, `&lang.StringLit{Value: "hello"}`, got)
	parsesAsGoExpr(t, got)
}

func TestEncodeStringLitMultiline(t *testing.T) {
	got, err := EncodeNode(&lang.StringLit{Value: "line one\nline two", Form: lang.StringLiteralClip})
	require.NoError(t, err)
	require.Contains(t, got, `Value: "line one\nline two"`)
	require.Contains(t, got, "Form: lang.StringLiteralClip")
	parsesAsGoExpr(t, got)
}

func TestEncodeStringLitEscapes(t *testing.T) {
	got, err := EncodeNode(&lang.StringLit{Value: `quote " backslash \ tab` + "\t"})
	require.NoError(t, err)
	parsesAsGoExpr(t, got)
}

func TestEncodeNumberLitInt(t *testing.T) {
	got, err := EncodeNode(&lang.NumberLit{Value: "42", ParsedInt: 42})
	require.NoError(t, err)
	require.Contains(t, got, `Value: "42"`)
	require.Contains(t, got, "ParsedInt: 42")
	parsesAsGoExpr(t, got)
}

func TestEncodeNumberLitFloat(t *testing.T) {
	got, err := EncodeNode(&lang.NumberLit{Value: "3.14", IsFloat: true, ParsedFloat: 3.14})
	require.NoError(t, err)
	require.Contains(t, got, `Value: "3.14"`)
	require.Contains(t, got, "IsFloat: true")
	require.Contains(t, got, "ParsedFloat: 3.14")
	parsesAsGoExpr(t, got)
}

func TestEncodeBoolLit(t *testing.T) {
	got, err := EncodeNode(&lang.BoolLit{Value: true})
	require.NoError(t, err)
	require.Equal(t, "&lang.BoolLit{Value: true}", got)
	parsesAsGoExpr(t, got)
}

func TestEncodeNullLit(t *testing.T) {
	got, err := EncodeNode(&lang.NullLit{})
	require.NoError(t, err)
	require.Equal(t, "&lang.NullLit{}", got)
}

func TestEncodeIdent(t *testing.T) {
	got, err := EncodeNode(&lang.Ident{Name: "string"})
	require.NoError(t, err)
	require.Equal(t, `&lang.Ident{Name: "string"}`, got)
}

func TestEncodeArrayLit(t *testing.T) {
	got, err := EncodeNode(&lang.ArrayLit{Elements: []lang.Expr{
		&lang.StringLit{Value: "a"},
		&lang.NumberLit{Value: "1", ParsedInt: 1},
	}})
	require.NoError(t, err)
	require.Contains(t, got, `Elements: []lang.Expr{`)
	require.Contains(t, got, `&lang.StringLit{Value: "a"}`)
	require.Contains(t, got, `ParsedInt: 1`)
	parsesAsGoExpr(t, got)
}

func TestEncodeObjectLit(t *testing.T) {
	got, err := EncodeNode(&lang.ObjectLit{Fields: []*lang.Field{
		{
			Key:   lang.FieldKey{Kind: lang.FieldIdent, Name: "size"},
			Value: &lang.NumberLit{Value: "3", ParsedInt: 3},
		},
		{
			Key:   lang.FieldKey{Kind: lang.FieldString, String: "weird key"},
			Value: &lang.StringLit{Value: "v"},
		},
	}})
	require.NoError(t, err)
	require.Contains(t, got, `Fields: []*lang.Field{`)
	require.Contains(t, got, `Kind: lang.FieldIdent, Name: "size"`)
	require.Contains(t, got, `Kind: lang.FieldString, String: "weird key"`)
	parsesAsGoExpr(t, got)
}

func TestEncodeSelectorBodyField(t *testing.T) {
	got, err := EncodeNode(&lang.Field{
		Key: lang.FieldKey{Kind: lang.FieldIdent, Name: "hello"},
		Decl: &lang.SelectorBody{
			Selector: lang.Selector{
				Parts: []lang.Ident{{Name: "std"}, {Name: "fs-file"}},
			},
			Body: &lang.ObjectLit{Fields: []*lang.Field{{
				Key:   lang.FieldKey{Kind: lang.FieldIdent, Name: "path"},
				Value: &lang.Ident{Name: "path"},
			}}},
		},
	})
	require.NoError(t, err)
	require.Contains(t, got, `Decl: &lang.SelectorBody{`)
	require.Contains(t, got, `Selector: lang.Selector{Parts: []lang.Ident{`)
	require.Contains(t, got, `lang.Ident{Name: "std"}`)
	require.Contains(t, got, `Body: &lang.ObjectLit{`)
	parsesAsGoExpr(t, got)
}

func TestEncodeDotPath(t *testing.T) {
	out := encodeFixture(t, "dot-path")
	require.Contains(t, out, `&lang.DotPath{`)
	require.Contains(t, out, `&lang.Ident{Name: "resource"}`)
	require.Contains(t, out, `Segments: []lang.DotSegment{`)
	require.Contains(t, out, `{Name: "aws"}`)
	require.Contains(t, out, `{Name: "id"}`)
	parsesAsGoExpr(t, out)
}

func TestEncodeDotPathSplat(t *testing.T) {
	out := encodeFixture(t, "dot-path-splat")
	require.Contains(t, out, `{Splat: true}`)
	require.Contains(t, out, `{Name: "id"}`)
	parsesAsGoExpr(t, out)
}

func TestEncodeDotPathGuarded(t *testing.T) {
	out := encodeFixture(t, "dot-path-guarded")
	require.Contains(t, out, `{Name: "db", Guarded: true}`)
	require.Contains(t, out, `{Name: "host", Guarded: true}`)
	parsesAsGoExpr(t, out)
}

func TestEncodeDotPathWithIndex(t *testing.T) {
	out := encodeFixture(t, "dot-path-with-index")
	require.Contains(t, out, `Index: &lang.StringLit{Value: "us-east-1a"}`)
	parsesAsGoExpr(t, out)
}

func TestEncodeCall(t *testing.T) {
	out := encodeFixture(t, "call")
	require.Contains(t, out, `&lang.Call{`)
	require.Contains(t, out, `Callee: &lang.Ident{Name: "format"}`)
	require.Contains(t, out, `Args: []lang.Expr{`)
	parsesAsGoExpr(t, out)
}

func TestEncodeInfixAndPrefix(t *testing.T) {
	out := encodeFixture(t, "infix-and-prefix")
	require.Contains(t, out, `&lang.Prefix{Op: "!"`)
	require.Contains(t, out, `&lang.Infix{Op: "=="`)
	parsesAsGoExpr(t, out)
}

func TestEncodeFile(t *testing.T) {
	out := encodeFixture(t, "file")
	require.True(t, strings.HasPrefix(out, "&lang.File{"))
	require.Contains(t, out, "Kind: lang.FileUnknown")
	require.Contains(t, out, "Body: &lang.ObjectLit{")
	require.Contains(t, out, `&lang.StringLit{Value: "test stack"}`)
	parsesAsGoExpr(t, out)
}

func TestEncodeFileExpressionTypechecks(t *testing.T) {
	// Wrap the encoded expression in a stub Go file and parse it as a
	// full file. This catches any obvious imbalance in braces or
	// commas that ParseExpr alone might miss.
	got := encodeFixture(t, "file-expression-typechecks")
	wrapped := "package x\n\nimport \"github.com/cloudboss/unobin/pkg/lang\"\n\nvar _ = " + got + "\n"
	fset := token.NewFileSet()
	_, err := parser.ParseFile(fset, "x.go", wrapped, parser.AllErrors)
	require.NoError(t, err, "wrapped output should parse:\n%s", wrapped)
}

func TestEncodeRejectsUnsupportedNode(t *testing.T) {
	type bogus struct{ lang.Node }
	_, err := EncodeNode(bogus{})
	require.Error(t, err)
}

func TestEncodeCallModuleQualified(t *testing.T) {
	got, err := EncodeNode(&lang.Call{
		Library: &lang.Ident{Name: "lib"},
		Func:    &lang.Ident{Name: "foo"},
		Args:    []lang.Expr{&lang.NumberLit{Value: "1", ParsedInt: 1}},
	})
	require.NoError(t, err)
	require.Contains(t, got, `Library: &lang.Ident{Name: "lib"}`)
	require.Contains(t, got, `Func: &lang.Ident{Name: "foo"}`)
	parsesAsGoExpr(t, got)
}

func TestEncodeMetaKey(t *testing.T) {
	out := encodeFixture(t, "meta-key")
	require.Contains(t, out, `Name: "@trigger"`)
	parsesAsGoExpr(t, out)
}

func TestEncodeEmptyObject(t *testing.T) {
	got, err := EncodeNode(&lang.ObjectLit{})
	require.NoError(t, err)
	require.Equal(t, "&lang.ObjectLit{Fields: []*lang.Field{}}", got)
	parsesAsGoExpr(t, got)
}

func TestEncodeEmptyArray(t *testing.T) {
	got, err := EncodeNode(&lang.ArrayLit{})
	require.NoError(t, err)
	require.Equal(t, "&lang.ArrayLit{Elements: []lang.Expr{}}", got)
	parsesAsGoExpr(t, got)
}

func TestEncodeTypeAtomic(t *testing.T) {
	got, err := EncodeNode(&lang.TypeAtomic{Name: "string"})
	require.NoError(t, err)
	require.Equal(t, `&lang.TypeAtomic{Name: "string"}`, got)
	parsesAsGoExpr(t, got)
}

func TestEncodeTypeList(t *testing.T) {
	got, err := EncodeNode(&lang.TypeList{Elem: &lang.TypeAtomic{Name: "string"}})
	require.NoError(t, err)
	require.Contains(t, got, "&lang.TypeList{Elem: ")
	require.Contains(t, got, `&lang.TypeAtomic{Name: "string"}`)
	parsesAsGoExpr(t, got)
}

func TestEncodeTypeMap(t *testing.T) {
	got, err := EncodeNode(&lang.TypeMap{Elem: &lang.TypeAtomic{Name: "string"}})
	require.NoError(t, err)
	require.Contains(t, got, "&lang.TypeMap{Elem: ")
	parsesAsGoExpr(t, got)
}

func TestEncodeTypeObject(t *testing.T) {
	got, err := EncodeNode(&lang.TypeObject{Fields: []*lang.TypeObjectField{
		{Name: "x", Type: &lang.TypeAtomic{Name: "string"}},
		{Name: "y", Decl: &lang.ObjectLit{}},
	}})
	require.NoError(t, err)
	require.Contains(t, got, "&lang.TypeObject{Fields: []*lang.TypeObjectField{")
	require.Contains(t, got, `Name: "x", Type: `)
	require.Contains(t, got, `Name: "y", Decl: &lang.ObjectLit{`)
	parsesAsGoExpr(t, got)
}

func TestEncodeTypeTuple(t *testing.T) {
	got, err := EncodeNode(&lang.TypeTuple{Elements: []lang.TypeExpr{
		&lang.TypeAtomic{Name: "string"},
		&lang.TypeAtomic{Name: "integer"},
	}})
	require.NoError(t, err)
	require.Contains(t, got, "&lang.TypeTuple{Elements: []lang.TypeExpr{")
	parsesAsGoExpr(t, got)
}

func TestEncodeTypeOptional(t *testing.T) {
	got, err := EncodeNode(&lang.TypeOptional{Elem: &lang.TypeAtomic{Name: "string"}})
	require.NoError(t, err)
	require.Contains(t, got, "&lang.TypeOptional{Elem: ")
	parsesAsGoExpr(t, got)
}

func TestEncodeTypeLibraryConfig(t *testing.T) {
	got, err := EncodeNode(&lang.TypeLibraryConfig{
		Path: &lang.StringLit{Value: "github.com/acme/aws"},
	})
	require.NoError(t, err)
	require.Contains(t, got, "&lang.TypeLibraryConfig{")
	require.Contains(t, got, `Value: "github.com/acme/aws"`)
	parsesAsGoExpr(t, got)
}

func encodeExpr(t *testing.T, src string) string {
	t.Helper()
	f, err := lang.ParseSource("test.ub", []byte("v: "+src+"\n"))
	require.NoError(t, err)
	require.NotEmpty(t, f.Body.Fields)
	got, err := EncodeNode(f.Body.Fields[0].Value)
	require.NoError(t, err)
	parsesAsGoExpr(t, got)
	return got
}

func TestEncodeConditional(t *testing.T) {
	got := encodeExpr(t, "if a then b else c")
	require.Equal(t,
		`&lang.Conditional{Cond: &lang.Ident{Name: "a"}, `+
			`Then: &lang.Ident{Name: "b"}, Else: &lang.Ident{Name: "c"}}`,
		got)
}

func TestEncodeListComprehension(t *testing.T) {
	got := encodeExpr(t, "[ for s in var.subnets : s.cidr ]")
	require.Equal(t,
		`&lang.Comprehension{Kind: lang.CompList, Names: []string{"s"}, `+
			`Source: &lang.DotPath{Root: &lang.Ident{Name: "var"}, `+
			`Segments: []lang.DotSegment{{Name: "subnets"}}}, `+
			`Value: &lang.DotPath{Root: &lang.Ident{Name: "s"}, `+
			`Segments: []lang.DotSegment{{Name: "cidr"}}}}`,
		got)
}

func TestEncodeMapComprehensionGroupAndFilter(t *testing.T) {
	got := encodeExpr(t, "{ for s in var.subnets : s.az => s.id... when s.public }")
	require.Contains(t, got, "&lang.Comprehension{Kind: lang.CompMap")
	require.Contains(t, got, `Names: []string{"s"}`)
	require.Contains(t, got, "Key: &lang.DotPath{")
	require.Contains(t, got, "Group: true")
	require.Contains(t, got, "Filter: &lang.DotPath{")
}

func TestEncodeComprehensionTwoNames(t *testing.T) {
	got := encodeExpr(t, "[ for i, s in var.items : i ]")
	require.Contains(t, got, `Names: []string{"i", "s"}`)
}

func TestEncodeLocalsBlock(t *testing.T) {
	got := encodeFixture(t, "locals-block")
	parsesAsGoExpr(t, got)
	require.Contains(t, got, `Name: "locals"`,
		"the locals block must survive codegen so a composite body keeps it")
	require.Contains(t, got, `&lang.Ident{Name: "local"}`,
		"a local reference inside the block must encode as a local-rooted path")
}
