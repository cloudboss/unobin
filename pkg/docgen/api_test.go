package docgen

import (
	"go/ast"
	"go/doc"
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parsePackage(t *testing.T, src string) (*doc.Package, *token.FileSet) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "src.go", src, parser.ParseComments)
	require.NoError(t, err)
	pkg, err := doc.NewFromFiles(fset, []*ast.File{f}, "example.com/p")
	require.NoError(t, err)
	return pkg, fset
}

func TestAPIReference(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "package doc and a function",
			src: "// Package p does things.\n" +
				"package p\n\n" +
				"// Foo does a foo.\n" +
				"func Foo() {}\n",
			want: "# package p\n" +
				"\n```\nimport \"example.com/p\"\n```\n" +
				"\nPackage p does things.\n" +
				"\n## Functions\n" +
				"\n### func Foo\n" +
				"\n```go\nfunc Foo()\n```\n" +
				"\nFoo does a foo.\n",
		},
		{
			name: "type with a constructor and a method",
			src: "package p\n\n" +
				"// Greeter greets people.\n" +
				"type Greeter struct {\n" +
				"\tName string\n" +
				"}\n\n" +
				"// New makes a Greeter.\n" +
				"func New() *Greeter { return &Greeter{} }\n\n" +
				"// Hello returns a greeting.\n" +
				"func (g *Greeter) Hello() string { return \"\" }\n",
			want: "# package p\n" +
				"\n```\nimport \"example.com/p\"\n```\n" +
				"\n## Types\n" +
				"\n### type Greeter\n" +
				"\n```go\ntype Greeter struct {\n\tName string\n}\n```\n" +
				"\nGreeter greets people.\n" +
				"\n#### func New\n" +
				"\n```go\nfunc New() *Greeter\n```\n" +
				"\nNew makes a Greeter.\n" +
				"\n#### func (*Greeter) Hello\n" +
				"\n```go\nfunc (g *Greeter) Hello() string\n```\n" +
				"\nHello returns a greeting.\n",
		},
		{
			name: "unexported names are excluded",
			src: "package p\n\n" +
				"// Exported is shown.\n" +
				"func Exported() {}\n\n" +
				"func unexported() {}\n",
			want: "# package p\n" +
				"\n```\nimport \"example.com/p\"\n```\n" +
				"\n## Functions\n" +
				"\n### func Exported\n" +
				"\n```go\nfunc Exported()\n```\n" +
				"\nExported is shown.\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg, fset := parsePackage(t, tt.src)
			assert.Equal(t, tt.want, APIReference(pkg, fset))
		})
	}
}

func TestAPIReferenceIsDeterministic(t *testing.T) {
	src := "// Package p.\n" +
		"package p\n\n" +
		"func Zebra() {}\n" +
		"func Apple() {}\n"
	pkg, fset := parsePackage(t, src)
	first := APIReference(pkg, fset)
	for range 5 {
		require.Equal(t, first, APIReference(pkg, fset))
	}
	assert.Less(t, indexOf(first, "func Apple"), indexOf(first, "func Zebra"))
}
